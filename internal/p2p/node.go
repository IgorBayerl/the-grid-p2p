package p2p

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/IgorBayerl/the-grid/internal/protocol"

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/discovery/routing"
	dutil "github.com/libp2p/go-libp2p/p2p/discovery/util"
	"github.com/multiformats/go-multiaddr"
)

type Node struct {
	Ctx       context.Context
	Host      host.Host
	DHT       *dht.IpfsDHT
	PubSub    *pubsub.PubSub
	Discovery *routing.RoutingDiscovery

	MsgIn      chan protocol.NetMessage
	StatusChan chan protocol.StatusUpdate

	CurrentRoom string
	DiscoveryTp *pubsub.Topic

	GameStream network.Stream
	RemotePeer peer.ID

	streamLock sync.Mutex
}

func NewNode() *Node {
	h, err := libp2p.New(
		libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/0"),
		libp2p.EnableNATService(),
		libp2p.EnableHolePunching(),
	)
	if err != nil {
		log.Fatal(err)
	}

	n := &Node{
		Ctx:        context.Background(),
		Host:       h,
		MsgIn:      make(chan protocol.NetMessage, 100),
		StatusChan: make(chan protocol.StatusUpdate, 100),
	}

	h.SetStreamHandler(protocol.GameProtocol, n.handleStream)

	return n
}

func (n *Node) Log(format string, a ...interface{}) {
	msg := fmt.Sprintf(format, a...)
	select {
	case n.StatusChan <- protocol.StatusUpdate{Key: "LOG", Value: msg}:
	default:
	}
}

// --- Game Logic ---

func (n *Node) StartGame() {
	if n.RemotePeer == "" {
		n.Log("Cannot Start: Waiting for Peer...")
		return
	}

	n.Log("Dialing Game Stream to %s...", n.RemotePeer.ShortString())
	s, err := n.Host.NewStream(n.Ctx, n.RemotePeer, protocol.GameProtocol)
	if err != nil {
		n.Log("Stream Failed: %s", err)
		return
	}

	// Set synchronously
	n.GameStream = s

	// Start Heartbeat loop
	go n.heartbeatLoop()

	// Handle Incoming
	go n.handleStream(s)
}

func (n *Node) heartbeatLoop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-n.Ctx.Done():
			return
		case <-ticker.C:
			if n.GameStream != nil {
				// Send Heartbeat (Empty payload)
				n.SendPacket(protocol.PacketHeartbeat, nil)
			} else {
				return // Stream closed
			}
		}
	}
}

func (n *Node) handleStream(s network.Stream) {
	n.RemotePeer = s.Conn().RemotePeer()
	n.GameStream = s

	n.Log("Connected to %s", n.RemotePeer.ShortString())
	n.StatusChan <- protocol.StatusUpdate{Key: "GAME", Value: "START"}

	// Start sending heartbeats if I was the receiver of the stream too
	go n.heartbeatLoop()

	rw := bufio.NewReadWriter(bufio.NewReader(s), bufio.NewWriter(s))

	for {
		// Read until newline
		str, err := rw.ReadString('\n')
		if err != nil {
			n.Log("Stream Closed: %v", err)
			n.StatusChan <- protocol.StatusUpdate{Key: "GAME", Value: "STOP"}
			s.Reset()
			return
		}

		// Decode
		var msg protocol.NetMessage
		if err := json.Unmarshal([]byte(str), &msg); err != nil {
			// This often happens if 'Sender' is malformed in JSON
			n.Log("Bad Packet JSON: %v", err)
			continue
		}

		// Important: We populate the Sender manually.
		// We do NOT rely on the JSON field for ID.
		msg.Sender = s.Conn().RemotePeer()
		n.MsgIn <- msg
	}
}

func (n *Node) SendPacket(typeByte byte, payload interface{}) {
	if n.GameStream == nil {
		return
	}

	// Payload might be nil for heartbeat
	var data []byte
	if payload != nil {
		data, _ = json.Marshal(payload)
	}

	msg := protocol.NetMessage{
		Type: typeByte,
		Data: data,
		// Sender is purposely left blank/ignored by JSON
	}

	bytes, _ := json.Marshal(msg)

	n.streamLock.Lock()
	defer n.streamLock.Unlock()

	// Append Newline to frame the message
	_, err := n.GameStream.Write(append(bytes, '\n'))
	if err != nil {
		n.Log("Send Error: %s", err)
		n.GameStream = nil
	}
}

// --- Discovery & Bootstrap (Standard) ---

func (n *Node) Bootstrap() {
	n.StatusChan <- protocol.StatusUpdate{Key: "DHT", Value: "START"}
	n.Log("Connecting to Public Swarm...")
	n.ConnectToBootnodes()

	kademlia, err := dht.New(n.Ctx, n.Host)
	if err != nil {
		return
	}
	n.DHT = kademlia
	kademlia.Bootstrap(n.Ctx)

	n.Discovery = routing.NewRoutingDiscovery(kademlia)

	ps, err := pubsub.NewGossipSub(n.Ctx, n.Host)
	if err != nil {
		return
	}
	n.PubSub = ps

	topic, err := n.PubSub.Join(protocol.DiscoveryTopic)
	if err != nil {
		return
	}
	n.DiscoveryTp = topic

	go n.handleDiscoveryEvents(topic)

	for i := 0; i < 30; i++ {
		if len(n.Host.Network().Peers()) > 0 {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	n.StatusChan <- protocol.StatusUpdate{Key: "DHT", Value: "DONE"}
}

func (n *Node) ConnectToBootnodes() {
	var wg sync.WaitGroup
	for _, addrStr := range dht.DefaultBootstrapPeers {
		addr, err := multiaddr.NewMultiaddr(addrStr.String())
		if err != nil {
			continue
		}
		peerinfo, _ := peer.AddrInfoFromP2pAddr(addr)
		wg.Add(1)
		go func(pi *peer.AddrInfo) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(n.Ctx, 5*time.Second)
			defer cancel()
			n.Host.Connect(ctx, *pi)
		}(peerinfo)
	}
	wg.Wait()
}

func (n *Node) JoinLobby(code string) {
	n.CurrentRoom = code
	n.StatusChan <- protocol.StatusUpdate{Key: "RELAY", Value: "SEARCHING"}
	go n.advertiseLoop()
	dutil.Advertise(n.Ctx, n.Discovery, "grid-lobby-"+code)
	go n.findPeersDHT("grid-lobby-" + code)
}

func (n *Node) advertiseLoop() {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-n.Ctx.Done():
			return
		case <-ticker.C:
			count := len(n.Host.Network().Peers())
			n.StatusChan <- protocol.StatusUpdate{Key: "SWARM", Value: fmt.Sprintf("%d", count)}

			addrs := []string{}
			for _, a := range n.Host.Addrs() {
				addrs = append(addrs, a.String())
			}
			packet := protocol.DiscoveryPacket{
				RoomCode: n.CurrentRoom, PeerID: n.Host.ID().String(), Addrs: addrs,
			}
			data, _ := json.Marshal(packet)
			if n.DiscoveryTp != nil {
				n.DiscoveryTp.Publish(n.Ctx, data)
			}
		}
	}
}

func (n *Node) findPeersDHT(topic string) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-n.Ctx.Done():
			return
		case <-ticker.C:
			peerChan, err := n.Discovery.FindPeers(n.Ctx, topic)
			if err != nil {
				continue
			}
			for p := range peerChan {
				if p.ID == n.Host.ID() {
					continue
				}
				n.connectToPeer(p.ID, p.Addrs)
			}
		}
	}
}

func (n *Node) handleDiscoveryEvents(topic *pubsub.Topic) {
	sub, err := topic.Subscribe()
	if err != nil {
		return
	}
	for {
		msg, err := sub.Next(n.Ctx)
		if err != nil {
			return
		}
		if msg.ReceivedFrom == n.Host.ID() {
			continue
		}

		var packet protocol.DiscoveryPacket
		if err := json.Unmarshal(msg.Data, &packet); err != nil {
			continue
		}

		if packet.RoomCode == n.CurrentRoom {
			var mas []multiaddr.Multiaddr
			for _, a := range packet.Addrs {
				ma, err := multiaddr.NewMultiaddr(a)
				if err == nil {
					mas = append(mas, ma)
				}
			}
			id, _ := peer.Decode(packet.PeerID)
			n.connectToPeer(id, mas)
		}
	}
}

func (n *Node) connectToPeer(id peer.ID, addrs []multiaddr.Multiaddr) {
	if n.Host.Network().Connectedness(id) == network.Connected {
		n.RemotePeer = id
		go n.monitorConnection(id)
		return
	}

	n.RemotePeer = id
	n.StatusChan <- protocol.StatusUpdate{Key: "PEERS", Value: id.String()}

	targetInfo := peer.AddrInfo{ID: id, Addrs: addrs}
	ctx, cancel := context.WithTimeout(n.Ctx, 10*time.Second)
	defer cancel()

	n.Host.Connect(ctx, targetInfo)
	n.StatusChan <- protocol.StatusUpdate{Key: "RELAY", Value: "CONNECTED"}
	go n.monitorConnection(id)
}

func (n *Node) monitorConnection(pid peer.ID) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	loggedDirect := false

	for {
		select {
		case <-n.Ctx.Done():
			return
		case <-ticker.C:
			conns := n.Host.Network().ConnsToPeer(pid)
			if len(conns) == 0 {
				return
			}
			n.StatusChan <- protocol.StatusUpdate{Key: "PEERS", Value: pid.String()}

			for _, c := range conns {
				addr := c.RemoteMultiaddr().String()
				isRelay := strings.Contains(addr, "circuit")

				if !isRelay {
					if !loggedDirect {
						n.Log("DIRECT LINK ESTABLISHED! (%s)", addr)
						n.StatusChan <- protocol.StatusUpdate{Key: "HOLEPUNCH", Value: "DIRECT"}
						loggedDirect = true
					}
					return
				}
			}
		}
	}
}

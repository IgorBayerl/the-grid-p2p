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

// NetworkNode defines the public interface
type NetworkNode interface {
	ID() peer.ID
	GetRemotePeer() peer.ID
	IsConnected() bool
	Send(typeByte byte, payload interface{})
	Events() <-chan interface{}
	JoinLobby(code string)
	StartStream(pid peer.ID)
	Bootstrap()
}

type Node struct {
	Ctx       context.Context
	Host      host.Host
	DHT       *dht.IpfsDHT
	PubSub    *pubsub.PubSub
	Discovery *routing.RoutingDiscovery

	eventsOut chan interface{}

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
		Ctx:       context.Background(),
		Host:      h,
		eventsOut: make(chan interface{}, 100),
	}

	h.SetStreamHandler(protocol.GameProtocol, n.handleStream)

	return n
}

// --- Interface Implementation ---

func (n *Node) ID() peer.ID {
	return n.Host.ID()
}

func (n *Node) GetRemotePeer() peer.ID {
	return n.RemotePeer
}

func (n *Node) IsConnected() bool {
	n.streamLock.Lock()
	defer n.streamLock.Unlock()
	return n.GameStream != nil
}

func (n *Node) Events() <-chan interface{} {
	return n.eventsOut
}

func (n *Node) Send(typeByte byte, payload interface{}) {
	n.streamLock.Lock()
	s := n.GameStream
	n.streamLock.Unlock()

	if s == nil {
		return
	}

	var data []byte
	if payload != nil {
		data, _ = json.Marshal(payload)
	}

	msg := protocol.NetMessage{
		Type: typeByte,
		Data: data,
	}

	bytes, _ := json.Marshal(msg)

	// We append a newline so the other side can use ReadString('\n')
	_, err := s.Write(append(bytes, '\n'))
	if err != nil {
		n.emitLog(fmt.Sprintf("Send Error: %s", err))
		n.closeStream()
	}
}

// --- Internal Helpers ---

func (n *Node) emitStatus(key, val string) {
	select {
	case n.eventsOut <- protocol.StatusEvent{Key: key, Val: val}:
	default:
	}
}

func (n *Node) emitLog(msg string) {
	select {
	case n.eventsOut <- protocol.LogEvent(msg):
	default:
	}
}

func (n *Node) closeStream() {
	n.streamLock.Lock()
	if n.GameStream != nil {
		n.GameStream.Reset()
		n.GameStream = nil
	}
	n.streamLock.Unlock()
	n.emitStatus("GAME", "STOP")
}

// --- Connection Logic ---

func (n *Node) StartStream(pid peer.ID) {
	n.RemotePeer = pid
	n.emitLog(fmt.Sprintf("Dialing Game Stream to %s...", pid.ShortString()))

	s, err := n.Host.NewStream(n.Ctx, pid, protocol.GameProtocol)
	if err != nil {
		n.emitLog(fmt.Sprintf("Stream Failed: %s", err))
		return
	}

	// CRITICAL FIX: Run handleStream in a goroutine to prevent freezing the UI
	go n.handleStream(s)
}

func (n *Node) handleStream(s network.Stream) {
	n.streamLock.Lock()
	n.GameStream = s
	n.RemotePeer = s.Conn().RemotePeer()
	n.streamLock.Unlock()

	n.emitLog(fmt.Sprintf("Connected to %s", n.RemotePeer.ShortString()))
	n.emitStatus("GAME", "START")

	// CRITICAL FIX: Use bufio.Reader instead of json.Decoder for robustness
	reader := bufio.NewReader(s)

	for {
		// Read until newline
		str, err := reader.ReadString('\n')
		if err != nil {
			n.emitLog(fmt.Sprintf("Stream Closed: %v", err))
			n.closeStream()
			return
		}

		if len(strings.TrimSpace(str)) == 0 {
			continue
		}

		var msg protocol.NetMessage
		if err := json.Unmarshal([]byte(str), &msg); err != nil {
			n.emitLog(fmt.Sprintf("Bad Packet: %v", err))
			continue
		}

		// Inject Sender ID
		msg.Sender = s.Conn().RemotePeer()
		n.eventsOut <- msg
	}
}

// --- Discovery & Bootstrap ---

func (n *Node) Bootstrap() {
	n.emitStatus("DHT", "START")
	n.emitLog("Connecting to Public Swarm...")
	n.connectToBootnodes()

	kademlia, err := dht.New(n.Ctx, n.Host)
	if err != nil {
		n.emitLog(fmt.Sprintf("DHT Error: %v", err))
		return
	}
	n.DHT = kademlia
	kademlia.Bootstrap(n.Ctx)

	n.Discovery = routing.NewRoutingDiscovery(kademlia)

	ps, err := pubsub.NewGossipSub(n.Ctx, n.Host)
	if err != nil {
		n.emitLog(fmt.Sprintf("PubSub Error: %v", err))
		return
	}
	n.PubSub = ps

	topic, err := n.PubSub.Join(protocol.DiscoveryTopic)
	if err != nil {
		n.emitLog(fmt.Sprintf("Join Topic Error: %v", err))
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
	n.emitStatus("DHT", "DONE")
}

func (n *Node) connectToBootnodes() {
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
	n.emitStatus("RELAY", "SEARCHING")
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
			n.emitStatus("SWARM", fmt.Sprintf("%d", count))

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
	n.emitStatus("PEERS", id.String())

	targetInfo := peer.AddrInfo{ID: id, Addrs: addrs}
	ctx, cancel := context.WithTimeout(n.Ctx, 10*time.Second)
	defer cancel()

	n.Host.Connect(ctx, targetInfo)
	n.emitStatus("RELAY", "CONNECTED")
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
			n.emitStatus("PEERS", pid.String())

			for _, c := range conns {
				addr := c.RemoteMultiaddr().String()
				isRelay := strings.Contains(addr, "circuit")

				if !isRelay {
					if !loggedDirect {
						n.emitLog(fmt.Sprintf("DIRECT LINK ESTABLISHED! (%s)", addr))
						n.emitStatus("HOLEPUNCH", "DIRECT")
						loggedDirect = true
					}
					return
				}
			}
		}
	}
}

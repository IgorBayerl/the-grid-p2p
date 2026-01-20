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
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/discovery/routing"
	dutil "github.com/libp2p/go-libp2p/p2p/discovery/util"
	"github.com/multiformats/go-multiaddr"
)

type NetworkNode interface {
	ID() peer.ID
	GetRemotePeer() peer.ID
	IsConnected() bool
	Send(typeByte byte, payload interface{})
	Events() <-chan interface{}
	JoinLobby(code string)
	LeaveLobby()
	StartStream(pid peer.ID)
	Bootstrap()
}

type Node struct {
	Ctx       context.Context
	Host      host.Host
	DHT       *dht.IpfsDHT
	Discovery *routing.RoutingDiscovery

	eventsOut chan interface{}

	CurrentRoom string
	lobbyCtx    context.Context
	lobbyCancel context.CancelFunc

	GameStream network.Stream
	RemotePeer peer.ID
	streamLock sync.Mutex
}

func NewNode() *Node {
	// Enable NATService and HolePunching for direct P2P links
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

// --- Public API ---

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
	_, err := s.Write(append(bytes, '\n'))
	if err != nil {
		n.emitLog(fmt.Sprintf("Send Error: %s", err))
		n.closeStream()
	}
}

// --- Internal Utilities ---

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
		_ = n.GameStream.Reset()
		n.GameStream = nil
	}
	n.RemotePeer = ""
	n.streamLock.Unlock()
	n.emitStatus("GAME", "STOP")
}

// --- Stream Management ---

func (n *Node) StartStream(pid peer.ID) {
	n.RemotePeer = pid
	n.emitLog(fmt.Sprintf("Dialing Game Stream to %s...", pid.ShortString()))

	s, err := n.Host.NewStream(n.Ctx, pid, protocol.GameProtocol)
	if err != nil {
		n.emitLog(fmt.Sprintf("Stream Failed: %s", err))
		return
	}

	go n.handleStream(s)
}

func (n *Node) handleStream(s network.Stream) {
	n.streamLock.Lock()
	n.GameStream = s
	n.RemotePeer = s.Conn().RemotePeer()
	n.streamLock.Unlock()

	n.emitLog(fmt.Sprintf("Connected to %s", n.RemotePeer.ShortString()))
	n.emitStatus("GAME", "START")

	reader := bufio.NewReader(s)

	for {
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

		msg.Sender = s.Conn().RemotePeer()
		n.eventsOut <- msg
	}
}

// --- Bootstrap & Discovery ---

func (n *Node) Bootstrap() {
	n.emitStatus("DHT", "START")
	n.emitLog("Connecting to Public Swarm...")

	n.connectToBootnodes()

	// Initialize DHT for peer discovery
	kademlia, err := dht.New(n.Ctx, n.Host)
	if err != nil {
		n.emitLog(fmt.Sprintf("DHT Error: %v", err))
		return
	}
	n.DHT = kademlia

	if err = kademlia.Bootstrap(n.Ctx); err != nil {
		n.emitLog(fmt.Sprintf("DHT Bootstrap Error: %v", err))
	}

	n.Discovery = routing.NewRoutingDiscovery(kademlia)

	go n.reportSwarmSize()

	n.waitForSwarm()
	n.emitStatus("DHT", "DONE")
}

func (n *Node) LeaveLobby() {
	if n.lobbyCancel != nil {
		n.lobbyCancel()
	}
	n.closeStream()
	n.CurrentRoom = ""
}

func (n *Node) JoinLobby(code string) {
	if n.lobbyCancel != nil {
		n.lobbyCancel()
	}

	n.lobbyCtx, n.lobbyCancel = context.WithCancel(n.Ctx)
	n.CurrentRoom = code
	n.emitStatus("RELAY", "SEARCHING")

	n.emitLog("Starting DHT Discovery...")

	// Get current time slot for advertising
	topics := getDiscoveryTopics(code)
	currentTopic := topics[0] // Advertise on the FRESHEST slot only

	// Advertise our presence
	dutil.Advertise(n.lobbyCtx, n.Discovery, currentTopic)

	// Search for others (pass the code, not the topic, we calculate topics inside)
	go n.findPeersLoop(n.lobbyCtx, code)
}

func (n *Node) findPeersLoop(ctx context.Context, code string) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	n.emitLog(fmt.Sprintf("DHT: Querying lobby %s...", code))

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Calculate valid topics for right now
			searchTopics := getDiscoveryTopics(code)

			for _, topic := range searchTopics {
				peerChan, err := n.Discovery.FindPeers(ctx, topic)
				if err != nil {
					continue
				}

				for p := range peerChan {
					if p.ID == n.Host.ID() {
						continue
					}
					// We found someone!
					n.connectToPeer(ctx, p.ID, p.Addrs)
				}
			}
		}
	}
}

// --- Connection Handlers ---

func (n *Node) connectToPeer(ctx context.Context, id peer.ID, addrs []multiaddr.Multiaddr) {
	if n.Host.Network().Connectedness(id) == network.Connected {
		n.RemotePeer = id
		go n.monitorConnection(ctx, id)
		return
	}

	n.RemotePeer = id
	n.emitStatus("PEERS", id.String())
	n.emitLog("Found Peer. Initiating Handshake...")

	targetInfo := peer.AddrInfo{ID: id, Addrs: addrs}
	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	err := n.Host.Connect(dialCtx, targetInfo)
	if err != nil {
		n.emitLog(fmt.Sprintf("Handshake Failed: %s", err))
		return
	}

	n.emitStatus("RELAY", "CONNECTED")
	go n.monitorConnection(ctx, id)
}

func (n *Node) monitorConnection(ctx context.Context, pid peer.ID) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			conns := n.Host.Network().ConnsToPeer(pid)
			if len(conns) == 0 {
				return
			}
			n.emitStatus("PEERS", pid.String())

			for _, c := range conns {
				addr := c.RemoteMultiaddr().String()
				// Check for relay circuit in address
				isRelay := strings.Contains(addr, "circuit")

				if !isRelay {
					n.emitLog(fmt.Sprintf("Direct Link Confirmed (%s)", addr))
					n.emitStatus("HOLEPUNCH", "DIRECT")
					return
				}
			}
		}
	}
}

func (n *Node) reportSwarmSize() {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-n.Ctx.Done():
			return
		case <-ticker.C:
			count := len(n.Host.Network().Peers())
			n.emitStatus("SWARM", fmt.Sprintf("%d", count))
		}
	}
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
			_ = n.Host.Connect(ctx, *pi)
		}(peerinfo)
	}
	wg.Wait()
}

func (n *Node) waitForSwarm() {
	for i := 0; i < 30; i++ {
		if len(n.Host.Network().Peers()) > 0 {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func getDiscoveryTopics(code string) []string {
	const timeWindow = 120
	timestamp := time.Now().Unix()

	currentSlot := timestamp / timeWindow
	prevSlot := currentSlot - 1

	return []string{
		fmt.Sprintf("grid-lobby-%s-%d", code, currentSlot),
		fmt.Sprintf("grid-lobby-%s-%d", code, prevSlot),
	}
}

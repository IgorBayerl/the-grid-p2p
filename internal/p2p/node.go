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
	Discovery *routing.RoutingDiscovery

	// --- STRATEGY B: PUBSUB (Variables) ---
	// PubSub      *pubsub.PubSub
	// DiscoveryTp *pubsub.Topic

	eventsOut chan interface{}

	CurrentRoom string

	GameStream network.Stream
	RemotePeer peer.ID

	streamLock sync.Mutex
}

func NewNode() *Node {
	// 1. Configure the Host to support NAT Traversal
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

// --- Connection Logic (The "Airlock") ---

func (n *Node) StartStream(pid peer.ID) {
	n.RemotePeer = pid
	n.emitLog(fmt.Sprintf("Dialing Game Stream to %s...", pid.ShortString()))

	// This is the direct dial. If NAT traversal works, this succeeds.
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

// ============================================================================
//   DISCOVERY & BOOTSTRAP
// ============================================================================

func (n *Node) Bootstrap() {
	n.emitStatus("DHT", "START")
	n.emitLog("Connecting to Public Swarm...")

	// 1. Connect to Public Bootnodes (Gateway to the IPFS network)
	n.connectToBootnodes()

	// 2. Initialize DHT (Distributed Hash Table)
	//    This is needed for Strategy A.
	kademlia, err := dht.New(n.Ctx, n.Host)
	if err != nil {
		n.emitLog(fmt.Sprintf("DHT Error: %v", err))
		return
	}
	n.DHT = kademlia

	//    Make ourselves a server so others can query us
	if err = kademlia.Bootstrap(n.Ctx); err != nil {
		n.emitLog(fmt.Sprintf("DHT Bootstrap Error: %v", err))
	}

	n.Discovery = routing.NewRoutingDiscovery(kademlia)

	// --- STRATEGY B: PUBSUB (DISABLED) ---
	/*
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

		// Start listening for shouted messages
		go n.handleStrategyB_PubSub(topic)
	*/

	// Start a UI reporter loop just to update peer count
	go n.reportSwarmSize()

	// Wait until we have some connections before saying we are ready
	n.waitForSwarm()
	n.emitStatus("DHT", "DONE")
}

func (n *Node) JoinLobby(code string) {
	n.CurrentRoom = code
	n.emitStatus("RELAY", "SEARCHING")

	// =========================================================
	// STRATEGY A: DHT (The Phonebook) - ACTIVE
	// =========================================================
	n.emitLog("Strategy: Using DHT (High Reliability, Slow Discovery)")

	// 1. Advertise: Write our name in the phonebook under "grid-lobby-{code}"
	//    This runs in the background and republishes every few hours.
	dutil.Advertise(n.Ctx, n.Discovery, "grid-lobby-"+code)

	// 2. Find: Periodically check the phonebook for other names
	go n.strategyA_FindPeersDHT("grid-lobby-" + code)

	// =========================================================
	// STRATEGY B: PubSub (The Shout) - DISABLED
	// =========================================================
	// n.emitLog("Strategy: PubSub Disabled")
	// go n.strategyB_AdvertiseLoop()
}

// ============================================================================
//   STRATEGY A: DHT LOGIC (ACTIVE)
//   Concept: We ask the global network "Who provides 'grid-lobby-123'?"
//   Pros: Works even if peers join minutes apart. Persistent.
//   Cons: Slow. Provider records take time to propagate through the network.
// ============================================================================

func (n *Node) strategyA_FindPeersDHT(topic string) {
	// Poll more frequently for university demo purposes (every 2 seconds)
	// In production, you wouldn't spam the DHT this hard.
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	n.emitLog("DHT: Querying network for peers...")

	for {
		select {
		case <-n.Ctx.Done():
			return
		case <-ticker.C:
			// FindPeers asks the DHT for addresses providing the key 'topic'
			peerChan, err := n.Discovery.FindPeers(n.Ctx, topic)
			if err != nil {
				n.emitLog(fmt.Sprintf("DHT Find Error: %v", err))
				continue
			}

			found := false
			for p := range peerChan {
				if p.ID == n.Host.ID() {
					continue // Don't connect to self
				}

				found = true
				// We found a peer in the phonebook! Try to call them.
				n.connectToPeer(p.ID, p.Addrs)
			}

			if !found {
				// Optional: Log that we are still looking
				// n.emitLog("DHT: No peers found yet...")
			}
		}
	}
}

// ============================================================================
//   STRATEGY B: PUBSUB LOGIC (DISABLED)
//   Concept: We shout our details to everyone subscribed to "the-grid-discovery"
//   Pros: Instantaneous if both are online.
//   Cons: If you shout before I listen, I miss it forever.
// ============================================================================

/*
func (n *Node) strategyB_AdvertiseLoop() {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-n.Ctx.Done():
			return
		case <-ticker.C:
			// Create a packet with our details
			addrs := []string{}
			for _, a := range n.Host.Addrs() {
				addrs = append(addrs, a.String())
			}
			packet := protocol.DiscoveryPacket{
				RoomCode: n.CurrentRoom, PeerID: n.Host.ID().String(), Addrs: addrs,
			}
			data, _ := json.Marshal(packet)

			// Publish to the GossipSub topic
			if n.DiscoveryTp != nil {
				n.DiscoveryTp.Publish(n.Ctx, data)
			}
		}
	}
}

func (n *Node) handleStrategyB_PubSub(topic *pubsub.Topic) {
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

		// FILTER: Only connect if they are in the same Room Code
		if packet.RoomCode == n.CurrentRoom {
			id, _ := peer.Decode(packet.PeerID)

			// Reconstruct Multiaddrs
			var mas []multiaddr.Multiaddr
			for _, a := range packet.Addrs {
				ma, err := multiaddr.NewMultiaddr(a)
				if err == nil {
					mas = append(mas, ma)
				}
			}

			n.connectToPeer(id, mas)
		}
	}
}
*/

// ============================================================================
//   COMMON HELPERS
// ============================================================================

func (n *Node) connectToPeer(id peer.ID, addrs []multiaddr.Multiaddr) {
	// If already connected, just monitor the quality (Direct vs Relay)
	if n.Host.Network().Connectedness(id) == network.Connected {
		n.RemotePeer = id
		go n.monitorConnection(id)
		return
	}

	n.RemotePeer = id
	n.emitStatus("PEERS", id.String())
	n.emitLog("Found Peer via Discovery! Connecting...")

	targetInfo := peer.AddrInfo{ID: id, Addrs: addrs}
	ctx, cancel := context.WithTimeout(n.Ctx, 10*time.Second)
	defer cancel()

	// Attempt the Libp2p connection handshake
	err := n.Host.Connect(ctx, targetInfo)
	if err != nil {
		n.emitLog(fmt.Sprintf("Connection Failed: %s", err))
		return
	}

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
				// A Relay address looks like: /ip4/1.2.3.4/tcp/4001/p2p/QmRelay/p2p-circuit/p2p/QmPeer
				isRelay := strings.Contains(addr, "circuit")

				if !isRelay {
					if !loggedDirect {
						n.emitLog(fmt.Sprintf("DIRECT LINK CONFIRMED! (%s)", addr))
						n.emitStatus("HOLEPUNCH", "DIRECT")
						loggedDirect = true
					}
					return
				}
			}
		}
	}
}

// Helper: Just updates the UI on how many peers we are connected to globally
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
			n.Host.Connect(ctx, *pi)
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

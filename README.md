# ⚡ The Grid P2P
### Serverless, Terminal-Based P2P Multiplayer Demonstrator

**The Grid** is a proof-of-concept "Social Gaming SDK" built in Go. It proves that you can create a low-latency, realtime multiplayer experience without renting a single server or database.

It combines **libp2p** (networking), **DHTs** (discovery), and **Bubble Tea** (UI) to create a terminal game that forces direct connections between players, bypassing central relays to ensure low latency.

---

## 📖 In Simple Terms: What is this?

Most multiplayer games work like a conference call: everyone connects to a central server, and that server passes messages around.

**The Grid works like a Walkie-Talkie:**
1.  **Discovery (The Phonebook):** We use the public IPFS network to find other players. We don't have a database of rooms; the network *is* the database.
2.  **The "Time-Slot" (Ghost Busting):** Unlike standard P2P apps where old "ghost" records linger for hours, The Grid uses **Time-Slotted Keys**. Room codes automatically rotate every 2 minutes. If you played 5 minutes ago, your record is mathematically invisible to new players.
3.  **The "Airlock" (The Guard):** The game blocks gameplay until a **Direct Connection** (Hole Punch) is verified. It refuses to play over slow relays.

---

## 🖥️ UI Walkthrough

### 1. The Lobby (Entry)
*The node connects to the global swarm (Bootstrapping). Once connected, you enter a shared code.*

```text
  THE GRID PROTOCOL

  ENTER ROOM CODE:
  > 808_

  [ INFO ]
  Connected to 742 Swarm Nodes.
  Waiting for input...
```

### 2. The Airlock (Negotiation)
*This is the critical phase. The app searches the DHT for the room code within the current 120-second time window.*

```text
  CONNECTION IN PROGRESS
  Target: ROOM 808  |  Timeout: 114s
  / Scanning Network...

  > Discovery   [ FOUND ]      <-- Peer found in DHT
  > Handshake   [ CONNECTED ]  <-- Connected via Relay
  > Direct Link [ WORKING ]    <-- Attempting Hole Punch
```

### 3. The Game (Active)
*A direct UDP/TCP link is established. Latency is calculated in real-time.*

```text
  THE GRID | HOST (Green @) | PING: 42 ms
  ┌──────────────────────────────────────────────────┐
  │ . . . . . . . . . . . . . . . . . . . . . . . . .│
  │ . . @ . . . . . . . . . . . . . . . . . . . . . .│
  │ . . . . . . . . . . . . . . . . . . . X . . . . .│
  │ . . . . . . . . . . . . . . . . . . . . . . . . .│
  └──────────────────────────────────────────────────┘
  [ARROWS] Move  [Q] Quit to Lobby

  [ GAME LOG ]
  > Received Input DX:-1 DY:0
```

---

## 🛠 Architecture & How It Works

<details>
<summary><strong>1. Time-Slotted Discovery (Why divide by 120?)</strong> (Click to Expand)</summary>

In a Distributed Hash Table (DHT), records usually stay alive for 24 hours. This creates "Ghost Peers"—users who quit hours ago but still appear in search results.

**The Solution:** We implement **Temporal Sharding**.
*   We take the current Unix Timestamp and divide it by `120` (2 minutes).
*   Room Code `808` becomes Topic `grid-lobby-808-14166666`.
*   Every 120 seconds, the math result changes by +1, effectively creating a **new room**.
*   When you search, the code looks for the `Current Window` and the `Previous Window` (to catch people who joined 10 seconds ago).
*   **Result:** Old sessions are mathematically impossible to find.
</details>

<details>
<summary><strong>2. The "Airlock" Mechanism (NAT Traversal)</strong> (Click to Expand)</summary>

This is the project's strict gatekeeper.
*   **Relay vs. Direct:** Usually, P2P falls back to a "Relay" (proxy) if a direct connection fails. Relays are slow.
*   **The Rule:** The Grid allows discovery over relays, but **blocks gameplay** until a Direct Connection (Hole Punch) is verified.
*   **DCUtR:** We use *Direct Connection Upgrade through Relay*. The peers coordinate via a relay to synchronize opening their firewalls simultaneously.
</details>

<details>
<summary><strong>3. State Synchronization</strong> (Click to Expand)</summary>

*   **Host-Authoritative:** To prevent cheating, one peer is the "Boss".
*   The peer with the "lower" alphabetical Peer ID automatically becomes the Host.
*   **Protocol:**
    *   Client sends `InputPacket` (Key Presses).
    *   Host updates logic.
    *   Host broadcasts `StatePacket` (World Position).
</details>

---

## 🚀 How to Demo / Test

### Scenario: The "Real" Test (NAT Traversal)
*Requires two different devices (e.g., Laptop on WiFi vs Laptop on Phone Hotspot).*

1.  **Launch:** Run the application on both computers.
2.  **Wait:** Allow the bootstrap to finish (Status: `[ OK ]`).
3.  **Connect:** Enter the same Room Code (e.g., `555`) on both.
4.  **Observe the Airlock:**
    *   You will see the **Timeout Timer** counting down from 120s.
    *   Wait for `Direct Link` to turn `[ DIRECT ]` (Green).
5.  **Fail Condition:** If the timer hits `0s`, the session is scrubbed. Press Enter to try again.

---

## 🔧 Troubleshooting

**1. "It timed out after 120 seconds"**
*   This means the DHT propagation was too slow or the peers couldn't find each other.
*   **Fix:** Ensure both players are entering the code at roughly the same time (within 1-2 minutes of each other). Just press Enter to retry.

**2. "Stuck on [ WAITING ] for Direct Link"**
*   The peers found each other via Relay, but the Hole Punch failed.
*   If this persists for >1 minute, one of the routers likely has **Symmetric NAT**, which is very hard to punch through without a TURN server (which we intentionally don't use to keep this serverless).

---

## 📦 Installation

Requires **Go 1.21+**.

```bash
# 1. Clone the repo
git clone https://github.com/IgorBayerl/the-grid-p2p.git
cd the-grid

# 2. Tidy dependencies
go mod tidy

# 3. Run
go run main.go
```

**Build:**
```bash
go build -o game.exe main.go
```
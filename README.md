***

# ⚡ The Grid P2P
### Serverless, Terminal-Based P2P Multiplayer Demonstrator

**The Grid** is a proof-of-concept "Social Gaming SDK" built in Go. It proves that you can create a low-latency, realtime multiplayer experience without renting a single server or database.

It combines **libp2p** (networking), **DHTs** (discovery), and **Bubble Tea** (UI) to create a terminal game that forces direct connections between players, bypassing central relays to ensure low latency.

---

## 📖 In Simple Terms: What is this?

Most multiplayer games work like a conference call: everyone connects to a central server (the "host"), and that server passes messages around. If the server goes down, the game ends.

**The Grid works like a Walkie-Talkie:**
1.  **Discovery (The Phonebook):** We use the public IPFS network (DHT) to find other players using a "Room Code". We don't have a database of rooms; the network *is* the database.
2.  **Connection (The Handshake):** Once we find a peer, we try to connect. Because most home routers block incoming connections (Firewalls/NAT), we use a technique called "Hole Punching" to trick the routers into letting us talk.
3.  **The "Airlock" (The Guard):** The game refuses to start if the connection is slow (relayed). It forces a direct, high-speed link.
4.  **The Game:** One player acts as the "Host" (authority) and the other as the "Client". They exchange movement data directly.

---

## 🎯 What does this project test?

This is not just a game; it is a networking testbench. It validates three specific challenges:

1.  **NAT Traversal (Hole Punching):** Can two computers on different private networks (e.g., Home WiFi vs. Phone Hotspot) talk directly without a middleman?
2.  **Decentralized Discovery:** Can we find a specific peer out of thousands of nodes in the global swarm without a central server?
3.  **State Synchronization:** Can we keep two terminals in sync using a Host-Authoritative model over a raw P2P stream?

---

## 🛠 Architecture & How It Works

<details>
<summary><strong>1. The "Serverless" Philosophy (Discovery)</strong> (Click to Expand)</summary>

*   **No Database:** We do not store "Lobby IDs" anywhere.
*   **DHT & GossipSub:** When you enter a Room Code (e.g., `829104`), the app hashes it to create a unique topic on the global IPFS Distributed Hash Table.
*   **Bootstrapping:** On startup, your node connects to "Bootnodes" (public servers run by Protocol Labs). This takes 10-20 seconds. Once connected, you are part of the global swarm.
</details>

<details>
<summary><strong>2. The "Airlock" Mechanism (NAT Traversal)</strong> (Click to Expand)</summary>

This is the project's strict gatekeeper.
*   **Relay vs. Direct:** Usually, P2P falls back to a "Relay" (proxy) if a direct connection fails. Relays are slow.
*   **The Rule:** The Grid allows discovery over relays, but **blocks gameplay** until a Direct Connection (Hole Punch) is verified.
*   **DCUtR:** We use *Direct Connection Upgrade through Relay*. The peers coordinate via a relay to synchronize opening their firewalls simultaneously.
</details>

<details>
<summary><strong>3. Game Sync & Topology</strong> (Click to Expand)</summary>

*   **Host-Authoritative:** To prevent cheating and de-sync, one peer is the "Boss".
    *   The peer with the "lower" alphabetical Peer ID automatically becomes the Host.
*   **The Loop:**
    1.  **Client** presses arrow key -> Sends `PacketInput` (DX/DY).
    2.  **Host** receives input -> Updates X/Y coordinates in memory.
    3.  **Host** broadcasts `PacketState` (All Player Positions) to everyone.
    4.  **Client** receives State -> Renders the screen.
*   **Heartbeat:** A background loop pings every second. If the connection drops, the UI shows a "Broken Heart" icon.
</details>

---

## 🚀 How to Demo / Test (Step-by-Step)

To see the magic happen, you need to simulate two distinct peers.

### Scenario A: Localhost Testing (Logic Test)
*Useful for testing game mechanics and UI flow.*

1.  **Open Terminal 1 (Host-to-be):**
    ```bash
    go run main.go
    ```
2.  **Open Terminal 2 (Client-to-be):**
    ```bash
    go run main.go
    ```
3.  **Wait for Bootstrap:** Both terminals will show a loading bar. Wait until the text says `Global Swarm Uplink ..... [ OK ]`.
4.  **Join Lobby:**
    *   In Terminal 1, type a code (e.g., `123`) and press Enter.
    *   In Terminal 2, type the **same** code (`123`) and press Enter.
5.  **The Airlock:**
    *   You will see `Negotiating...`.
    *   Since this is localhost, the connection will instantly become `🟢 DIRECT LINK ESTABLISHED`.
6.  **Play:**
    *   Press `Enter` to start.
    *   One window will say **HOST (Green @)**, the other **CLIENT (Red X)**.
    *   Move around. Watch the lag (should be near zero).

### Scenario B: The "Real" Test (NAT Traversal)
*This tests the hole-punching capability. Requires two different devices.*

**Prerequisites:**
*   Computer A (e.g., on Home WiFi).
*   Computer B (e.g., tethered to a Phone Hotspot / different WiFi).

1.  **Launch:** Run the application on both computers.
2.  **Wait:** Allow the bootstrap to finish (approx 15-30s).
3.  **Connect:** Enter the same Room Code on both.
4.  **Observe the Airlock (The Critical Part):**
    *   Initially, status will be `🟡 WAITING...` or `🟡 NEGOTIATING...`.
    *   The logs will show `Relay Handshake`.
    *   **Wait:** It might take 5-15 seconds for the "Hole Punch" to succeed.
    *   **Success:** The status changes to `🟢 DIRECT LINK ESTABLISHED`.
5.  **Fail Condition:** If it stays Yellow for >60 seconds, your router has a "Symmetric NAT" which is difficult to punch through without a TURN server (which we intentionally don't use).

---

## 🕹 Controls & UI

| Key                    | Action                         |
|:-----------------------|:-------------------------------|
| **Up/Down/Left/Right** | Move Character                 |
| **Enter**              | Confirm Room Code / Start Game |
| **Backspace**          | Delete Room Code character     |
| **Q** or **Ctrl+C**    | Quit Application               |

**UI Indicators:**
*   ❤️ **Heart:** Connection is healthy (Direct P2P).
*   💔 **Broken Heart:** Connection lost (Peer disconnected or timed out).
*   **Log Window:** Shows raw packet events (Input Sent, State Received).

---

## 🔧 Troubleshooting

**1. "It's stuck on Searching..."**
*   The IPFS DHT is vast. Sometimes it takes a moment to propagate the room topic.
*   **Fix:** Restart both nodes. Ensure you wait for the "Global Swarm Uplink" to be `[ OK ]` *before* entering the lobby code.

**2. "Peers connected, but game won't start"**
*   Check the Airlock status. If it is not `DIRECT`, the game prevents starting to ensure quality.
*   If you are on a corporate network or university WiFi, UDP hole punching might be blocked.

**3. "Bad Packet" logs**
*   This usually happens if an old version of the client tries to talk to a new version. Ensure both peers are running the exact same binary.

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

Generate Build
```bash
go build -o game.exe main.go
```
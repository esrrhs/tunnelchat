# Tunnelchat (Go Edition)

[English](README.md) | [中文](README_zh.md)

**Tunnelchat** is a decentralized, peer-to-peer (P2P) command-line chat application powered by STUN NAT traversal and end-to-end encryption. Built with a modern pure Go core, it operates without legacy GUI frameworks or external C++ dependencies, offering a clean, lightweight, out-of-the-box interactive terminal chat experience on Linux.

---

## Features

* **Serverless Architecture**: Pure P2P topology; all communications are directly peer-to-peer without central relay servers.
* **STUN NAT Traversal**: Pure Go RFC 3489 / RFC 5389 implementation for public IP mapping and NAT type discovery.
* **Automatic LAN / Offline Fallback**: Intelligently falls back to local network IPs when STUN servers are unreachable, ensuring smooth operation across LAN and WAN environments.
* **End-to-End Encryption**: End-to-end encrypted payload transmission using DES-ECB + MD5 key derivation with mutual negotiation per peer.
* **Heartbeat & Hole Punching**: Periodic UDP heartbeats keep NAT mappings open and sustain connectivity.
* **Dynamic Status Synchronization**: Automatically tracks and synchronizes friend IP and port changes.
* **Interactive CLI**: Rich terminal commands for viewing friends, real-time messaging, status inspection, and connection token exchange.

---

## Quick Start

### 1. Build from Source

Requires Go 1.20 or later:

```bash
# Using Makefile
make build

# Or using the build script
./build.sh

# Run unit tests
make test
```

The compiled binary will be placed at `bin/tunnelchat`.

---

### 2. Command Reference

```bash
tunnelchat <command> [arguments]
```

| Command | Arguments | Description |
| :--- | :--- | :--- |
| `new` | `<name> <password>` | Create or reset a user account |
| `info` | None | Export encrypted connection string to share with friends |
| `add` | `<info_string>` | Add a friend using their connection string and initiate handshake |
| `online` | None | Launch interactive CLI chat mode |

---

### 3. Usage Walkthrough

#### Step 1: Create Accounts

User Alice:
```bash
./bin/tunnelchat new Alice 123456
```

User Bob:
```bash
./bin/tunnelchat new Bob 654321
```

#### Step 2: Share Connection Info and Add Friend

Alice retrieves her connection string:
```bash
./bin/tunnelchat info
```
Output:
```
My Information:
  Name: Alice
  Account: 4B3C18DE30FDE73A393D43E085B88350
  Address: 162.62.119.173:53714

Share this info string with your friends to add you:
98F8E4DED6E5EEF52D81640FCA8135EF3960FEF370972992...
```

Bob adds Alice:
```bash
./bin/tunnelchat add 98F8E4DED6E5EEF52D81640FCA8135EF3960FEF370972992...
```

Similarly, Alice adds Bob's connection string to establish mutual key negotiation and authorization.

#### Step 3: Start Interactive Chat

Launch interactive chat mode:
```bash
./bin/tunnelchat online
```

Inside the interactive terminal:
* **Send message**: Type `<FriendName> <Message>` (e.g., `Bob Hi Bob, how are you?`)
* **List friends**: Type `/friends` or `/list`
* **Show user info**: Type `/info`
* **Add friend online**: Type `/add <info_string>`
* **Exit**: Type `q` or `quit`

---

## Project Structure

```
.
├── cmd/
│   └── tunnelchat/     # CLI entry point
├── pkg/
│   ├── cli/            # Interactive terminal UI and CLI logic
│   ├── config/         # XML configuration and friend/account management
│   ├── core/           # P2P engine (UDP transport, RPC, heartbeats, dispatch)
│   ├── crypto/         # Cryptographic primitives (DES, MD5, GUID)
│   └── stun/           # Pure Go STUN client and NAT discovery
├── bin/                # Compiled binary output directory
├── build.sh            # Build script
├── Makefile            # Makefile targets
├── README.md           # English documentation (main)
└── README_zh.md        # Chinese documentation
```
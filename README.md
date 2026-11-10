# Tunnelchat (v2)

[English](README.md) | [中文](README_zh.md)

**Tunnelchat v2** is a command-line peer-to-peer chat tool. Peers pin each other's long-term identities, discover optional endpoint hints, probe candidates in both directions, and then run a **Noise_IK** handshake (X25519 + ChaCha20-Poly1305 + SHA-256, with Ed25519 identity binding). There is no silent fallback to the legacy DES/MD5 protocol.

This is a local, still-evolving implementation. It has been tested with **loopback and a userspace NAT/STUN/DHT/relay emulator**, not on real dual-NAT or CGNAT paths. Do not treat successful unit tests as a guarantee of traversal on the public Internet.

---

## What v2 changes

* **Identity:** a random 32-byte seed derives Ed25519 (signing / pinning) and X25519 (Noise static). Passwords are not used as key material.
* **Sessions:** `Noise_IK_25519_ChaChaPoly_SHA256` with a fixed prologue (`tunnelchat-v2-noise-ik`). Transport nonces use a sliding replay window. Tampered or replayed packets are dropped. DHT-advertised addresses are untrusted until a pinned probe and handshake succeed.
* **Shared UDP socket:** STUN, optional DHT, probes, sessions, and optional relay framing share one socket so the **STUN mapped port is the chat port**. Mapped addresses are refreshed periodically; a saved IP is not treated as permanent.
* **Candidates:** local IPv4/IPv6 host addresses plus STUN server-reflexive addresses. Invites are out-of-band and may include LAN hints. **Public DHT records never carry raw private addresses**; optional BEP 44-style hints are encrypted and then stripped to public endpoints only.
* **DHT:** `online` **enables Mainline DHT discovery by default** (documented public bootstraps; override or disable below). **DHT is discovery only**, not a chat relay and not a hole-punch coordinator. Chat payloads stay end-to-end encrypted (Noise/AEAD). Going online advertises your public IP to bootstrap and DHT nodes. A well-known v2 swarm plus **Ed25519-signed presence** (name bound to the key) lets you list other currently-online users as *strangers* — they are **never auto-added** to the trusted friend list. Raw DHT IPs are not trusted as identities. Unsigned, stale, or forged presence is ignored. Private addresses are not published in plaintext public records.
* **Relay:** optional, operator-run UDP forwarder. Payloads stay opaque. Access is token-MAC'd, allocations are bounded and idle-expired. Default listen address is loopback. This is not a required central server.

---

## Incompatibility and migration

v2 **cannot talk to v1** (DES-ECB / MD5 / `tunnelchat.xml`) clients. Existing v1 XML files are **detected and left untouched**. `tunnelchat new` refuses to overwrite an existing v2 file and refuses `.xml` paths. Create a new identity in a new JSON path (default `tunnelchat.json`). There is no automatic import of old accounts or friend keys.

---

## Build

Requires Go 1.26 or later:

```bash
make build     # bin/tunnelchat
make test      # unit + local integration tests
```

Tests use in-process fake STUN/DHT/relay and a userspace NAT. They do **not** announce to the public Mainline DHT.

---

## Usage

```bash
# Create identities (the optional leftover password argument is ignored)
./bin/tunnelchat -c alice.json new Alice
./bin/tunnelchat -c bob.json new Bob

# Print a signed invite (may briefly bind a socket and query STUN; no DHT announce)
./bin/tunnelchat -c alice.json info

# Pin the other identity (both sides should add each other)
./bin/tunnelchat -c bob.json add 'TC2INV1....'

# Online at the same time (no offline inbox)
./bin/tunnelchat -c alice.json online
./bin/tunnelchat -c bob.json online
```

Inside `online`: `<Name> <message>`, `/friends`, `/peers` (or `/who`), `/connect <fingerprint>`, `/addpeer <fingerprint>`, `/info`, `/add <invite>`, `q`.

### DHT discovery (on by default for `online`)

```bash
# Default: contact public Mainline bootstraps and join the v2 public swarm
./bin/tunnelchat -c alice.json online

# Disable DHT entirely
./bin/tunnelchat -c alice.json --no-dht online

# Or in the JSON config: { "dht": { "disabled": true } }

# Override bootstraps (e.g. a local node); this replaces the default list
./bin/tunnelchat -c alice.json --dht-bootstrap 127.0.0.1:6881 online
```

Default bootstrap routers (documented, used only when no override is set):

* `router.bittorrent.com:6881`
* `dht.transmissionbt.com:6881`
* `router.utorrent.com:6881`
* `dht.libtorrent.org:25401`

`info` still does **not** announce to the DHT. Tests never apply this default list, so unit tests do not contact the public network.

#### Finding strangers

While both people are `online` with DHT on, `/peers` or `/who` lists discovered users: display name (signed together with their long-term Ed25519 key) and fingerprint. Anyone can choose any display name; **compare fingerprints** before trusting a name. Presence is short-lived (about five minutes) and may be missing or polluted.

* `/connect <fingerprint>` starts the normal encrypted handshake. It does **not** add them as a trusted friend.
* `/addpeer <fingerprint>` pins them to your friend list (explicit).
* Invites (`/add TC2INV1...`) still work for out-of-band introductions.

This is not anonymous. Other DHT users can see that a tunnelchat client is online at your public IP.

### Optional relay

```bash
./bin/tunnelchat relay --listen 127.0.0.1:9000 --token 'long-random-secret'
./bin/tunnelchat -c alice.json --relay-server 127.0.0.1:9000 --relay-token 'long-random-secret' online
```

The relay operator can see IP addresses and timing, not message plaintext. Do not run an unattended public relay unless you intend to.

### Flags

| Flag | Meaning |
| :--- | :--- |
| `-c path` | JSON config (mode 0600). Default `tunnelchat.json`. |
| `--no-dht` | Disable DHT discovery and public swarm announce. |
| `--dht-bootstrap host:port` | Repeatable. Replaces the default public bootstrap list. |
| `--no-stun` | Skip STUN (LAN or tests). |
| `--relay-server`, `--relay-token` | Opt-in relay fallback. |

---

## Limitations (read these)

* **NAT / “sub-LAN” traversal is not guaranteed.** Same subnet works when LAN candidates or working hairpin exist. Different subnets need routing and open UDP. Guest isolation, double NAT, CGNAT, and symmetric NAT often fail without a relay. Simultaneous probing is implemented; there is **no port prediction**. Real dual-NAT/CGNAT was **not** tested in this tree.
* **Going `online` with DHT on advertises your public IP** to bootstrap/DHT nodes and to other users who look up the public swarm. This is not an anonymity tool. Chat content stays end-to-end encrypted; metadata and IPs do not.
* **Stranger names are not unique.** The signature binds a name to a key, but anyone can label themselves "Alice". Compare fingerprints.
* **DHT records expire, can be missing, or can be polluted.** Presence TTL is short. DHT still does not punch NAT; hard NAT may still need a relay.
* **No offline delivery.** Both peers must be `online` at the same time.
* **IPv6:** host candidates are gathered; the primary listener is IPv4 (`udp4`). IPv6-only receive is incomplete.
* **Invite privacy:** an invite contains identity keys, a rendezvous secret, and optional LAN endpoints. Treat it like a capability. Do not paste it into public DHT records or public forums.
* **Crypto:** Noise_IK + Ed25519 pinning replaces DES/MD5. Keep the JSON seed file private (`0600`).
* **DHT storage** (including signed mutable items) is best-effort, size-limited, and short-lived. Not all Mainline nodes implement BEP 44.

---

## Project layout

```
cmd/tunnelchat/     CLI entry
pkg/identity/       Seed, Ed25519, X25519
pkg/session/        Noise_IK sessions, replay window
pkg/stun/           Shared-socket STUN client + test server
pkg/dht/            Local/Mainline-compatible BEP 5 + signed mutable items
pkg/probe/          Signed bidirectional probes
pkg/relay/          Optional opaque UDP relay
pkg/candidate/      Host / srflx classification
pkg/core/           Engine
pkg/netsim/         Userspace NAT for tests
```

Test App: Dummy Two Sequencers

This test app provides a dummy sequencer process that connects to the shared publisher leader app to exercise 2PC end-to-end over TCP with a one-time ECDSA handshake. You run two identical sequencer processes with different chain IDs.

Prerequisites
- Go 1.24+
- Shared publisher leader app available (this repo)

Quick Start
1) In one terminal, run the shared publisher leader app (auth enabled in config):
   - make run-dev
     or
   - go run ./shared-publisher-leader-app --log-pretty --metrics

2) In another terminal, run two sequencer instances (same binary):
   - go run ./test-app --chain-id 1 --initiate --private-key 3afc6aa26dcfb78f93b2df978e41b2d89449e7951670763717265ab0a552aae0 --sp-pub 034f2a8d175528ed60f64b7c3a5d5e72cf2aa3acda444b33e16fdfb3e3e4326ce5
   - go run ./test-app --chain-id 2 --private-key 1e33f16449a0b646f672b0a5415bed21310d388effb7d3b95816d1c12c492f74 --sp-pub 034f2a8d175528ed60f64b7c3a5d5e72cf2aa3acda444b33e16fdfb3e3e4326ce5

What it does
- Two sequencers (chain IDs 0x01 and 0x02) connect to SP :8080
- The initiator sends an XTRequest that includes both chains
- Both vote commit; SP reaches commit and broadcasts Decided
- After commit, each sequencer submits a dummy Block including the XT ID

Optional: One-shot runner script
- ./scripts/run-test-app.sh
  - Starts the shared publisher, runs two sequencers (chain-id 1 and 2), then cleans up

Flags
- --sp-addr: shared publisher address (default: localhost:8080)
- --chain-id: chain id byte as integer (default: 1)
- --initiate: whether this sequencer initiates the XT
- --private-key: hex ECDSA private key; default keys are used for chain-id 1/2 if omitted
 - --sp-pub: compressed (33B hex) SP public key; optional (see Auth model)

Auth configuration
- shared-publisher-leader-app/configs/config.yaml has auth.enabled: true and lists trusted sequencers:
  - id: seq-1 -> public key 02524a41...f124c
  - id: seq-2 -> public key 02b4e1e4...4decb
- The SP private key is configured and used for identity; per-message signatures are NOT used.

Authentication model (handshake-only)

Overview
- A one-time ECDSA handshake runs immediately after TCP connect. After a successful handshake, the server tags the connection with the verified client ID. Per-message signatures were removed.
- Server verifies handshake and strictly rejects clients whose public keys are not in its trusted set.
- Client currently does not authenticate the server in this harness (adding SP pubkey on the client is optional and reserved for future verification features).

Handshake details
- Request (client -> server): timestamp (ns), 16B nonce, client compressed public key (33B), and signature over concat(timestamp||nonce).
- Response (server -> client): accepted flag and a session_id. Allowed clock drift: ±30s; outside that the server rejects the handshake.

Generate Keys (dev)
- You can generate a dev set of keys using:
  - go run ./scripts/gen-keys.go
- This prints private keys and compressed public keys (33-byte, hex). Use private keys for processes and compressed public keys for trust lists.

Configure the Shared Publisher (server)
- In shared-publisher-leader-app/configs/config.yaml set:
  - auth.enabled: true
  - auth.private_key: SP private key (hex)
  - auth.trusted_sequencers: list of sequencer IDs and their compressed public keys (hex, 33-byte)
- Example entries are provided in the repo by default for seq-1 and seq-2.

Run Sequencers (clients)
- Pass each sequencer its own private key using --private-key; SP must trust these public keys or the connection is rejected at handshake.
- Optional: pass the SP compressed public key via --sp-pub to add it to the client’s trusted list (reserved for future mutual auth/per-message verification; not required for this harness).

What “verified” means here
- Verified on the server means: handshake signature is valid and the client’s public key is trusted; otherwise, the server rejects the connection.

Things to watch out for
- Public key format: Use compressed public keys (33 bytes), not uncompressed (65 bytes) and not addresses.
- Key mismatch: If verified_id remains unknown:<prefix>, ensure the SP public key passed with --sp-pub matches the SP private key loaded by the leader.
- Trusted IDs: The “id” strings in the SP config (e.g., seq-1) are labels bound to compressed public keys. The keys are what matter for admission.
- Secrets: Never commit real private keys; the defaults in this repo are for dev only.
- Chain IDs vs identity: Chain ID bytes in messages are separate from auth identities/keys; don’t conflate them.
- Clock skew: Ensure client clocks are within ±30s of the server or the handshake is rejected.

Verifying in logs
- Client: “Handshake successful” followed by “Connected to server”.
- Server: “Client authenticated” with verified_id and session_id; subsequent messages log sender as the verified client ID.

Runner script
- ./scripts/run-test-app.sh waits for SP health on http://localhost:8081/health (not a raw TCP probe) to avoid false handshake EOFs.

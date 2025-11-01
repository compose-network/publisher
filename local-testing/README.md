# Local Publisher Testing Guide

This guide walks through launching the Shared Publisher locally and exercising the critical message
 flows (authentication handshake, submitting cross-chain transaction requests, and sending votes) with the lightweight sequencer client provided in `local-testing/sequencer-client`.

## 1. Prerequisites

- Go 1.24 or newer (matches `go.mod`)
- `make`
- `curl` and `jq` (helpful for the HTTP endpoints)
- Optional: a writable `$GOCACHE` location if you run `go run` repeatedly

All commands below assume you are in the repository root (`/Users/matheusfranco/Documents/bloxapp/compose/publisher`).

## 2. Build the Publisher Binary

```bash
make build
```

The binary is written to `bin/publisher`.

## 3. Prepare a Local Configuration

1. Start from the sample configuration:
   ```bash
   cp publisher-leader-app/configs/config.example.yaml publisher-leader-app/configs/config.local.yaml
   ```

2. Edit `publisher-leader-app/configs/config.local.yaml` and adjust the following for local use:

   ```yaml
   server:
     listen_addr: ":8089"

   api:
     listen_addr: ":8081"

   metrics:
     enabled: true
     port: 8081
     path: /metrics

   l1:
     enabled: false          # Skip L1 publishing for local testing

   proofs:
     enabled: false          # Disable prover pipeline locally
     require_proof: false

   # Enable only if you want authenticated handshakes
   auth:
     enabled: true
     private_key: "<SP_PRIV_HEX>"
     trusted_sequencers:
       - id: "seq-11155111"
         public_key: "<SEQ1_PUB_HEX>"
       - id: "seq-84532"
         public_key: "<SEQ2_PUB_HEX>"
   ```

3. When `auth.enabled: true`, generate a throw-away key set and record the values for later steps:

   ```bash
   go run ./scripts/gen-keys.go
   ```

   Example output:

   ```
   SP_PRIV=6f7e...d1
   SP_PUB=03ab...ff
   SP_ADDR=0x...

   SEQ1_PRIV=3afc...ae0
   SEQ1_PUB=034f...ce5
   SEQ1_ADDR=0x...

   SEQ2_PRIV=1e33...2f74
   SEQ2_PUB=0289...1aa
   SEQ2_ADDR=0x...
   ```

   - Place `SP_PRIV` in `auth.private_key`.
   - Copy the sequencer public keys into `auth.trusted_sequencers`.
   - Keep the sequencer private keys handy; you will use them with the helper client.
   - The `id` you choose (`seq-11155111`, etc.) is what you should pass through `--client-id` when running the helper.

> **Tip:** If you do not need authentication for a quick smoke test, set `auth.enabled: false` and omit the private keys. The helper client can then be run without `--private-key`.

## 4. Launch the Shared Publisher

```bash
./bin/publisher \
  --config publisher-leader-app/configs/config.local.yaml \
  --log-pretty \
  --metrics
```

Useful checks in another terminal:

```bash
curl -sf http://127.0.0.1:8081/health
curl -sf http://127.0.0.1:8081/ready
curl -sf http://127.0.0.1:8081/stats | jq .
curl -sf http://127.0.0.1:8081/metrics | head
```

Leave the publisher running while you interact with it.

## 5. Helper Sequencer Clients

The one-shot helper lives in `local-testing/sequencer-client` and speaks the same TCP protocol as a real sequencer.
Every invocation establishes a fresh connection, optionally performs the ECDSA handshake, sends the requested message, waits for responses (controlled by `--wait`), and disconnects.
For multi-step flows, use the YAML-driven client described below.

- `--sp-addr`: TCP endpoint of the publisher (default `127.0.0.1:8089`)
- `--client-id`: Sequencer identifier (match `auth.trusted_sequencers[*].id` when auth is enabled)
- `--private-key`: Hex private key to sign the handshake (omit when auth is disabled)
- `--chain-id`: Sequencer chain id (decimal or `0x` hex, parsed as `uint64`)
- `--instance-id`: Hex-encoded instance identifier (required for `send-vote`)
- `--wait`: How long to keep the connection open after sending the payload (default `2s`)

Run all commands below from the repository root.

### 5.1 Handshake Only

Verifies that authentication is wired correctly.

```bash
go run ./local-testing/sequencer-client \
  --action handshake \
  --sp-addr 127.0.0.1:8089 \
  --client-id seq-11155111 \
  --wait 0 \
  --private-key "$SEQ1_PRIV"
```

A successful run logs `handshake completed successfully` and the publisher logs should show the connection.

### 5.2 Submit an XT Request

This queues a cross-chain transaction request (with random content) at the publisher.

```bash
go run ./local-testing/sequencer-client \
  --action submit-xt \
  --sp-addr 127.0.0.1:8089 \
  --client-id seq-11155111 \
  --chain-id1 11155111 \
  --chain-id2 84532 \
  --wait 5s \
  --private-key "$SEQ1_PRIV"
```

- `--chain-id` accepts decimal or `0x`-prefixed hex and is stored as a `uint64`.
- Watch the publisher logs (or the helper output) for the `StartInstance` message that follows. It contains the `instance_id` used when sending votes.

### 5.3 Send a Vote

Use the `instance_id` reported in the `StartInstance` message (hex-encoded) and indicate commit or abort.

```bash
go run ./local-testing/sequencer-client \
  --action send-vote \
  --sp-addr 127.0.0.1:8089 \
  --client-id seq-11155111 \
  --private-key "$SEQ1_PRIV" \
  --chain-id 11155111 \
  --instance-id 0x<INSTANCE_ID_FROM_LOGS> \
  --vote true \
  --wait 3s
```

Accepted vote values: `true`, `false`, `commit`, `abort`, `1`, or `0`.

### 5.4 Send a Ping (Connection Health)

```bash
go run ./local-testing/sequencer-client \
  --action send-ping \
  --sp-addr 127.0.0.1:8089 \
  --client-id seq-11155111 \
  --private-key "$SEQ1_PRIV" \
  --wait 0
```

The helper logs the `pong` reply emitted by the publisher.

### 5.5 Running Multiple Sequencers

Repeat the commands with the second sequencer identity and key:

```bash
go run ./local-testing/sequencer-client \
  --action submit-xt \
  --sp-addr 127.0.0.1:8089 \
  --client-id seq-84532 \
  --private-key "$SEQ2_PRIV" \
  --chain-id 84532 \
  --xt-payload 0xbeadface
```

Ensure the second public key is present in `auth.trusted_sequencers`.

### 5.6 YAML-Driven Workflow Client

For longer interactive flows (for example, submit an XT request, wait for a `StartInstance`, then issue a vote) use the workflow helper in `local-testing/sequencer-client-workflow`. It reuses the same transport stack but executes a sequence of actions defined in a YAML file.

1. Start from the sample configuration:
   ```bash
   cp local-testing/sequencer-client-workflow/example.yaml /tmp/workflow.yaml
   ```

2. Adjust `/tmp/workflow.yaml`:

   ```yaml
   sp_addr: 127.0.0.1:8089
   client_id: seq-11155111
   wait_window: 5s
   actions:
     - type: submit-xt
       chains:
         - chain_id: "11155111"
           random_bytes: 128
         - chain_id: "84532"
           random_count: 2
     - type: send-vote
       chain_id: "11155111"
       vote: true
       timeout: 10s
     - type: send-vote
       chain_id: "84532"
       vote: true
       timeout: 10s
     - type: wait
       duration: 30s
   ```

   - `sp_addr`, `client_id`, and `private_key` line up with the CLI flags from the single-shot helper.
   - Each entry under `actions` is executed in order. Supported types are `submit-xt` (send an XT request, optionally overriding per-chain transaction payloads), `wait` (sleep for a duration), and `send-vote`.
   - When `send-vote` omits `instance_id`, the helper blocks until a `StartInstance` message arrives and reuses its `instance_id`. Override `timeout` to adjust how long it waits.

3. Run the workflow:

   ```bash
   go run ./local-testing/sequencer-client-workflow \
     --config /tmp/workflow.yaml
   ```

The helper logs each step, incoming messages (including the `StartInstance` used for votes), and keeps the connection open for `wait_window` after the final action.

## 6. Observability Checklist

- Metrics: `http://127.0.0.1:8081/metrics`
- Health: `http://127.0.0.1:8081/health` and `.../ready`
- Stats: `http://127.0.0.1:8081/stats`
- Logs: watch the publisher terminal for slot progression, XT queueing, vote handling, and decisions.

## 7. Clean Up

Stop the publisher with `Ctrl+C`. Any helper client connections terminate automatically once their `--wait` duration elapses.

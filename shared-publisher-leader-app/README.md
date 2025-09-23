## Shared Publisher (Leader App)

This directory contains the executable that runs the Shared Publisher leader. It exposes a TCP server that sequencers connect to and a small HTTP server for health and metrics. Authentication via ECDSA signatures is supported and can be enabled via configuration.

### Build and Run

- Build the binary:
  ```bash
  make build
  ```

- Run with default config:
  ```bash
  ./bin/rollup-shared-publisher
  ```

- Run with a custom config:
  ```bash
  ./bin/rollup-shared-publisher --config shared-publisher-leader-app/configs/config.yaml
  ```

### Configuration

Config is loaded from a YAML file (default: `shared-publisher-leader-app/configs/config.yaml`) and can be overridden by environment variables using upper snake case names with section prefixes.

Minimal example:
```yaml
server:
  listen_addr: ":8080"
  max_connections: 1000
  read_timeout: 30s
  write_timeout: 30s
  max_message_size: 10485760

metrics:
  enabled: true
  port: 8081
  path: "/metrics"

log:
  level: info
  pretty: true
  output: stdout

auth:
  enabled: true
  private_key: "<SP_PRIVATE_KEY_HEX>"
  trusted_sequencers:
    - id: "rollupA"
      public_key: "<SEQ1_PUBLIC_KEY_HEX>"
    - id: "rollupB"
      public_key: "<SEQ2_PUBLIC_KEY_HEX>"

consensus:
  timeout: 60s
  role: leader
```

Environment variable overrides (examples):
- `SERVER_LISTEN_ADDR=":9090"`
- `SERVER_MAX_CONNECTIONS=2000`
- `AUTH_ENABLED=true`
- `AUTH_PRIVATE_KEY=...`

### Authentication

The leader app integrates the `x/auth` module and the TCP transport to verify signed messages:
- When `auth.enabled` is true and `auth.private_key` is provided, the server signs outbound messages and verifies inbound ones.
- `auth.trusted_sequencers` defines allowed sequencer identities by mapping a human-friendly `id` to a compressed public key (33-byte hex). Verified connections will log the "Connection identity established" message with the resolved `verified_id`.

Key generation helper:
```bash
go run ./scripts/generate_keys.go
```

This prints the Shared Publisher private key and compressed public keys for two sample sequencers you can paste into the config.

Expected logs when auth is enabled and sequencers use known keys:
```
INFO Authentication enabled for shared publisher address=0x...
INFO Added trusted sequencer id=rollupA
INFO Added trusted sequencer id=rollupB
INFO Connection identity established verified_id=rollupA
```

If a sequencer connects with an unknown key, messages will still be signature-verified but marked as `unknown:<pubkey-prefix>` unless you enforce reject logic externally.

### HTTP Endpoints

- `GET /health` — liveness
- `GET /ready` — returns `503` until there is at least one connection
- `GET /metrics` — Prometheus metrics (when enabled)
- `GET /stats` — internal stats plus build info

### CLI

```bash
./bin/rollup-shared-publisher --help
```

Flags include `--config`, log tuning, and server/metrics overrides. See `shared-publisher-leader-app/main.go` for details.

### Notes

- See the repository root `README.md` for a broader architecture overview and the SDK usage patterns under `x/`.
- The leader app always runs in leader mode, but the `consensus.role` value is accepted for completeness.

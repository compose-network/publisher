## Compose Publisher

This directory contains the executable that runs the Shared Publisher. It exposes a QUIC server that
sidecars connect to and an HTTP server for health, readiness, and stats. Cross-chain transactions are
coordinated through 2PC consensus from compose-sdk.

### Build and Run

- Build the binary:
  ```bash
  make build
  ```

- Run with default config:
  ```bash
  ./bin/publisher
  ```

- Run with a custom config:
  ```bash
  ./bin/publisher --config compose-publisher/configs/config.yaml
  ```

### Configuration

Configuration is loaded from a YAML file (default: `compose-publisher/configs/config.yaml`) and can be
overridden by environment variables. For example, `server.listen_addr` can be set with `SERVER_LISTEN_ADDR`.

See [`configs/config.example.yaml`](./configs/config.example.yaml) for a fully commented example.

#### Main Sections

- **`server`**: QUIC server for sidecar connections.
- **`api`**: HTTP API server for health, readiness, and stats.
- **`consensus`**: 2PC consensus parameters (timeout, role).
- **`metrics`**: Prometheus metrics endpoint configuration.
- **`log`**: Logging level and format.

```yaml
server:
  listen_addr: ":8080"
  write_timeout: 30s

api:
  listen_addr: ":8081"

consensus:
  timeout: "60s"
  role: "leader"

metrics:
  enabled: true
  port: 8081
  path: /metrics

log:
  level: info
  pretty: false
  output: stdout
```

### HTTP Endpoints

The API server (default `:8081`) exposes:

- **`GET /health`**: Liveness probe. Returns `200 OK` if the service is running.
- **`GET /ready`**: Readiness probe. Returns `200 OK` when sidecars are connected, `503` otherwise.
- **`GET /stats`**: Application statistics and build information.

### CLI

```bash
./bin/publisher --help
```

Flags include `--config`, log tuning, and server/metrics overrides. See `compose-publisher/main.go` for
details.

# HTTP Server (API)

HTTP server for the Shared Publisher and SDK tools. Provides a minimal, production‑ready HTTP surface with Gorilla mux router and structured middleware.

## Architecture

- `server/api`
  - `server.go` – Gorilla `mux.Router` with explicit `net.Listener`, graceful shutdown, middleware chain, timeouts and header limits
  - `config.go` – Listen address, timeouts, max header bytes configuration
  - `response.go` – JSON helpers (`WriteJSON`, `WriteError`)
- `server/api/middleware`
  - `logger.go` – Structured access logs (method, path, status, bytes, latency)
  - `recover.go` – Panic recovery with stack traces

Feature endpoints register onto the shared router from their respective modules:
- `x/superblock/proofs/http/handler.go` – `/v1/proofs/*` routes

## Usage

Initialize server at startup:

```go
cfg := api.DefaultConfig()
s := api.NewServer(cfg, log)
s.Use(middleware.Recover(log))
s.Use(middleware.Logger(log))

// Health/metrics endpoints
s.Router.HandleFunc("/health", handleHealth).Methods("GET")
s.Router.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{})).Methods("GET")

// Module routes
h := proofshttp.NewHandler(collector, log)
h.RegisterMux(s.Router)

// Start with graceful shutdown context
go s.Start(ctx)
```

Stop server by canceling the context passed to `Start(ctx)`.

## Configuration

Server configuration options:

```yaml
api:
  listen_addr: ":8082"
  read_header_timeout: 5s
  read_timeout: 15s
  write_timeout: 30s
  idle_timeout: 120s
  max_header_bytes: 1048576
```

Configuration maps to `server/api.Config`. Application reads config and passes to `NewServer`.

## Testing

Unit test handlers using `httptest`. For integration tests, start server on random port, exercise routes with `http.Client`, then cancel context and verify shutdown behavior.

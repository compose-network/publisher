# Compose Sidecar Testing Guide

## Running Tests

From the `dome/` directory:

```bash
cd dome

# Run mint test (gives tokens to test account)
go test -v -count=1 -run TestMint ./test/ -timeout 60s

# Run bridge test (transfers tokens A→B)
go test -v -count=1 -run TestSendCrossTxBridgeFromAToB ./test/ -timeout 120s

# Run ping-pong test
go test -v -count=1 -run TestPingPong ./test/ -timeout 120s
```

Always run mint before bridge (bridge needs tokens).

## Important Containers

| Container           | Purpose                            |
|---------------------|------------------------------------|
| `compose-sidecar-a` | Sidecar for rollup A (chain 77777) |
| `compose-sidecar-b` | Sidecar for rollup B (chain 88888) |
| `op-rbuilder-a`     | Block builder for rollup A         |
| `op-rbuilder-b`     | Block builder for rollup B         |

## Watching Logs

```bash
# Follow sidecar logs (most useful for debugging)
docker logs -f compose-sidecar-a
docker logs -f compose-sidecar-b

# Follow builder logs (shows tx execution errors)
docker logs -f op-rbuilder-a
docker logs -f op-rbuilder-b

# Grep for specific XT
docker logs compose-sidecar-a 2>&1 | grep "xt-123456"

# Watch for errors only
docker logs op-rbuilder-b 2>&1 | grep -i error | tail -20
```

## Key Log Messages to Look For

**Sidecar logs:**

- `New XT submitted via API` - XT received
- `Processing XT` - simulation starting
- `Mailbox simulation complete` - success=true/false, dependencies count
- `Local vote recorded` - vote=true/false
- `Made local decision` - decision=true/false, put_inbox_txs count
- `Built putInbox transaction` - putInbox tx created (chain B only for bridge)

**Builder logs:**

- `Block added to canonical chain` - block mined with txs
- `Failed to build flashblock` - sidecar tx execution failed
- `failed to execute sidecar transactions` - txs returned by sidecar failed

## Rebuilding After Code Changes

```bash
# Build docker image (from compose-publisher root)
docker build -t compose-sidecar:latest -f compose-sidecar/build/Dockerfile .

# Restart sidecars
docker restart compose-sidecar-a compose-sidecar-b
```

## Common Issues

1. **"execution reverted" with no dependencies** - Usually means missing tokens (run mint first)
2. **"failed to execute sidecar transactions"** - Check putInbox tx format, nonce, or gas
3. **Chain B tx not included** - Check sidecar-b logs for putInbox building and decision
4. **Both votes false** - Check simulation errors on both sidecars

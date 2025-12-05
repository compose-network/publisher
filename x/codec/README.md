# Codec Best Practices

Guidelines for using and extending the `x/codec` module in production.

## Goals

- Efficient, safe protobuf serialization with 4‑byte big‑endian length framing.
- Clear size limits to prevent memory abuse and DoS.
- Simple extensibility via a registry while keeping a secure default.

## Usage

- Default codec: use `NewRegistry().Default()` (Protobuf, 10MB limit).
- Streaming: prefer `EncodeStream`/`DecodeStream` for network I/O to avoid large intermediate buffers.
- Reuse instances: codecs are goroutine‑safe and can be reused across handlers.

## Size Limits

- Set `maxMessageSize` to match transport limits (see `x/transport` configs). Keep it tight (10–50MB typical).
- Always check encode/decode errors; never assume messages fit.
- Reject empty or oversized frames before allocation.

## Performance

- Zero‑copy minded: internal pools reduce allocations; returned `Encode` slices are independent and safe to use.
- Batch higher‑level messages when appropriate to amortize framing overhead.
- Use streaming decode with a buffered reader when reading from sockets.

## Safety & Robustness

- Validate lengths first, then read payloads (`Decode`/`DecodeStream` already enforce this order).
- Treat all input as untrusted: do not bypass size checks; avoid custom unmarshalling without bounds.
- Propagate errors; don’t log protobuf payloads directly (may contain sensitive data).

## Extensibility

- Implement `Codec` (and optionally `StreamCodec`) for alternative formats.
- Register with a unique name in a local `Registry` created via `NewRegistry()`.
- Keep wire‑compatibility stable; avoid changing framing for existing codecs.

## Testing

- Round‑trip tests: `Encode` → `Decode` on representative messages.
- Stream tests: `EncodeStream`/`DecodeStream` through `bytes.Buffer`.
- Boundary tests: empty messages, truncated frames, and `maxMessageSize` exceedance.
- Concurrency sanity: basic parallel round‑trips to ensure safety.

## Interop

- Transport layer (`x/transport/tcp`) uses the same 4‑byte length framing; align `maxMessageSize` across layers.
- Protobuf schema evolution rules apply: use optional/added fields carefully to maintain backward compatibility.

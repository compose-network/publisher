# syntax=docker/dockerfile:1
FROM golang:1.24-alpine AS builder
WORKDIR /src
RUN apk add --no-cache git
COPY services/optimism/go.mod services/optimism/go.sum ./
RUN go mod download
COPY services/optimism /src/optimism
WORKDIR /src/optimism
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /artifacts/op-node ./op-node/cmd
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /artifacts/op-batcher ./op-batcher/cmd
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /artifacts/op-proposer ./op-proposer/cmd

FROM alpine:3.20 AS op-node
RUN apk add --no-cache ca-certificates
COPY --from=builder /artifacts/op-node /usr/local/bin/op-node
ENTRYPOINT ["/usr/local/bin/op-node"]

FROM alpine:3.20 AS op-batcher
RUN apk add --no-cache ca-certificates
COPY --from=builder /artifacts/op-batcher /usr/local/bin/op-batcher
ENTRYPOINT ["/usr/local/bin/op-batcher"]

FROM alpine:3.20 AS op-proposer
RUN apk add --no-cache ca-certificates
COPY --from=builder /artifacts/op-proposer /usr/local/bin/op-proposer
ENTRYPOINT ["/usr/local/bin/op-proposer"]

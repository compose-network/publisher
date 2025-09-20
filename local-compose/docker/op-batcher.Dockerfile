# syntax=docker/dockerfile:1
FROM golang:1.24-alpine AS builder
WORKDIR /src
RUN apk add --no-cache git
COPY services/optimism/go.mod services/optimism/go.sum ./
RUN go mod download
COPY services/optimism /src/optimism
WORKDIR /src/optimism
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /bin/op-batcher ./op-batcher/cmd

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=builder /bin/op-batcher /usr/local/bin/op-batcher
ENTRYPOINT ["/usr/local/bin/op-batcher"]

# syntax=docker/dockerfile:1
ARG OP_DEPLOYER_VERSION=0.3.3
ARG OP_DEPLOYER_TAG=op-deployer/v${OP_DEPLOYER_VERSION}
ARG OP_DEPLOYER_ARCHIVE=op-deployer-${OP_DEPLOYER_VERSION}-linux-amd64.tar.gz

FROM alpine:3.20
ARG OP_DEPLOYER_VERSION
ARG OP_DEPLOYER_TAG
ARG OP_DEPLOYER_ARCHIVE
RUN apk add --no-cache curl ca-certificates
RUN curl -L "https://github.com/ethereum-optimism/optimism/releases/download/${OP_DEPLOYER_TAG}/${OP_DEPLOYER_ARCHIVE}" -o /tmp/op-deployer.tar.gz \
    && tar -xz -C /usr/local/bin -f /tmp/op-deployer.tar.gz op-deployer-${OP_DEPLOYER_VERSION}-linux-amd64/op-deployer \
    && mv /usr/local/bin/op-deployer-${OP_DEPLOYER_VERSION}-linux-amd64/op-deployer /usr/local/bin/op-deployer \
    && rm -rf /usr/local/bin/op-deployer-${OP_DEPLOYER_VERSION}-linux-amd64 /tmp/op-deployer.tar.gz
ENTRYPOINT ["/usr/local/bin/op-deployer"]

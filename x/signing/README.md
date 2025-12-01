# x/signing

Coordinator key management for putInbox transaction signing.

## Overview

The `signing` package manages the coordinator's ECDSA private key used to sign putInbox transactions. The coordinator acts on behalf of sequencers to ensure atomic cross-chain message delivery.

## Usage

```go
import (
    "github.com/compose-network/publisher/x/signing"
    "github.com/ethereum/go-ethereum/crypto"
)

// Generate or load coordinator key
key, _ := crypto.GenerateKey()

// Create signer
signer, err := signing.NewCoordinatorSigner(key, 1) // chainID = 1

// Get coordinator address
address := signer.Address()

// Access private key for geth transaction signing
privateKey := signer.PrivateKey()
```

## CoordinatorSigner

The main component that wraps the coordinator's private key:

```go
type CoordinatorSigner struct {
    key     *ecdsa.PrivateKey
    address common.Address
    chainID uint64
}
```

### Methods

- `Address() common.Address` - Returns coordinator's Ethereum address
- `AddressBytes() []byte` - Returns address as byte slice
- `ChainID() uint64` - Returns configured chain ID
- `PrivateKey() *ecdsa.PrivateKey` - Exposes key for signing (use with caution)

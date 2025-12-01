package signing

import (
	"crypto/ecdsa"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// CoordinatorSigner manages the coordinator's private key for signing putInbox transactions.
type CoordinatorSigner struct {
	key     *ecdsa.PrivateKey
	address common.Address
	chainID uint64
}

// NewCoordinatorSigner creates a signer with the given private key.
func NewCoordinatorSigner(key *ecdsa.PrivateKey, chainID uint64) (*CoordinatorSigner, error) {
	if key == nil {
		return nil, fmt.Errorf("private key is required")
	}

	address := crypto.PubkeyToAddress(key.PublicKey)

	return &CoordinatorSigner{
		key:     key,
		address: address,
		chainID: chainID,
	}, nil
}

// Address returns the coordinator's address.
func (s *CoordinatorSigner) Address() common.Address {
	return s.address
}

// AddressBytes returns the coordinator's address as bytes.
func (s *CoordinatorSigner) AddressBytes() []byte {
	return s.address.Bytes()
}

// ChainID returns the chain ID this signer is configured for.
func (s *CoordinatorSigner) ChainID() uint64 {
	return s.chainID
}

// PrivateKey returns the underlying private key.
func (s *CoordinatorSigner) PrivateKey() *ecdsa.PrivateKey {
	return s.key
}

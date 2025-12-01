package signing

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCoordinatorSigner(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)

	signer, err := NewCoordinatorSigner(key, 1)
	require.NoError(t, err)
	assert.NotNil(t, signer)
	assert.Equal(t, uint64(1), signer.ChainID())
}

func TestNewCoordinatorSigner_NilKey(t *testing.T) {
	signer, err := NewCoordinatorSigner(nil, 1)
	assert.Error(t, err)
	assert.Nil(t, signer)
	assert.Contains(t, err.Error(), "private key is required")
}

func TestCoordinatorSigner_Address(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)

	expectedAddress := crypto.PubkeyToAddress(key.PublicKey)

	signer, err := NewCoordinatorSigner(key, 1)
	require.NoError(t, err)

	assert.Equal(t, expectedAddress, signer.Address())
	assert.Equal(t, expectedAddress.Bytes(), signer.AddressBytes())
}

func TestCoordinatorSigner_AddressBytes(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)

	signer, err := NewCoordinatorSigner(key, 1)
	require.NoError(t, err)

	addrBytes := signer.AddressBytes()
	assert.Len(t, addrBytes, 20)
	assert.Equal(t, signer.Address().Bytes(), addrBytes)
}

func TestCoordinatorSigner_ChainID(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)

	testCases := []uint64{1, 10, 42161, 999999}

	for _, chainID := range testCases {
		signer, err := NewCoordinatorSigner(key, chainID)
		require.NoError(t, err)
		assert.Equal(t, chainID, signer.ChainID())
	}
}

func TestCoordinatorSigner_PrivateKey(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)

	signer, err := NewCoordinatorSigner(key, 1)
	require.NoError(t, err)

	retrievedKey := signer.PrivateKey()
	assert.Equal(t, key, retrievedKey)
}

func TestCoordinatorSigner_DeterministicAddress(t *testing.T) {
	key, err := crypto.HexToECDSA("ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80")
	require.NoError(t, err)

	signer, err := NewCoordinatorSigner(key, 1)
	require.NoError(t, err)

	expectedAddress := common.HexToAddress("0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266")
	assert.Equal(t, expectedAddress, signer.Address())
}

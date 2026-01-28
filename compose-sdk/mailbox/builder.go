package mailbox

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

// putInbox ABI for the Mailbox contract
const putInboxABI = `[{"inputs":[{"internalType":"uint256","name":"chainMessageSender","type":"uint256"},{"internalType":"address","name":"sender","type":"address"},{"internalType":"address","name":"receiver","type":"address"},{"internalType":"uint256","name":"sessionId","type":"uint256"},{"internalType":"bytes","name":"label","type":"bytes"},{"internalType":"bytes","name":"data","type":"bytes"}],"name":"putInbox","outputs":[],"stateMutability":"nonpayable","type":"function"}]`

// PutInboxBuilder builds signed putInbox transactions.
type PutInboxBuilder interface {
	// BuildPutInboxTx builds a signed putInbox transaction for the given dependency.
	BuildPutInboxTx(ctx context.Context, dep CrossRollupDependency) (*types.Transaction, error)
}

// BuilderConfig holds configuration for the PutInboxBuilder.
type BuilderConfig struct {
	ChainID        uint64
	MailboxAddress common.Address
	PrivateKeyHex  string
	RPCURL         string
}

// DefaultBuilder implements PutInboxBuilder using a coordinator private key.
type DefaultBuilder struct {
	chainID        uint64
	mailboxAddress common.Address
	privateKey     *ecdsa.PrivateKey
	client         *ethclient.Client
	abi            abi.ABI
	nonce          uint64
	nonceSet       bool
}

// NewBuilder creates a new PutInboxBuilder.
func NewBuilder(cfg BuilderConfig) (*DefaultBuilder, error) {
	if cfg.PrivateKeyHex == "" {
		return nil, fmt.Errorf("coordinator private key is required")
	}

	privateKey, err := crypto.HexToECDSA(strings.TrimPrefix(cfg.PrivateKeyHex, "0x"))
	if err != nil {
		return nil, fmt.Errorf("invalid private key: %w", err)
	}

	parsedABI, err := abi.JSON(strings.NewReader(putInboxABI))
	if err != nil {
		return nil, fmt.Errorf("parse ABI: %w", err)
	}

	client, err := ethclient.Dial(cfg.RPCURL)
	if err != nil {
		return nil, fmt.Errorf("connect to RPC: %w", err)
	}

	return &DefaultBuilder{
		chainID:        cfg.ChainID,
		mailboxAddress: cfg.MailboxAddress,
		privateKey:     privateKey,
		client:         client,
		abi:            parsedABI,
	}, nil
}

// Address returns the coordinator address.
func (b *DefaultBuilder) Address() common.Address {
	return crypto.PubkeyToAddress(b.privateKey.PublicKey)
}

// BuildPutInboxTx builds a signed putInbox transaction for the given dependency.
func (b *DefaultBuilder) BuildPutInboxTx(ctx context.Context, dep CrossRollupDependency) (*types.Transaction, error) {
	coordinatorAddr := b.Address()

	// Get nonce if not already set
	if !b.nonceSet {
		nonce, err := b.client.PendingNonceAt(ctx, coordinatorAddr)
		if err != nil {
			return nil, fmt.Errorf("get nonce: %w", err)
		}
		b.nonce = nonce
		b.nonceSet = true
	}

	// Encode putInbox call
	sessionID := big.NewInt(0)
	if dep.SessionID != nil {
		sessionID = dep.SessionID
	}

	data, err := b.abi.Pack(
		"putInbox",
		new(big.Int).SetUint64(dep.SourceChainID),
		dep.Sender,
		dep.Receiver,
		sessionID,
		dep.Label,
		dep.Data,
	)
	if err != nil {
		return nil, fmt.Errorf("encode putInbox: %w", err)
	}

	// Build EIP-1559 transaction
	txData := &types.DynamicFeeTx{
		ChainID:   new(big.Int).SetUint64(b.chainID),
		Nonce:     b.nonce,
		GasTipCap: big.NewInt(1000000000),  // 1 gwei
		GasFeeCap: big.NewInt(20000000000), // 20 gwei
		Gas:       500000,
		To:        &b.mailboxAddress,
		Value:     big.NewInt(0),
		Data:      data,
	}

	tx := types.NewTx(txData)

	// Sign transaction with London signer (EIP-1559)
	signer := types.NewLondonSigner(new(big.Int).SetUint64(b.chainID))
	signedTx, err := types.SignTx(tx, signer, b.privateKey)
	if err != nil {
		return nil, fmt.Errorf("sign transaction: %w", err)
	}

	b.nonce++

	return signedTx, nil
}

// ResetNonce resets the nonce tracking (useful after errors).
func (b *DefaultBuilder) ResetNonce() {
	b.nonceSet = false
}

// Close closes the underlying RPC client.
func (b *DefaultBuilder) Close() {
	if b.client != nil {
		b.client.Close()
	}
}

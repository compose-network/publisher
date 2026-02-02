package coordinator

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"strings"

	"github.com/compose-network/compose-sdk/protocol"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/rs/zerolog"
)

// putInbox ABI for the Mailbox contract
const putInboxABI = `[{"inputs":[{"internalType":"uint256","name":"chainMessageSender","type":"uint256"},{"internalType":"address","name":"sender","type":"address"},{"internalType":"address","name":"receiver","type":"address"},{"internalType":"uint256","name":"sessionId","type":"uint256"},{"internalType":"bytes","name":"label","type":"bytes"},{"internalType":"bytes","name":"data","type":"bytes"}],"name":"putInbox","outputs":[],"stateMutability":"nonpayable","type":"function"}]`

// DefaultPutInboxBuilder implements PutInboxBuilder using a coordinator private key.
type DefaultPutInboxBuilder struct {
	log            zerolog.Logger
	chainID        uint64
	mailboxAddress common.Address
	privateKey     *ecdsa.PrivateKey
	rpcURL         string
	client         *ethclient.Client
	abi            abi.ABI
}

// PutInboxBuilderConfig holds configuration for the PutInboxBuilder.
type PutInboxBuilderConfig struct {
	ChainID        uint64
	MailboxAddress common.Address
	PrivateKeyHex  string
	RPCURL         string
	Log            zerolog.Logger
}

// NewPutInboxBuilder creates a new PutInboxBuilder.
func NewPutInboxBuilder(cfg PutInboxBuilderConfig) (*DefaultPutInboxBuilder, error) {
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

	return &DefaultPutInboxBuilder{
		log:            cfg.Log.With().Str("component", "put_inbox_builder").Logger(),
		chainID:        cfg.ChainID,
		mailboxAddress: cfg.MailboxAddress,
		privateKey:     privateKey,
		rpcURL:         cfg.RPCURL,
		client:         client,
		abi:            parsedABI,
	}, nil
}

func (b *DefaultPutInboxBuilder) PendingNonceAt(ctx context.Context) (uint64, error) {
	coordinatorAddr := crypto.PubkeyToAddress(b.privateKey.PublicKey)
	nonce, err := b.client.PendingNonceAt(ctx, coordinatorAddr)
	if err != nil {
		return 0, fmt.Errorf("get nonce: %w", err)
	}
	return nonce, nil
}

// BuildPutInboxTxWithNonce builds and signs a putInbox transaction with the given nonce.
func (b *DefaultPutInboxBuilder) BuildPutInboxTxWithNonce(
	ctx context.Context,
	dep protocol.CrossRollupDependency,
	nonce uint64,
) (*ethtypes.Transaction, error) {

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
	txData := &ethtypes.DynamicFeeTx{
		ChainID:   new(big.Int).SetUint64(b.chainID),
		Nonce:     nonce,
		GasTipCap: big.NewInt(1000000000),  // 1 gwei
		GasFeeCap: big.NewInt(20000000000), // 20 gwei
		Gas:       500000,
		To:        &b.mailboxAddress,
		Value:     big.NewInt(0),
		Data:      data,
	}

	tx := ethtypes.NewTx(txData)

	// Sign transaction with London signer (EIP-1559)
	signer := ethtypes.NewLondonSigner(new(big.Int).SetUint64(b.chainID))
	signedTx, err := ethtypes.SignTx(tx, signer, b.privateKey)
	if err != nil {
		return nil, fmt.Errorf("sign transaction: %w", err)
	}

	b.log.Debug().
		Uint64("source_chain", dep.SourceChainID).
		Str("sender", dep.Sender.Hex()).
		Str("receiver", dep.Receiver.Hex()).
		Str("tx_hash", signedTx.Hash().Hex()).
		Uint64("nonce", nonce).
		Msg("Built putInbox transaction")

	return signedTx, nil
}

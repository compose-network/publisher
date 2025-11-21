package mailbox

import (
	"crypto/ecdsa"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/rs/zerolog"
)

type putInboxBuilder struct {
	chainID  uint64
	selector MailboxSelector
	key      *ecdsa.PrivateKey
	abiSpec  abi.ABI
	log      zerolog.Logger
}

func newPutInboxBuilder(
	chainID uint64,
	selector MailboxSelector,
	key *ecdsa.PrivateKey,
	abiSpec abi.ABI,
	log zerolog.Logger,
) PutInboxTxBuilder {
	return &putInboxBuilder{
		chainID:  chainID,
		selector: selector,
		key:      key,
		abiSpec:  abiSpec,
		log:      log,
	}
}

func (b *putInboxBuilder) BuildPutInboxTx(dep CrossRollupDependency, nonce uint64) (*types.Transaction, error) {
	if b.key == nil {
		return nil, fmt.Errorf("coordinator key is not configured")
	}

	if b.selector == nil {
		return nil, fmt.Errorf("mailbox selector is not configured")
	}

	callData, err := b.abiSpec.Pack("putInbox",
		new(big.Int).SetUint64(dep.SourceChainID),
		dep.Sender,
		dep.Receiver,
		dep.SessionID,
		dep.Label,
		dep.Data,
	)
	if err != nil {
		return nil, err
	}

	mailboxAddr, ok := b.selector.MailboxAddress(b.chainID)
	if !ok {
		return nil, fmt.Errorf("unable to select mailbox addr: no address configured for chain %d", b.chainID)
	}

	txData := &types.DynamicFeeTx{
		ChainID:    new(big.Int).SetUint64(b.chainID),
		Nonce:      nonce,
		GasTipCap:  big.NewInt(1000000000),
		GasFeeCap:  big.NewInt(20000000000),
		Gas:        500000,
		To:         &mailboxAddr,
		Value:      big.NewInt(0),
		Data:       callData,
		AccessList: nil,
	}

	tx := types.NewTx(txData)
	signedTx, err := types.SignTx(tx, types.NewLondonSigner(new(big.Int).SetUint64(b.chainID)), b.key)
	if err != nil {
		return nil, fmt.Errorf("failed to sign tx %v", err)
	}

	b.log.Info().
		Str("tx_hash", signedTx.Hash().Hex()).
		Uint64("nonce", nonce).
		Stringer("session_id", dep.SessionID).
		Str("mailbox", mailboxAddr.Hex()).
		Uint64("source_chain", dep.SourceChainID).
		Str("sender", dep.Sender.Hex()).
		Str("receiver", dep.Receiver.Hex()).
		Int("label_len", len(dep.Label)).
		Int("data_len", len(dep.Data)).
		Stringer("gas_tip_cap", txData.GasTipCap).
		Stringer("gas_fee_cap", txData.GasFeeCap).
		Msg("Created putInbox transaction")

	return signedTx, nil
}

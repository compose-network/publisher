package peer

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/rs/zerolog"
)

// Coordinator handles peer-to-peer sidecar communication.
type Coordinator interface {
	// ForwardXT forwards an XT to peer sidecars for their chains.
	ForwardXT(ctx context.Context, instanceID string, txs map[uint64][][]byte, originSeq uint64) error

	// SendVoteToPeers sends our vote to all peer sidecars.
	SendVoteToPeers(ctx context.Context, instanceID string, chainID uint64, vote bool) error

	// GetPeerChainIDs returns the chain IDs of all peer sidecars.
	GetPeerChainIDs() []uint64

	// Close closes all peer connections.
	Close(ctx context.Context) error
}

// Client defines the transport contract required for peer coordination.
type Client interface {
	SendRaw(ctx context.Context, data []byte) error
	IsConnected() bool
	Disconnect(ctx context.Context) error
}

// XTForwardRequest is sent to peer sidecars to forward an XT.
type XTForwardRequest struct {
	InstanceID   string              `json:"instance_id"`
	Transactions map[string][]string `json:"transactions"` // chainID -> hex txs
	OriginChain  uint64              `json:"origin_chain"`
	OriginSeq    uint64              `json:"origin_seq"`
}

// VoteRequest is sent to peer sidecars with our vote.
type VoteRequest struct {
	InstanceID string `json:"instance_id"`
	ChainID    uint64 `json:"chain_id"`
	Vote       bool   `json:"vote"`
}

// DefaultCoordinator handles direct coordination with peer sidecars.
type DefaultCoordinator struct {
	localChainID uint64
	peers        map[uint64]Client // chainID -> transport client
	log          zerolog.Logger
}

// NewCoordinator creates a new peer coordinator.
func NewCoordinator(
	localChainID uint64,
	peers map[uint64]Client,
	log zerolog.Logger,
) *DefaultCoordinator {
	return &DefaultCoordinator{
		localChainID: localChainID,
		peers:        peers,
		log:          log.With().Str("component", "peer_coordinator").Logger(),
	}
}

// ForwardXT forwards an XT to peer sidecars for their chains.
func (c *DefaultCoordinator) ForwardXT(
	ctx context.Context,
	instanceID string,
	txs map[uint64][][]byte,
	originSeq uint64,
) error {
	if len(txs) == 0 {
		return nil
	}

	hexTxs := make(map[string][]string, len(txs))
	for chainID, chainTxs := range txs {
		if len(chainTxs) == 0 {
			continue
		}
		encoded := make([]string, 0, len(chainTxs))
		for _, txBytes := range chainTxs {
			encoded = append(encoded, fmt.Sprintf("0x%x", txBytes))
		}
		hexTxs[fmt.Sprintf("%d", chainID)] = encoded
	}

	req := XTForwardRequest{
		InstanceID:   instanceID,
		Transactions: hexTxs,
		OriginChain:  c.localChainID,
		OriginSeq:    originSeq,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal forward request: %w", err)
	}

	for chainID, client := range c.peers {
		if _, hasTx := txs[chainID]; !hasTx {
			continue
		}

		if !client.IsConnected() {
			c.log.Warn().Uint64("peer_chain", chainID).Msg("Peer not connected, skipping XT forward")
			continue
		}

		if err := client.SendRaw(ctx, body); err != nil {
			c.log.Error().Err(err).Uint64("peer_chain", chainID).Msg("Failed to forward XT to peer")
			continue
		}

		c.log.Debug().
			Str("instance_id", instanceID).
			Uint64("peer_chain", chainID).
			Msg("Forwarded XT to peer sidecar")
	}

	return nil
}

// SendVoteToPeers sends our vote to all peer sidecars.
func (c *DefaultCoordinator) SendVoteToPeers(ctx context.Context, instanceID string, chainID uint64, vote bool) error {
	if len(c.peers) == 0 {
		return nil
	}

	req := VoteRequest{
		InstanceID: instanceID,
		ChainID:    chainID,
		Vote:       vote,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal vote request: %w", err)
	}

	for peerChainID, client := range c.peers {
		if !client.IsConnected() {
			c.log.Warn().Uint64("peer_chain", peerChainID).Msg("Peer not connected, skipping vote")
			continue
		}

		if err := client.SendRaw(ctx, body); err != nil {
			c.log.Error().Err(err).Uint64("peer_chain", peerChainID).Msg("Failed to send vote to peer")
			continue
		}

		c.log.Debug().
			Str("instance_id", instanceID).
			Uint64("peer_chain", peerChainID).
			Bool("vote", vote).
			Msg("Sent vote to peer sidecar via QUIC")
	}

	return nil
}

// GetPeerChainIDs returns the chain IDs of all peer sidecars.
func (c *DefaultCoordinator) GetPeerChainIDs() []uint64 {
	ids := make([]uint64, 0, len(c.peers))
	for chainID := range c.peers {
		ids = append(ids, chainID)
	}
	return ids
}

// Close closes all peer connections.
func (c *DefaultCoordinator) Close(ctx context.Context) error {
	for chainID, client := range c.peers {
		if err := client.Disconnect(ctx); err != nil {
			c.log.Error().Err(err).Uint64("peer_chain", chainID).Msg("Failed to disconnect from peer")
		}
	}
	return nil
}

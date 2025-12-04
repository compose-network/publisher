package registry

import (
	"context"
	"fmt"
	"path"
	"time"

	compreg "github.com/compose-network/registry/registry"
	"github.com/compose-network/specs/compose"
	"github.com/rs/zerolog"
)

// composeService is a static, compose-registry-backed implementation of Service.
// It loads chains for the selected network (by L1 chain ID) from the registry
// (embedded or directory-based) and serves them via the Service interface.
type composeService struct {
	rollups      map[compose.ChainID]*RollupInfo
	log          zerolog.Logger
	l1PublicRPC  string
	publisherDGF string
}

// NewComposeService creates a compose-backed registry service.
// If registryPath is empty, the embedded registry is used.
func NewComposeService(registryPath string, l1ChainID uint64, log zerolog.Logger) (*composeService, error) {
	var r compreg.Registry
	var err error
	if registryPath != "" {
		r, err = compreg.NewFromDir(registryPath)
		if err != nil {
			return nil, fmt.Errorf("open registry dir: %w", err)
		}
	} else {
		r = compreg.New()
	}

	net, err := r.GetNetworkById(l1ChainID)
	if err != nil {
		return nil, fmt.Errorf("resolve network for l1.chain_id=%d: %w", l1ChainID, err)
	}

	// Capture network-level config for later access (L1 RPC, SP contracts)
	ncfg, err := net.LoadConfig()
	if err != nil {
		return nil, fmt.Errorf("load network config: %w", err)
	}
	chains, err := net.ListChains()
	if err != nil {
		return nil, fmt.Errorf("list chains: %w", err)
	}

	rollups := make(map[compose.ChainID]*RollupInfo)
	now := time.Now()
	for _, ch := range chains {
		cfg, err := ch.LoadConfig()
		if err != nil {
			return nil, fmt.Errorf("load config for %s: %w", path.Join(net.Slug(), ch.Slug()), err)
		}

		// Build endpoint from [sequencer] host:port
		endpoint := cfg.Sequencer.Host
		if cfg.Sequencer.Port != 0 && cfg.Sequencer.Host != "" {
			endpoint = fmt.Sprintf("%s:%d", cfg.Sequencer.Host, cfg.Sequencer.Port)
		}

		ri := &RollupInfo{
			ChainID:      compose.ChainID(cfg.ChainID),
			Endpoint:     endpoint,
			PublicKey:    nil,
			StartingSlot: 1,
			IsActive:     true,
			UpdatedAt:    now,
		}
		ri.ChainID = compose.ChainID(cfg.ChainID)
		rollups[ri.ChainID] = ri
	}

	return &composeService{
		rollups:      rollups,
		log:          log.With().Str("component", "registry.compose").Logger(),
		l1PublicRPC:  ncfg.L1.PublicRPC,
		publisherDGF: ncfg.Publisher.DisputeGameFactory,
	}, nil
}

func (c *composeService) Start(ctx context.Context) error { return nil }
func (c *composeService) Stop(ctx context.Context) error  { return nil }

// Service methods
func (c *composeService) GetActiveRollups(ctx context.Context) ([]compose.ChainID, error) {
	return c.active(), nil
}

func (c *composeService) GetRollupEndpoint(ctx context.Context, chainID compose.ChainID) (string, error) {
	if ri, ok := c.rollups[chainID]; ok && ri.IsActive {
		return ri.Endpoint, nil
	}
	return "", fmt.Errorf("rollup not found or inactive")
}

func (c *composeService) GetRollupPublicKey(ctx context.Context, chainID compose.ChainID) ([]byte, error) {
	if ri, ok := c.rollups[chainID]; ok && ri.IsActive {
		return ri.PublicKey, nil
	}
	return nil, fmt.Errorf("rollup not found or inactive")
}

func (c *composeService) IsRollupActive(ctx context.Context, chainID compose.ChainID) (bool, error) {
	if ri, ok := c.rollups[chainID]; ok {
		return ri.IsActive, nil
	}
	return false, nil
}

func (c *composeService) WatchRegistry(ctx context.Context) (<-chan Event, error) {
	ch := make(chan Event)
	close(ch)
	return ch, nil
}

func (c *composeService) GetRollupInfo(chainID compose.ChainID) (*RollupInfo, error) {
	if ri, ok := c.rollups[chainID]; ok {
		out := *ri
		out.ChainID = ri.ChainID
		if ri.PublicKey != nil {
			out.PublicKey = append([]byte(nil), ri.PublicKey...)
		}
		return &out, nil
	}
	return nil, fmt.Errorf("rollup not found")
}

func (c *composeService) GetAllRollups() map[compose.ChainID]*RollupInfo {
	out := make(map[compose.ChainID]*RollupInfo, len(c.rollups))
	for k, v := range c.rollups {
		cp := *v
		cp.ChainID = v.ChainID
		if v.PublicKey != nil {
			cp.PublicKey = append([]byte(nil), v.PublicKey...)
		}
		out[k] = &cp
	}
	return out
}

func (c *composeService) SetPollingInterval(_ time.Duration) {}

// helpers
func (c *composeService) active() []compose.ChainID {
	res := make([]compose.ChainID, 0, len(c.rollups))
	for _, v := range c.rollups {
		if v.IsActive {
			res = append(res, v.ChainID)
		}
	}
	return res
}

// Optional network-level accessors (used by leader app for config hydration)
func (c *composeService) L1PublicRPC() string                 { return c.l1PublicRPC }
func (c *composeService) PublisherDisputeGameFactory() string { return c.publisherDGF }

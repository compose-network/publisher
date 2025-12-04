package registry

import (
	"context"
	"time"

	"github.com/compose-network/specs/compose"
)

type Service interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	GetActiveRollups(ctx context.Context) ([]compose.ChainID, error)
	GetRollupEndpoint(ctx context.Context, chainID compose.ChainID) (string, error)
	GetRollupPublicKey(ctx context.Context, chainID compose.ChainID) ([]byte, error)
	IsRollupActive(ctx context.Context, chainID compose.ChainID) (bool, error)
	WatchRegistry(ctx context.Context) (<-chan Event, error)
	GetRollupInfo(chainID compose.ChainID) (*RollupInfo, error)
	GetAllRollups() map[compose.ChainID]*RollupInfo
	SetPollingInterval(interval time.Duration)
}

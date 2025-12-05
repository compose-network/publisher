package adapter

import "context"

// BaseAdapter provides a default, embeddable implementation of the Adapter interface.
// It is intended to be used by rollup implementations to reduce boilerplate code.
//
// The BaseAdapter provides no-op implementations for the lifecycle hooks (OnStart, OnStop)
// and basic getters for identity fields. Rollup-specific logic, especially for
// message handling, must be implemented by the consuming type that embeds BaseAdapter.
type BaseAdapter struct {
	name    string
	version string
	chainID string
}

// NewBaseAdapter creates a new instance of BaseAdapter with the provided
// identity information (name, version, and chain ID).
func NewBaseAdapter(name, version, chainID string) *BaseAdapter {
	return &BaseAdapter{
		name:    name,
		version: version,
		chainID: chainID,
	}
}

// Name returns the identifier of the rollup implementation.
func (b *BaseAdapter) Name() string { return b.name }

// Version returns the version of the rollup implementation.
func (b *BaseAdapter) Version() string { return b.version }

// ChainID returns the chain ID that the rollup is configured to operate on.
func (b *BaseAdapter) ChainID() string { return b.chainID }

// OnStart is a no-op lifecycle hook. It is called when the adapter starts.
// By default, it returns nil. Implementers can override this method to add
// custom startup logic for their rollup adapter.
func (b *BaseAdapter) OnStart(ctx context.Context) error { return nil }

// OnStop is a no-op lifecycle hook. It is called when the adapter stops.
// By default, it returns nil. Implementers can override this method to add
// custom shutdown or cleanup logic for their rollup adapter.
func (b *BaseAdapter) OnStop(ctx context.Context) error { return nil }

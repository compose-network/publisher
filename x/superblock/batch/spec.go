package batch

import "time"

// This file contains constants defined by external specifications.
// These values MUST NOT be changed as they are part of protocol specifications.

// Ethereum Consensus Layer Specification
// Reference: https://github.com/ethereum/consensus-specs
const (
	// SlotDuration is the time between Ethereum consensus slots
	// Ethereum Spec: 12 seconds per slot
	SlotDuration = 12 * time.Second

	// SlotsPerEpoch is the number of slots in an Ethereum epoch
	// Ethereum Spec: 32 slots per epoch
	SlotsPerEpoch = 32

	// SecondsPerSlot is the duration of one slot in seconds
	SecondsPerSlot = 12

	// SecondsPerEpoch is the duration of one epoch in seconds
	// Calculated: 12 seconds/slot * 32 slots/epoch = 384 seconds
	SecondsPerEpoch = SecondsPerSlot * SlotsPerEpoch
)

// EthereumMainnetGenesis timestamp
// This is the Unix timestamp of the Ethereum Beacon Chain genesis block.
// Date: 2020-12-01 12:00:23 UTC
const EthereumMainnetGenesis = 1606824023

// Settlement Layer Specification
//
// | Config Field                   | Value                                                 |
// |-------------------------------|--------------------------------------------------------|
// | Block Time                     | 12 seconds                                            |
// | Ethereum Epochs Batch Factor   | 10 (batch triggered when Mod(Curr Eth Epoch, 10) == 0)|
const (
	// BatchFactor determines how often batches are triggered
	// Spec: Batches trigger every 10 Ethereum epochs
	// This is the fundamental synchronization constant - DO NOT CHANGE
	BatchFactor = 10

	// SlotsPerBatch is the number of slots in one batch period
	// Calculated: 10 epochs * 32 slots/epoch = 320 slots
	SlotsPerBatch = BatchFactor * SlotsPerEpoch
)

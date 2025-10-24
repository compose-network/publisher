package cdcp

import (
	"errors"
	"sync"
)

var (
	ErrInstanceAlreadyInitialized     = errors.New("instance already initialized")
	ErrInstanceNotWaitingForVotes     = errors.New("instance not waiting for votes")
	ErrERChainCannotSendVote          = errors.New("ER chain cannot send vote")
	ErrChainIDDoesNotBelongToInstance = errors.New("chainID does not belong to instance")
	ErrDuplicateVote                  = errors.New("duplicate vote")
	ErrOnlyERChainCanSendWSDecision   = errors.New("only ER chain can send WS decision")
	ErrInstanceAlreadyDecided         = errors.New("instance already decided")
)

type Instance interface {
	InitInstance() error
	ProcessVote(chainID ChainID, vote bool) error
	ProcessWSDecided(chainID ChainID, decision bool) error
	IsDecided() DecisionResult
	Timeout() error
}

type Messenger interface {
	// SendStartMessage sends a start message to all chains (native and external)
	SendStartMessage(slot Slot, seqNumber SequenceNumber, xtReq XTRequest, xtId XTId)
	// SendNativeDecided notifies the WS chain of the native decision
	SendNativeDecided(xtId XTId, decision bool)
	// SendDecided notifies all native chains of the decision
	SendDecided(xtId XTId, decision bool)
}

type InstanceState int

const (
	InstanceStateInit InstanceState = iota
	InstanceStateWaitingForVotes
	InstanceStateWaitingForWSDecided
	InstanceStateDecided
)

type instance struct {
	mu sync.Mutex

	// Dependencies
	messenger Messenger

	// Instance
	instanceData InstanceData
	chains       map[ChainID]struct{}
	erChainID    ChainID

	// State
	state    InstanceState
	decision DecisionResult
	votes    map[ChainID]bool
}

func NewInstance(
	msg Messenger,
	instanceData InstanceData,
	erChainID ChainID,
) Instance {

	chains := make(map[ChainID]struct{})
	for _, req := range instanceData.xTRequest {
		chains[req.ChainID] = struct{}{}
	}

	return &instance{
		mu:           sync.Mutex{},
		messenger:    msg,
		instanceData: instanceData,
		chains:       chains,
		erChainID:    erChainID,
		state:        InstanceStateInit,
		decision:     DecisionResultUndecided,
		votes:        make(map[ChainID]bool),
	}
}

func (i *instance) InitInstance() error {
	i.mu.Lock()
	defer i.mu.Unlock()

	if i.state != InstanceStateInit {
		return ErrInstanceAlreadyInitialized
	}

	i.messenger.SendStartMessage(i.instanceData.Slot,
		i.instanceData.SequenceNumber,
		i.instanceData.xTRequest,
		i.instanceData.xTId,
	)
	i.state = InstanceStateWaitingForVotes
	return nil
}

func (i *instance) ProcessVote(chainID ChainID, vote bool) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	if i.state != InstanceStateWaitingForVotes {
		return ErrInstanceNotWaitingForVotes
	}

	// ER Chain can't send vote
	if chainID == i.erChainID {
		return ErrERChainCannotSendVote
	}

	// Chain must belong to the instance
	if _, exists := i.chains[chainID]; !exists {
		return ErrChainIDDoesNotBelongToInstance
	}

	// If vote already recorded, ignore duplicate
	if _, exists := i.votes[chainID]; exists {
		return ErrDuplicateVote
	}

	i.votes[chainID] = vote

	if !vote {
		// If any vote is false, the decision is false
		i.state = InstanceStateDecided
		i.decision = DecisionResultRejected
		i.messenger.SendDecided(i.instanceData.xTId, false)
		i.messenger.SendNativeDecided(i.instanceData.xTId, false)
		return nil
	}

	if len(i.votes) == len(i.chains)-1 {
		// All votes received (excluding ER chain)
		i.state = InstanceStateWaitingForWSDecided
		i.messenger.SendNativeDecided(i.instanceData.xTId, true)
		return nil
	}

	return nil
}

func (i *instance) ProcessWSDecided(chainID ChainID, decision bool) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	if i.erChainID != chainID {
		return ErrOnlyERChainCanSendWSDecision
	}

	if i.state == InstanceStateDecided {
		return ErrInstanceAlreadyDecided
	}

	i.state = InstanceStateDecided
	i.messenger.SendDecided(i.instanceData.xTId, decision)
	if decision {
		i.decision = DecisionResultAccepted
	} else {
		i.decision = DecisionResultRejected
	}
	return nil
}

func (i *instance) IsDecided() DecisionResult {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.decision
}

func (i *instance) Timeout() error {
	i.mu.Lock()
	defer i.mu.Unlock()

	// If already decided or waiting for WS decision, no action needed
	if i.state == InstanceStateDecided || i.state == InstanceStateWaitingForWSDecided {
		return nil // No action needed
	}

	i.decision = DecisionResultRejected
	i.state = InstanceStateDecided
	i.messenger.SendDecided(i.instanceData.xTId, false)
	i.messenger.SendNativeDecided(i.instanceData.xTId, false)
	return nil
}

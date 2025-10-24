package consensus

import (
	"context"
	"fmt"
	"time"

	pb "github.com/compose-network/publisher/proto/rollup/v1"
	"github.com/compose-network/publisher/x/cdcp"
)

// StartTransaction initiates a new 2PC transaction
func (c *coordinator) startCDCPTransaction(ctx context.Context, from string, xtReq *pb.XTRequest) error {
	xtID, err := xtReq.XtID()
	if err != nil {
		return fmt.Errorf("failed to generate xtID: %w", err)
	}

	chains := xtReq.ChainIDs()
	if len(chains) == 0 {
		return fmt.Errorf("no participating chains found")
	}

	if !c.chainsIncludeERChain(chains) {
		return fmt.Errorf("ER chain %s not included in participating chains for CDCP instance", c.erChainID)
	}

	// Create CDCP instance
	instKey := cdcpInstanceKey(xtID.Hash)
	if _, exists := c.cdcpInstances[instKey]; exists {
		return fmt.Errorf("CDCP instance already exists for xt_id %s", xtID.Hex())
	}
	inst := cdcp.NewInstance(
		c.cdcpMessenger,
		cdcp.InstanceData{
			Slot:           0,
			SequenceNumber: 0,
			XTRequest:      convertToCdcpXTRequest(xtReq),
			XTId:           cdcpToCdcpXTID(xtID),
		},
		c.erChainID,
	)
	c.cdcpInstances[instKey] = inst
	if err := inst.InitInstance(); err != nil {
		return fmt.Errorf("failed to initialize CDCP instance: %w", err)
	}
	c.cdcpStartTime[instKey] = time.Now()
	// Start timer
	c.cdcpTimer[instKey] = time.AfterFunc(c.config.Timeout, func() {
		c.handleCDCPTimeout(xtID)
	})

	c.metrics.RecordTransactionStarted(len(chains))

	c.log.Info().
		Str("xt_id", xtID.Hex()).
		Int("participating_chains", len(chains)).
		Dur("timeout", c.config.Timeout).
		Msg("Started 2PC transaction")

	// Invoke start callback
	c.callbackMgr.InvokeStart(ctx, from, xtReq)

	return nil
}

// handleCDCPTimeout handles a timeout for a CDCP instance
func (c *coordinator) handleCDCPTimeout(xtID *pb.XtID) {
	if cdcpInstance, exists := c.cdcpInstances[cdcpInstanceKey(xtID.Hash)]; exists {
		if err := cdcpInstance.Timeout(); err != nil {
			c.log.Error().
				Err(err).
				Str("xt_id", xtID.Hex()).
				Msg("CDCP instance timeout handling failed")
		}
		c.metrics.RecordTimeout()
		if cdcpInstance.IsDecided() != cdcp.DecisionResultUndecided {
			c.handleTermination(xtID, cdcpDecisionStateToDecisionState(cdcpInstance.IsDecided()))
		}
	}
}

func (c *coordinator) chainsIncludeERChain(chains map[string]struct{}) bool {
	for chainID := range chains {
		if chainID == string(c.erChainID) {
			return true
		}
	}
	return false
}

// RecordCDCPVote processes a vote from a participant for a CDCP instance
func (c *coordinator) RecordCDCPVote(xtID *pb.XtID, chainID string, vote bool) (DecisionState, error) {

	inst, exists := c.cdcpInstances[cdcpInstanceKey(xtID.Hash)]
	if !exists {
		return StateUndecided, fmt.Errorf("CDCP instance not found for transaction %s", xtID.Hex())
	}

	if err := inst.ProcessVote(cdcp.ChainID(chainID), vote); err != nil {
		return cdcpDecisionStateToDecisionState(inst.IsDecided()), fmt.Errorf("failed to process vote for transaction %s: %w", xtID.Hex(), err)
	}

	voteLatency := time.Since(c.cdcpStartTime[cdcpInstanceKey(xtID.Hash)])
	c.metrics.RecordVote(chainID, vote, voteLatency)

	c.log.Info().
		Str("xt_id", xtID.Hex()).
		Str("chain", chainID).
		Bool("vote", vote).
		Msg("Recorded vote")

	if inst.IsDecided() != cdcp.DecisionResultUndecided {
		c.log.Info().
			Str("xt_id", xtID.Hex()).
			Str("decision", inst.IsDecided().String()).
			Msg("Transaction decision reached")
		return c.handleTermination(xtID, cdcpDecisionStateToDecisionState(inst.IsDecided())), nil
	}

	return StateUndecided, nil
}

// handleAbort handles an abort decision
func (c *coordinator) handleTermination(xtID *pb.XtID, state DecisionState) DecisionState {

	// Stop cdcp timer
	instKey := cdcpInstanceKey(xtID.Hash)

	if has, _ := c.cdcpHasTerminated[instKey]; has {
		// Already terminated instance
		return state
	}

	// Stop timer
	if timer, exists := c.cdcpTimer[instKey]; exists {
		timer.Stop()
		delete(c.cdcpTimer, instKey)
	}

	// Metric recording
	duration := time.Since(c.cdcpStartTime[instKey])
	c.metrics.RecordTransactionCompleted(state.String(), duration)

	// Schedule cleanup
	time.AfterFunc(5*time.Minute, func() {
		c.RemoveCDCPState(xtID)
	})

	return state
}

func (c *coordinator) RemoveCDCPState(xtID *pb.XtID) {
	instKey := cdcpInstanceKey(xtID.Hash)
	delete(c.cdcpInstances, instKey)
	delete(c.cdcpStartTime, instKey)
	delete(c.cdcpTimer, instKey)
	delete(c.cdcpHasTerminated, instKey)
}

// RecordWSDecision processes a WS decision
func (c *coordinator) RecordWSDecision(xtID *pb.XtID, from string, decision bool) error {
	instKey := cdcpInstanceKey(xtID.Hash)
	inst, exists := c.cdcpInstances[instKey]
	if !exists {
		c.log.Debug().
			Str("xt_id", xtID.Hex()).
			Bool("decision", decision).
			Msg("Received WS decision for unknown transaction")
		return nil
	}

	if err := inst.ProcessWSDecided(cdcp.ChainID(from), decision); err != nil {
		return fmt.Errorf("failed to process WS decision for transaction %s: %w", xtID.Hex(), err)
	}

	if inst.IsDecided() != cdcp.DecisionResultUndecided {
		c.handleTermination(xtID, cdcpDecisionStateToDecisionState(inst.IsDecided()))
	}

	return nil
}

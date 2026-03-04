package simulation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"time"

	"github.com/compose-network/compose-sdk/mailbox"
	"github.com/compose-network/specs/compose"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
)

// ChainRPC holds RPC configuration for a chain.
type ChainRPC struct {
	ChainID        compose.ChainID
	URL            string
	MailboxAddress common.Address
}

// Config holds simulator configuration.
type Config struct {
	Chains  []ChainRPC
	Timeout time.Duration
}

// Result represents the result of simulating a transaction.
type Result struct {
	ChainID          compose.ChainID
	Success          bool
	Error            string
	GasUsed          uint64
	StateChanges     map[common.Address]map[common.Hash]common.Hash
	StateOverrides   map[string]any
	Dependencies     []mailbox.CrossRollupDependency
	OutboundMessages []mailbox.CrossRollupMessage
}

// RequiresCoordination returns true if the result indicates cross-chain operations.
func (r *Result) RequiresCoordination() bool {
	return len(r.Dependencies) > 0 || len(r.OutboundMessages) > 0
}

// Simulator defines the interface for transaction simulation.
type Simulator interface {
	// Simulate simulates a transaction on the given chain.
	Simulate(ctx context.Context, chainID compose.ChainID, tx []byte, stateOverrides map[string]any) (*Result, error)

	// SimulateWithMailbox simulates a transaction with mailbox analysis.
	SimulateWithMailbox(
		ctx context.Context,
		chainID compose.ChainID,
		tx []byte,
		stateOverrides map[string]any,
		alreadySentMsgs []mailbox.CrossRollupMessage,
		fulfilledDeps []mailbox.CrossRollupDependency,
	) (*Result, error)

	// GetParser returns the mailbox parser for a chain.
	GetParser(chainID compose.ChainID) *mailbox.Parser
}

// RPCSimulator implements simulation via JSON-RPC calls to execution clients.
type RPCSimulator struct {
	chains  map[compose.ChainID]ChainRPC
	parsers map[compose.ChainID]*mailbox.Parser
	client  *http.Client
	timeout time.Duration
}

// NewSimulator creates a new RPC simulator.
func NewSimulator(cfg Config) (*RPCSimulator, error) {
	chains := make(map[compose.ChainID]ChainRPC)
	parsers := make(map[compose.ChainID]*mailbox.Parser)

	for _, c := range cfg.Chains {
		chains[c.ChainID] = c

		parser, err := mailbox.NewParser(c.ChainID, []common.Address{c.MailboxAddress})
		if err != nil {
			return nil, fmt.Errorf("create mailbox parser for chain %d: %w", c.ChainID, err)
		}
		parsers[c.ChainID] = parser
	}

	return &RPCSimulator{
		chains:  chains,
		parsers: parsers,
		timeout: cfg.Timeout,
		client: &http.Client{
			Timeout: cfg.Timeout,
		},
	}, nil
}

// GetParser returns the mailbox parser for a chain.
func (s *RPCSimulator) GetParser(chainID compose.ChainID) *mailbox.Parser {
	return s.parsers[chainID]
}

// Simulate simulates a transaction on the given chain using debug_traceCall.
func (s *RPCSimulator) Simulate(
	ctx context.Context,
	chainID compose.ChainID,
	txBytes []byte,
	stateOverrides map[string]any,
) (*Result, error) {
	chain, ok := s.chains[chainID]
	if !ok {
		return nil, fmt.Errorf("unknown chain: %d", chainID)
	}

	// Decode the RLP-encoded transaction
	tx := new(types.Transaction)
	if err := tx.UnmarshalBinary(txBytes); err != nil {
		return &Result{
			ChainID: chainID,
			Success: false,
			Error:   fmt.Sprintf("failed to decode transaction: %v", err),
		}, nil
	}

	// Build debug_traceCall request with decoded transaction fields
	traceReq := buildTraceRequestFromTx(tx)

	result, err := s.executeTraceCall(ctx, chain.URL, traceReq, stateOverrides)
	if err != nil {
		return &Result{
			ChainID: chainID,
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	return &Result{
		ChainID:        chainID,
		Success:        true,
		GasUsed:        result.GasUsed,
		StateChanges:   result.StateChanges,
		StateOverrides: result.StateOverrides,
	}, nil
}

// SimulateWithMailbox simulates a transaction and analyzes mailbox operations.
func (s *RPCSimulator) SimulateWithMailbox(
	ctx context.Context,
	chainID compose.ChainID,
	txBytes []byte,
	stateOverrides map[string]any,
	alreadySentMsgs []mailbox.CrossRollupMessage,
	fulfilledDeps []mailbox.CrossRollupDependency,
) (*Result, error) {
	chain, ok := s.chains[chainID]
	if !ok {
		return nil, fmt.Errorf("unknown chain: %d", chainID)
	}

	parser := s.parsers[chainID]
	if parser == nil {
		return nil, fmt.Errorf("no mailbox parser for chain: %d", chainID)
	}

	// Decode the transaction
	tx := new(types.Transaction)
	if err := tx.UnmarshalBinary(txBytes); err != nil {
		return &Result{
			ChainID: chainID,
			Success: false,
			Error:   fmt.Sprintf("failed to decode transaction: %v", err),
		}, nil
	}

	mailboxOverrides := BuildMailboxStateOverrides(chain.ChainID, chain.MailboxAddress, fulfilledDeps)
	stateOverrides = MergeStateOverrides(stateOverrides, mailboxOverrides)

	// Execute callTracer to get the call tree
	callTrace, err := s.executeCallTracerForTx(ctx, chain.URL, tx, stateOverrides)
	if err != nil {
		return &Result{
			ChainID: chainID,
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	// Analyze the call trace for mailbox operations
	mailboxState, err := parser.AnalyzeTrace(callTrace, alreadySentMsgs, fulfilledDeps)
	if err != nil {
		return &Result{
			ChainID: chainID,
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	result := &Result{
		ChainID:          chainID,
		Success:          mailboxState.Success,
		GasUsed:          mailboxState.GasUsed,
		Dependencies:     mailboxState.Dependencies,
		OutboundMessages: mailboxState.OutboundMessages,
	}

	// Also compute state overrides via prestateTracer for sequential overlays.
	if traceResult, err := s.executeTraceCall(ctx, chain.URL, buildTraceRequestFromTx(tx), stateOverrides); err == nil {
		result.StateOverrides = traceResult.StateOverrides
		result.StateChanges = traceResult.StateChanges
		if mailboxOverrides != nil {
			result.StateOverrides = MergeStateOverrides(mailboxOverrides, result.StateOverrides)
		}
	} else if mailboxOverrides != nil {
		result.StateOverrides = mailboxOverrides
	}

	if !mailboxState.Success {
		result.Error = callTrace.Error
	}

	return result, nil
}

// Internal types for trace responses

type traceRequest struct {
	To    string `json:"to,omitempty"`
	From  string `json:"from,omitempty"`
	Data  string `json:"data,omitempty"`
	Value string `json:"value,omitempty"`
	Gas   string `json:"gas,omitempty"`
}

type prestateAccount struct {
	Balance  *hexutil.Big                `json:"balance,omitempty"`
	Code     hexutil.Bytes               `json:"code,omitempty"`
	CodeHash *common.Hash                `json:"codeHash,omitempty"`
	Nonce    uint64                      `json:"nonce,omitempty"`
	Storage  map[common.Hash]common.Hash `json:"storage,omitempty"`
}

type prestateResult struct {
	Pre  map[common.Address]*prestateAccount `json:"pre"`
	Post map[common.Address]*prestateAccount `json:"post"`
}

type traceResult struct {
	GasUsed        uint64
	Output         []byte
	Error          string
	PreState       map[common.Address]*prestateAccount
	PostState      map[common.Address]*prestateAccount
	StateChanges   map[common.Address]map[common.Hash]common.Hash
	StateOverrides map[string]any
}

func buildTraceRequestFromTx(tx *types.Transaction) *traceRequest {
	req := &traceRequest{
		Gas: fmt.Sprintf("0x%x", tx.Gas()),
	}

	if tx.To() != nil {
		req.To = tx.To().Hex()
	}
	if len(tx.Data()) > 0 {
		req.Data = hexutil.Encode(tx.Data())
	}
	if tx.Value() != nil && tx.Value().Sign() > 0 {
		req.Value = fmt.Sprintf("0x%x", tx.Value())
	}

	signer := types.LatestSignerForChainID(tx.ChainId())
	if sender, err := types.Sender(signer, tx); err == nil {
		req.From = sender.Hex()
	}

	return req
}

func (s *RPCSimulator) executeTraceCall(
	ctx context.Context,
	rpcURL string,
	req *traceRequest,
	stateOverrides map[string]any,
) (*traceResult, error) {
	config := map[string]any{
		"tracer":       "prestateTracer",
		"tracerConfig": map[string]bool{"diffMode": true},
	}
	if len(stateOverrides) > 0 {
		config["stateOverrides"] = stateOverrides
	}

	rpcReq := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "debug_traceCall",
		"params": []any{
			req,
			"latest",
			config,
		},
	}

	body, err := json.Marshal(rpcReq)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", rpcURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var rpcResp struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if rpcResp.Error != nil {
		return nil, fmt.Errorf("RPC error: %s", rpcResp.Error.Message)
	}

	var prestate prestateResult
	if err := json.Unmarshal(rpcResp.Result, &prestate); err != nil {
		return nil, fmt.Errorf("unmarshal prestate: %w", err)
	}

	stateChanges := make(map[common.Address]map[common.Hash]common.Hash)
	for addr, postAccount := range prestate.Post {
		if postAccount.Storage != nil {
			stateChanges[addr] = postAccount.Storage
		}
	}

	var gasUsed uint64 = 21000
	for addr := range prestate.Post {
		if pre, ok := prestate.Pre[addr]; ok {
			if post, ok := prestate.Post[addr]; ok && pre.Balance != nil && post.Balance != nil {
				preBal := (*big.Int)(pre.Balance)
				postBal := (*big.Int)(post.Balance)
				diff := new(big.Int).Sub(preBal, postBal)
				if diff.Sign() > 0 {
					gasUsed = diff.Uint64()
				}
			}
		}
	}

	return &traceResult{
		GasUsed:        gasUsed,
		PreState:       prestate.Pre,
		PostState:      prestate.Post,
		StateChanges:   stateChanges,
		StateOverrides: buildStateOverrides(prestate.Pre, prestate.Post),
	}, nil
}

func (s *RPCSimulator) executeCallTracerForTx(
	ctx context.Context,
	rpcURL string,
	tx *types.Transaction,
	stateOverrides map[string]any,
) (*mailbox.CallTraceResult, error) {
	txArgs := make(map[string]any)

	if tx.To() != nil {
		txArgs["to"] = tx.To().Hex()
	}
	if len(tx.Data()) > 0 {
		txArgs["data"] = hexutil.Encode(tx.Data())
	}
	if tx.Value() != nil && tx.Value().Sign() > 0 {
		txArgs["value"] = fmt.Sprintf("0x%x", tx.Value())
	}
	txArgs["gas"] = fmt.Sprintf("0x%x", tx.Gas())

	signer := types.LatestSignerForChainID(tx.ChainId())
	if sender, err := types.Sender(signer, tx); err == nil {
		txArgs["from"] = sender.Hex()
	}

	config := map[string]any{
		"tracer": "callTracer",
	}
	if len(stateOverrides) > 0 {
		config["stateOverrides"] = stateOverrides
	}

	rpcReq := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "debug_traceCall",
		"params": []any{
			txArgs,
			"latest",
			config,
		},
	}

	body, err := json.Marshal(rpcReq)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", rpcURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var rpcResp struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if rpcResp.Error != nil {
		return nil, fmt.Errorf("RPC error: %s", rpcResp.Error.Message)
	}

	var callTrace mailbox.CallTraceResult
	if err := json.Unmarshal(rpcResp.Result, &callTrace); err != nil {
		return nil, fmt.Errorf("unmarshal call trace: %w", err)
	}

	return &callTrace, nil
}

func buildStateOverrides(
	pre map[common.Address]*prestateAccount,
	post map[common.Address]*prestateAccount,
) map[string]any {
	if len(pre) == 0 && len(post) == 0 {
		return nil
	}

	overrides := make(map[string]any)

	// Process post-state accounts first.
	for addr, postAccount := range post {
		account := make(map[string]any)

		if postAccount.Balance != nil {
			account["balance"] = hexutil.EncodeBig((*big.Int)(postAccount.Balance))
		}
		if postAccount.Nonce > 0 {
			account["nonce"] = fmt.Sprintf("0x%x", postAccount.Nonce)
		}
		if len(postAccount.Code) > 0 {
			account["code"] = hexutil.Encode(postAccount.Code)
		}

		if len(postAccount.Storage) > 0 {
			stateDiff := make(map[string]string, len(postAccount.Storage))
			for slot, value := range postAccount.Storage {
				stateDiff[slot.Hex()] = value.Hex()
			}
			account["stateDiff"] = stateDiff
		}

		if len(account) > 0 {
			overrides[addr.Hex()] = account
		}
	}

	// Handle accounts that disappeared in post (selfdestruct).
	for addr := range pre {
		if _, ok := post[addr]; ok {
			continue
		}
		overrides[addr.Hex()] = map[string]any{
			"balance": "0x0",
			"nonce":   "0x0",
			"code":    "0x",
			"state":   map[string]string{},
		}
	}

	if len(overrides) == 0 {
		return nil
	}
	return overrides
}

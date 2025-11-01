package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"math/big"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/compose-network/publisher/x/auth"
	"github.com/compose-network/publisher/x/transport"
	"github.com/compose-network/publisher/x/transport/tcp"
	pb "github.com/compose-network/specs/compose/proto"
	"github.com/ethereum/go-ethereum/common"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/proto"
	"gopkg.in/yaml.v3"
)

const (
	defaultServerAddr        = "127.0.0.1:8080"
	defaultWaitWindow        = 2 * time.Second
	defaultSendTimeout       = 5 * time.Second
	defaultRandomTxSize      = 256
	defaultRandomTxCount     = 2
	defaultInstanceWaitLimit = 30 * time.Second
)

var (
	errMissingConfigPath = errors.New("missing required flag: -config")
)

type duration struct {
	time.Duration
}

func (d *duration) UnmarshalYAML(value *yaml.Node) error {
	if value == nil {
		d.Duration = 0
		return nil
	}

	switch value.Tag {
	case "!!int":
		secs, err := strconv.ParseInt(value.Value, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid duration %q: %w", value.Value, err)
		}
		d.Duration = time.Duration(secs) * time.Second
	case "!!str", "":
		if value.Value == "" {
			d.Duration = 0
			return nil
		}
		parsed, err := time.ParseDuration(value.Value)
		if err != nil {
			return fmt.Errorf("invalid duration %q: %w", value.Value, err)
		}
		d.Duration = parsed
	default:
		return fmt.Errorf("unsupported duration type tag %q", value.Tag)
	}
	return nil
}

type config struct {
	ServerAddr string        `yaml:"sp_addr"`
	ClientID   string        `yaml:"client_id"`
	PrivateKey string        `yaml:"private_key"`
	WaitWindow duration      `yaml:"wait_window"`
	Actions    []actionSpec  `yaml:"actions"`
	LogLevel   zerolog.Level `yaml:"-"`
}

type actionSpec struct {
	Type string `yaml:"type"`

	Chains []xtChain `yaml:"chains"`

	ChainID        string   `yaml:"chain_id"`
	Vote           string   `yaml:"vote"`
	InstanceID     string   `yaml:"instance_id"`
	InstanceSource string   `yaml:"instance_source"`
	XtID           string   `yaml:"xt_id"`
	Transactions   []string `yaml:"transactions"`

	Duration duration `yaml:"duration"`
	Timeout  duration `yaml:"timeout"`
}

type xtChain struct {
	ChainID      string   `yaml:"chain_id"`
	Transactions []string `yaml:"transactions"`
	RandomBytes  int      `yaml:"random_bytes"`
	RandomCount  int      `yaml:"random_count"`
}

type instanceStore struct {
	mu     sync.Mutex
	latest *pb.StartInstance
	notify chan struct{}
}

func newInstanceStore() *instanceStore {
	return &instanceStore{notify: make(chan struct{})}
}

func (s *instanceStore) update(si *pb.StartInstance) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.latest = proto.Clone(si).(*pb.StartInstance)
	close(s.notify)
	s.notify = make(chan struct{})
}

func (s *instanceStore) latestInstance() (*pb.StartInstance, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.latest == nil {
		return nil, false
	}
	return proto.Clone(s.latest).(*pb.StartInstance), true
}

func (s *instanceStore) waitForInstance(ctx context.Context) (*pb.StartInstance, error) {
	if inst, ok := s.latestInstance(); ok {
		return inst, nil
	}

	s.mu.Lock()
	notify := s.notify
	s.mu.Unlock()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-notify:
	}

	inst, ok := s.latestInstance()
	if !ok {
		return nil, errors.New("start instance not available")
	}
	return inst, nil
}

func main() {
	cfgPath := flag.String("config", "", "Path to YAML file describing the workflow")
	flag.Parse()

	if *cfgPath == "" {
		flag.Usage()
		fmt.Fprintln(os.Stderr, errMissingConfigPath)
		os.Exit(2)
	}

	cfg, err := loadConfig(*cfgPath, os.ReadFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if err := run(context.Background(), cfg); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func loadConfig(path string, reader func(string) ([]byte, error)) (config, error) {
	data, err := reader(path)
	if err != nil {
		return config{}, fmt.Errorf("read config: %w", err)
	}

	var cfg config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return config{}, fmt.Errorf("parse config yaml: %w", err)
	}

	if cfg.ServerAddr == "" {
		cfg.ServerAddr = defaultServerAddr
	}
	if cfg.WaitWindow.Duration == 0 {
		cfg.WaitWindow.Duration = defaultWaitWindow
	}
	if len(cfg.Actions) == 0 {
		return config{}, errors.New("config must include at least one action")
	}
	return cfg, nil
}

func run(ctx context.Context, cfg config) error {
	logger := zerolog.New(zerolog.ConsoleWriter{
		Out:        os.Stdout,
		TimeFormat: time.RFC3339,
	}).Level(zerolog.InfoLevel).With().Timestamp().Logger()

	clientCfg := tcp.DefaultClientConfig()
	clientCfg.ServerAddr = cfg.ServerAddr
	clientCfg.ClientID = cfg.ClientID

	var client transport.Client = tcp.NewClient(clientCfg, logger)
	if cfg.PrivateKey != "" {
		authManager, err := auth.NewManagerFromHex(cfg.PrivateKey)
		if err != nil {
			return fmt.Errorf("invalid private key: %w", err)
		}
		if authManager == nil {
			return errors.New("failed to create auth manager")
		}
		client = client.WithAuth(authManager)
		logger = logger.With().Str("address", authManager.Address()).Logger()
	}

	store := newInstanceStore()

	client.SetHandler(func(ctx context.Context, msg *pb.Message) ([]common.Hash, error) {
		switch payload := msg.Payload.(type) {
		case *pb.Message_Decided:
			logger.Info().
				Str("instance_id", fmt.Sprintf("%x", payload.Decided.GetInstanceId())).
				Bool("decision", payload.Decided.GetDecision()).
				Msg("received Decided from SP")
		case *pb.Message_StartInstance:
			start := payload.StartInstance
			logger.Info().
				Str("instance_id", fmt.Sprintf("%x", start.GetInstanceId())).
				Uint64("period_id", start.GetPeriodId()).
				Uint64("sequence", start.GetSequenceNumber()).
				Msg("received StartInstance from SP")
			store.update(start)
		case *pb.Message_StartPeriod:
			logger.Info().
				Uint64("period_id", payload.StartPeriod.GetPeriodId()).
				Uint64("superblock_number", payload.StartPeriod.GetSuperblockNumber()).
				Msg("received StartPeriod from SP")
		case *pb.Message_Ping:
			logger.Info().Msg("received ping from SP")
		case *pb.Message_Pong:
			logger.Info().Msg("received pong from SP")
		default:
			logger.Info().Str("payload_type", fmt.Sprintf("%T", payload)).Msg("received message from SP")
		}
		return nil, nil
	})

	connectCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	if err := client.Connect(connectCtx, clientCfg.ServerAddr); err != nil {
		return fmt.Errorf("connect to %s: %w", clientCfg.ServerAddr, err)
	}
	defer client.Disconnect(context.Background())

	logger.Info().Str("client_id", client.GetID()).Msg("connected to Shared Publisher")

	for idx, action := range cfg.Actions {
		logger := logger.With().Int("step", idx+1).Str("action", action.Type).Logger()
		switch action.Type {
		case "submit-xt", "submit_xt", "submit-xt-request":
			if err := executeSubmitXT(ctx, client, action, logger); err != nil {
				return fmt.Errorf("action %d (%s): %w", idx+1, action.Type, err)
			}
		case "send-vote", "vote":
			if err := executeSendVote(ctx, client, store, action, logger); err != nil {
				return fmt.Errorf("action %d (%s): %w", idx+1, action.Type, err)
			}
		case "wait", "sleep":
			if err := executeWait(action, logger); err != nil {
				return fmt.Errorf("action %d (%s): %w", idx+1, action.Type, err)
			}
		default:
			return fmt.Errorf("action %d: unsupported type %q", idx+1, action.Type)
		}
	}

	if cfg.WaitWindow.Duration > 0 {
		logger.Info().Dur("wait", cfg.WaitWindow.Duration).Msg("waiting for responses")
		time.Sleep(cfg.WaitWindow.Duration)
	}

	return nil
}

func executeWait(action actionSpec, logger zerolog.Logger) error {
	if action.Duration.Duration <= 0 {
		return errors.New("wait action requires a positive duration")
	}
	logger.Info().Dur("duration", action.Duration.Duration).Msg("sleeping")
	time.Sleep(action.Duration.Duration)
	return nil
}

func executeSubmitXT(ctx context.Context, client transport.Client, action actionSpec, logger zerolog.Logger) error {
	if len(action.Chains) == 0 {
		// Support backwards-compatible fields: chain_id with optional transactions in action.
		if action.ChainID != "" {
			action.Chains = []xtChain{{
				ChainID:      action.ChainID,
				Transactions: action.Transactions,
			}}
		} else {
			return errors.New("submit-xt action requires at least one chain")
		}
	}

	reqs := make([]*pb.TransactionRequest, 0, len(action.Chains))
	for i, chain := range action.Chains {
		if chain.ChainID == "" {
			return fmt.Errorf("chain #%d is missing chain_id", i+1)
		}
		chainID, err := parseChainID(chain.ChainID)
		if err != nil {
			return fmt.Errorf("chain #%d invalid chain_id: %w", i+1, err)
		}

		txs, err := buildTransactions(chain)
		if err != nil {
			return fmt.Errorf("chain #%d: %w", i+1, err)
		}

		reqs = append(reqs, &pb.TransactionRequest{
			ChainId:     chainID,
			Transaction: txs,
		})
	}

	msg := &pb.Message{
		SenderId: client.GetID(),
		Payload: &pb.Message_XtRequest{
			XtRequest: &pb.XTRequest{
				TransactionRequests: reqs,
			},
		},
	}

	sendCtx, cancel := context.WithTimeout(ctx, defaultSendTimeout)
	defer cancel()

	logger.Info().Int("chains", len(reqs)).Msg("submitting XTRequest")
	if err := client.Send(sendCtx, msg); err != nil {
		return fmt.Errorf("send XTRequest: %w", err)
	}
	return nil
}

func buildTransactions(chain xtChain) ([][]byte, error) {
	var txs [][]byte

	switch {
	case len(chain.Transactions) > 0:
		txs = make([][]byte, 0, len(chain.Transactions))
		for idx, tx := range chain.Transactions {
			data, err := decodeHexString(tx, 0)
			if err != nil {
				return nil, fmt.Errorf("transaction #%d: %w", idx+1, err)
			}
			txs = append(txs, data)
		}
	default:
		count := chain.RandomCount
		if count <= 0 {
			count = defaultRandomTxCount
		}
		size := chain.RandomBytes
		if size <= 0 {
			size = defaultRandomTxSize
		}
		txs = make([][]byte, 0, count)
		for i := 0; i < count; i++ {
			payload := make([]byte, size)
			if _, err := rand.Read(payload); err != nil {
				return nil, fmt.Errorf("generate random transaction: %w", err)
			}
			txs = append(txs, payload)
		}
	}

	return txs, nil
}

func executeSendVote(ctx context.Context, client transport.Client, store *instanceStore, action actionSpec, logger zerolog.Logger) error {
	if action.ChainID == "" {
		return errors.New("send-vote requires chain_id")
	}
	if action.Vote == "" {
		return errors.New("send-vote requires vote value")
	}

	chainID, err := parseChainID(action.ChainID)
	if err != nil {
		return fmt.Errorf("parse chain_id: %w", err)
	}

	var instanceID []byte
	if action.InstanceID != "" {
		instanceID, err = decodeHexString(action.InstanceID, 0)
		if err != nil {
			return fmt.Errorf("parse instance_id: %w", err)
		}
		if len(instanceID) == 0 {
			return errors.New("instance_id decoded to empty byte slice")
		}
	} else {
		timeout := action.Timeout.Duration
		if timeout <= 0 {
			timeout = defaultInstanceWaitLimit
		}
		waitCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		inst, err := store.waitForInstance(waitCtx)
		if err != nil {
			return fmt.Errorf("wait for start instance: %w", err)
		}
		instanceID = inst.GetInstanceId()
		logger = logger.With().Str("instance_source", "latest_start_instance").Logger()
	}

	voteBool, err := parseVote(action.Vote)
	if err != nil {
		return fmt.Errorf("parse vote: %w", err)
	}

	msg := &pb.Message{
		SenderId: client.GetID(),
		Payload: &pb.Message_Vote{
			Vote: &pb.Vote{
				InstanceId: instanceID,
				ChainId:    chainID,
				Vote:       voteBool,
			},
		},
	}

	sendCtx, cancel := context.WithTimeout(ctx, defaultSendTimeout)
	defer cancel()

	logger.Info().
		Str("instance_id", fmt.Sprintf("%x", instanceID)).
		Bool("vote", voteBool).
		Msg("sending vote")

	if err := client.Send(sendCtx, msg); err != nil {
		return fmt.Errorf("send vote: %w", err)
	}
	return nil
}

func parseChainID(input string) (uint64, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return 0, errors.New("empty chain_id")
	}

	base := 10
	if strings.HasPrefix(value, "0x") || strings.HasPrefix(value, "0X") {
		base = 16
		value = value[2:]
	}

	if value == "" {
		return 0, errors.New("invalid chain_id")
	}

	id := new(big.Int)
	if _, ok := id.SetString(value, base); !ok {
		return 0, fmt.Errorf("unable to parse chain_id %q", input)
	}

	if id.Sign() < 0 {
		return 0, errors.New("chain_id must be non-negative")
	}

	if !id.IsUint64() {
		return 0, fmt.Errorf("chain_id %q overflows uint64", input)
	}
	return id.Uint64(), nil
}

func decodeHexString(input string, expectedLen int) ([]byte, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return nil, errors.New("empty hex value")
	}
	if strings.HasPrefix(value, "0x") || strings.HasPrefix(value, "0X") {
		value = value[2:]
	}
	if len(value)%2 != 0 {
		value = "0" + value
	}
	data, err := hex.DecodeString(value)
	if err != nil {
		return nil, err
	}
	if expectedLen > 0 && len(data) != expectedLen {
		return nil, fmt.Errorf("expected %d bytes, got %d", expectedLen, len(data))
	}
	return data, nil
}

func parseVote(input string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(input)) {
	case "true", "commit", "1", "yes":
		return true, nil
	case "false", "abort", "0", "no":
		return false, nil
	default:
		return false, fmt.Errorf("invalid vote value %q", input)
	}
}

// Ensure dependencies are retained when building via go build ./...

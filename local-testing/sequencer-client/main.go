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
	"strings"
	"time"

	"github.com/compose-network/publisher/x/auth"
	"github.com/compose-network/publisher/x/transport"
	"github.com/compose-network/publisher/x/transport/tcp"
	pb "github.com/compose-network/specs/compose/proto"
	"github.com/ethereum/go-ethereum/common"
	"github.com/rs/zerolog"
)

type commandFlags struct {
	spAddr     string
	clientID   string
	privateKey string
	action     string

	chainID    string
	chainID1   string
	chainID2   string
	instanceID string
	voteValue  string
	waitWindow time.Duration
}

func main() {
	flags := parseFlags()

	if err := run(flags); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func parseFlags() commandFlags {
	var flags commandFlags
	flag.StringVar(&flags.spAddr, "sp-addr", "127.0.0.1:8080", "Shared Publisher TCP endpoint")
	flag.StringVar(&flags.clientID, "client-id", "", "Optional client identifier (defaults to random UUID)")
	flag.StringVar(&flags.privateKey, "private-key", "", "Hex-encoded secp256k1 private key for auth handshake")
	flag.StringVar(&flags.action, "action", "", "Action to perform: handshake|submit-xt|send-vote|send-ping")

	flag.StringVar(&flags.chainID, "chain-id", "", "Chain ID (decimal or 0x-prefixed hex)")
	flag.StringVar(&flags.chainID1, "chain-id1", "", "Chain ID1 (decimal or 0x-prefixed hex)")
	flag.StringVar(&flags.chainID2, "chain-id2", "", "Chain ID2 (decimal or 0x-prefixed hex)")
	flag.StringVar(&flags.instanceID, "instance-id", "", "Hex-encoded instance identifier (for votes)")
	flag.StringVar(&flags.voteValue, "vote", "", "Vote value for send-vote: commit|abort|true|false")
	flag.DurationVar(&flags.waitWindow, "wait", 2*time.Second, "Time to keep the connection open after sending the message")

	flag.Parse()

	if flags.action == "" {
		flag.Usage()
		fmt.Fprintln(os.Stderr, "\nmissing required flag: -action")
		os.Exit(2)
	}

	return flags
}

func run(cfg commandFlags) error {
	logger := zerolog.New(zerolog.ConsoleWriter{
		Out:        os.Stdout,
		TimeFormat: time.RFC3339,
	}).Level(zerolog.InfoLevel).With().Timestamp().Logger()

	clientCfg := tcp.DefaultClientConfig()
	clientCfg.ServerAddr = cfg.spAddr
	clientCfg.ClientID = cfg.clientID

	var client transport.Client = tcp.NewClient(clientCfg, logger)

	if cfg.privateKey != "" {
		authManager, err := auth.NewManagerFromHex(cfg.privateKey)
		if err != nil {
			return fmt.Errorf("invalid private key: %w", err)
		}
		if authManager == nil {
			return errors.New("failed to create auth manager")
		}
		client = client.WithAuth(authManager)
		logger = logger.With().Str("address", authManager.Address()).Logger()
	}

	client.SetHandler(func(ctx context.Context, msg *pb.Message) ([]common.Hash, error) {
		switch payload := msg.Payload.(type) {
		case *pb.Message_Decided:
			logger.Info().
				Str("instance_id", fmt.Sprintf("%x", payload.Decided.GetInstanceId())).
				Bool("decision", payload.Decided.GetDecision()).
				Msg("received decision from SP")
		case *pb.Message_StartInstance:
			logger.Info().
				Str("instance_id", fmt.Sprintf("%x", payload.StartInstance.GetInstanceId())).
				Uint64("period_id", payload.StartInstance.GetPeriodId()).
				Uint64("sequence", payload.StartInstance.GetSequenceNumber()).
				Msg("received StartInstance from SP")
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

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := client.Connect(ctx, clientCfg.ServerAddr); err != nil {
		return fmt.Errorf("connect to %s: %w", clientCfg.ServerAddr, err)
	}
	defer client.Disconnect(context.Background())

	logger.Info().Str("client_id", client.GetID()).Msg("connected to Shared Publisher")

	switch cfg.action {
	case "handshake":
		// No additional payload; successful Connect implies handshake passed.
		logger.Info().Msg("handshake completed successfully")
	case "submit-xt":
		if err := sendXTRequest(ctx, client, cfg); err != nil {
			logger.Error().Err(err).Msg("failed to submit XT request")
			return err
		}
	case "send-vote":
		if err := sendVote(ctx, client, cfg); err != nil {
			logger.Error().Err(err).Msg("failed to send vote")
			return err
		}
	case "send-ping":
		if err := sendPing(ctx, client); err != nil {
			logger.Error().Err(err).Msg("failed to send ping")
			return err
		}
	default:
		return fmt.Errorf("unsupported action %q", cfg.action)
	}

	if cfg.waitWindow > 0 {
		logger.Info().Dur("wait", cfg.waitWindow).Msg("waiting for responses")
		time.Sleep(cfg.waitWindow)
	}

	return nil
}

func sendXTRequest(ctx context.Context, client transport.Client, cfg commandFlags) error {
	if cfg.chainID1 == "" {
		return errors.New("submit-xt requires -chain-id1")
	}
	if cfg.chainID2 == "" {
		return errors.New("submit-xt requires -chain-id2")
	}

	chainID1, err := parseChainID(cfg.chainID1)
	if err != nil {
		return fmt.Errorf("parse chain-id: %w", err)
	}
	chainID2, err := parseChainID(cfg.chainID2)
	if err != nil {
		return fmt.Errorf("parse chain-id: %w", err)
	}

	// txBytes is a random sequence of bytes
	txBytes1 := make([]byte, 256)
	_, err = rand.Read(txBytes1)
	txBytes2 := make([]byte, 256)
	_, err = rand.Read(txBytes2)

	msg := &pb.Message{
		SenderId: client.GetID(),
		Payload: &pb.Message_XtRequest{
			XtRequest: &pb.XTRequest{
				TransactionRequests: []*pb.TransactionRequest{
					{
						ChainId:     chainID1,
						Transaction: [][]byte{txBytes1},
					},
					{
						ChainId:     chainID2,
						Transaction: [][]byte{txBytes2},
					},
				},
			},
		},
	}

	if err := client.Send(ctx, msg); err != nil {
		return fmt.Errorf("send XTRequest: %w", err)
	}
	return nil
}

func sendVote(ctx context.Context, client transport.Client, cfg commandFlags) error {
	if cfg.chainID == "" {
		return errors.New("send-vote requires -chain-id")
	}
	if cfg.instanceID == "" {
		return errors.New("send-vote requires -instance-id")
	}
	if cfg.voteValue == "" {
		return errors.New("send-vote requires -vote")
	}

	chainID, err := parseChainID(cfg.chainID)
	if err != nil {
		return fmt.Errorf("parse chain-id: %w", err)
	}

	instanceID, err := decodeHexString(cfg.instanceID, 0)
	if err != nil {
		return fmt.Errorf("parse instance-id: %w", err)
	}
	if len(instanceID) == 0 {
		return errors.New("instance-id decoded to empty byte slice")
	}

	voteBool, err := parseVote(cfg.voteValue)
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

	if err := client.Send(ctx, msg); err != nil {
		return fmt.Errorf("send vote: %w", err)
	}
	return nil
}

func sendPing(ctx context.Context, client transport.Client) error {
	msg := &pb.Message{
		SenderId: client.GetID(),
		Payload: &pb.Message_Ping{
			Ping: &pb.Ping{
				Timestamp: time.Now().UnixMilli(),
			},
		},
	}

	if err := client.Send(ctx, msg); err != nil {
		return fmt.Errorf("send ping: %w", err)
	}
	return nil
}

func parseChainID(input string) (uint64, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return 0, errors.New("empty chain-id")
	}

	base := 10
	if strings.HasPrefix(value, "0x") || strings.HasPrefix(value, "0X") {
		base = 16
		value = value[2:]
	}

	if value == "" {
		return 0, errors.New("invalid chain-id")
	}

	id := new(big.Int)
	if _, ok := id.SetString(value, base); !ok {
		return 0, fmt.Errorf("unable to parse chain-id %q", input)
	}

	if id.Sign() < 0 {
		return 0, errors.New("chain-id must be non-negative")
	}

	if !id.IsUint64() {
		return 0, fmt.Errorf("chain-id %q overflows uint64", input)
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

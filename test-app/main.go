package main

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"math/big"
	"time"

	apilog "github.com/ssvlabs/rollup-shared-publisher/log"
	pb "github.com/ssvlabs/rollup-shared-publisher/proto/rollup/v1"
	"github.com/ssvlabs/rollup-shared-publisher/x/auth"
)

//nolint:gocyclo // its test app, we don't care about
func main() {
	fmt.Printf("CHainID: 1 = seq-1, 2 = seq-2: %v\n", hex.EncodeToString(big.NewInt(55555).Bytes()))
	fmt.Printf("CHainID: 2 = seq-1, 2 = seq-2: %v\n", hex.EncodeToString(big.NewInt(66666).Bytes()))
	var (
		spAddr     string
		chainID    int
		initiate   bool
		pretty     bool
		logLevel   string
		privateKey string
		spPub      string
		noAuth     bool
	)
	flag.StringVar(&spAddr, "sp-addr", "localhost:8080", "Shared Publisher address")
	flag.IntVar(&chainID, "chain-id", 1, "Sequencer chain ID (integer)")
	flag.BoolVar(&initiate, "initiate", false, "Whether this instance initiates the XT request")
	flag.BoolVar(&pretty, "log-pretty", true, "Pretty console logs")
	flag.StringVar(&logLevel, "log-level", "debug", "Log level (trace,debug,info,...) ")
	flag.StringVar(
		&privateKey,
		"private-key",
		"",
		"Hex ECDSA private key for auth (compressed pub must be trusted by SP)",
	)
	flag.StringVar(&spPub, "sp-pub", "", "Compressed hex public key of Shared Publisher (optional)")
	flag.BoolVar(&noAuth, "no-auth", false, "Disable authentication handshake entirely (overrides --private-key)")
	flag.Parse()

	logger := apilog.New(logLevel, pretty)
	log := logger.Module("test-app")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Determine default private keys for convenience if not provided, unless no-auth is set
	if !noAuth && privateKey == "" {
		switch chainID {
		case 1:
			privateKey = "3afc6aa26dcfb78f93b2df978e41b2d89449e7951670763717265ab0a552aae0"
		case 2:
			privateKey = "1e33f16449a0b646f672b0a5415bed21310d388effb7d3b95816d1c12c492f74"
		}
	}
	if spPub == "" {
		// default to SP pubkey configured in shared-publisher-leader-app/configs/config.yaml
		spPub = "034f2a8d175528ed60f64b7c3a5d5e72cf2aa3acda444b33e16fdfb3e3e4326ce5"
	}

	// Single sequencer instance; run two processes to simulate 2 nodes
	// Use mock IDs that match the SP registry service
	var chainIDBytes []byte
	switch chainID {
	case 1:
		chainIDBytes = big.NewInt(55555).Bytes() // mockID1
	case 2:
		chainIDBytes = big.NewInt(66666).Bytes() // mockID2
	default:
		chainIDBytes = []byte{byte(chainID & 0xFF)} // fallback for other IDs
	}
	// Initialize auth manager if key provided
	var authMgr auth.Manager
	if !noAuth && privateKey != "" {
		var err error
		authMgr, err = auth.NewManagerFromHex(privateKey)
		if err != nil {
			log.Error().Err(err).Msg("invalid private key")
			return
		}
		log.Info().
			Str("pub_key", authMgr.PublicKeyString()).
			Str("address", authMgr.Address()).
			Msg("Auth enabled for sequencer")
		// Trust SP public key for verify-known
		if spPub != "" {
			if pk, err := hex.DecodeString(spPub); err == nil {
				_ = authMgr.AddTrustedKey("shared-publisher", pk)
				log.Info().Str("trusted", "shared-publisher").Msg("Added SP public key to trusted list")
			} else {
				log.Warn().Err(err).Msg("Invalid --sp-pub; skipping trust")
			}
		}
	} else {
		log.Warn().Msg("Running without auth (no handshake)")
	}

	seq := NewSequencer(fmt.Sprintf("seq-%d", chainID), chainIDBytes, spAddr, log.Logger, authMgr)
	if err := seq.Start(ctx); err != nil {
		log.Error().Err(err).Msg("start sequencer failed")
		return
	}
	log.Info().Int("chain_id", chainID).Msg("Sequencer started")

	// Allow connections to establish
	time.Sleep(500 * time.Millisecond)

	if initiate {
		// Build an XT request with both chains participating using mock chain IDs
		xt := &pb.XTRequest{
			Transactions: []*pb.TransactionRequest{
				{ChainId: big.NewInt(55555).Bytes(), Transaction: [][]byte{[]byte("tx-1-a"), []byte("tx-1-b")}}, // mockID1
				{ChainId: big.NewInt(66666).Bytes(), Transaction: [][]byte{[]byte("tx-2-a")}},                   // mockID2
			},
		}
		if err := seq.InitiateXT(ctx, xt); err != nil {
			log.Error().Err(err).Msg("initiate XT failed")
			return
		}

		// Wait for commit decision from SP (only when initiating XT)
		decided := waitDecided(seq, 15*time.Second)
		if decided == nil {
			log.Error().Msg("timed out waiting for Decided from SP")
			return
		}
		if !decided.Decision {
			log.Error().Msg("transaction aborted by SP")
			return
		}
		log.Info().Str("xt_id", decided.XtId.Hex()).Msg("Commit decided by SP")

		// Continue participating in additional slots
		log.Info().Msg("Continuing to participate in additional slots...")
		time.Sleep(40 * time.Second) // Additional slots after XT
	} else {
		// For non-initiating sequencers, just wait for slot participation
		log.Info().Msg("Waiting for slot participation (3-4 slots)...")
		time.Sleep(50 * time.Second) // Wait for 3-4 slots (12s each)
	}

	// Let messages flush
	time.Sleep(500 * time.Millisecond)

	log.Info().Msg("Test completed: Multi-slot execution finished")
}

func waitDecided(s *Sequencer, timeout time.Duration) *pb.Decided {
	select {
	case d := <-s.decidedCh:
		return d
	case <-time.After(timeout):
		return nil
	}
}

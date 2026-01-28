// Package config provides configuration management for the compose sidecar.
package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config holds all sidecar configuration.
type Config struct {
	Server    ServerConfig    `mapstructure:"server"`
	Publisher PublisherConfig `mapstructure:"publisher"`
	Chains    ChainsConfig    `mapstructure:"chains"`
	Peers     PeersConfig     `mapstructure:"peers"`
	Log       LogConfig       `mapstructure:"log"`
}

// PeersConfig holds configuration for peer sidecars (for CIRC).
type PeersConfig struct {
	Sidecars []PeerSidecarConfig `mapstructure:"sidecars"`
}

// PeerSidecarConfig holds configuration for a peer sidecar.
type PeerSidecarConfig struct {
	ChainID uint64 `mapstructure:"chain_id"`
	Addr    string `mapstructure:"addr"`
}

// ServerConfig holds HTTP server configuration.
type ServerConfig struct {
	ListenAddr   string        `mapstructure:"listen_addr"`
	ReadTimeout  time.Duration `mapstructure:"read_timeout"`
	WriteTimeout time.Duration `mapstructure:"write_timeout"`
}

// PublisherConfig holds shared publisher connection configuration.
type PublisherConfig struct {
	Addr           string        `mapstructure:"addr"`
	ReconnectDelay time.Duration `mapstructure:"reconnect_delay"`
	MaxRetries     int           `mapstructure:"max_retries"`
	Enabled        bool          `mapstructure:"enabled"` // If false, run in standalone mode
}

// ChainsConfig holds configuration for participating chains.
type ChainsConfig struct {
	Chains []ChainConfig `mapstructure:"list"`
}

// ChainConfig holds configuration for a single chain.
type ChainConfig struct {
	ID             uint64 `mapstructure:"id"`
	Name           string `mapstructure:"name"`
	RPC            string `mapstructure:"rpc"`
	MailboxAddress string `mapstructure:"mailbox_address"`
	CoordinatorKey string `mapstructure:"coordinator_key"` // Private key for signing putInbox transactions
}

// LogConfig holds logging configuration.
type LogConfig struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
}

// DefaultConfig returns configuration with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			ListenAddr:   ":8080",
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 30 * time.Second,
		},
		Publisher: PublisherConfig{
			Addr:           "",
			ReconnectDelay: 5 * time.Second,
			MaxRetries:     10,
			Enabled:        false, // Standalone mode by default for v2
		},
		Chains: ChainsConfig{
			Chains: []ChainConfig{},
		},
		Log: LogConfig{
			Level:  "info",
			Format: "json",
		},
	}
}

// Load loads configuration from file and environment variables.
func Load(configPath string) (*Config, error) {
	v := viper.New()

	// Set defaults
	defaults := DefaultConfig()
	v.SetDefault("server.listen_addr", defaults.Server.ListenAddr)
	v.SetDefault("server.read_timeout", defaults.Server.ReadTimeout)
	v.SetDefault("server.write_timeout", defaults.Server.WriteTimeout)
	v.SetDefault("publisher.addr", defaults.Publisher.Addr)
	v.SetDefault("publisher.reconnect_delay", defaults.Publisher.ReconnectDelay)
	v.SetDefault("publisher.max_retries", defaults.Publisher.MaxRetries)
	v.SetDefault("log.level", defaults.Log.Level)
	v.SetDefault("log.format", defaults.Log.Format)

	// Load from file if specified
	if configPath != "" {
		v.SetConfigFile(configPath)
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("read config file: %w", err)
		}
	}

	// Bind environment variables
	v.SetEnvPrefix("SIDECAR")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	// Support single-chain configuration via environment variables
	// This is useful for Docker where each container handles one chain
	cfg.applyEnvOverrides()

	return &cfg, nil
}

// applyEnvOverrides applies environment variable overrides for Docker convenience.
// This allows configuring a single chain via environment variables.
func (c *Config) applyEnvOverrides() {
	v := viper.New()
	v.SetEnvPrefix("SIDECAR")
	v.AutomaticEnv()

	// Direct listen address override
	listenAddr := v.GetString("LISTEN_ADDR")
	if listenAddr != "" {
		c.Server.ListenAddr = listenAddr
	}

	// Publisher configuration override
	publisherAddr := v.GetString("PUBLISHER_ADDR")
	if publisherAddr != "" {
		c.Publisher.Addr = publisherAddr
		c.Publisher.Enabled = true // Enable publisher if address is provided
	}

	// Explicit publisher enable/disable
	if v.IsSet("PUBLISHER_ENABLED") {
		c.Publisher.Enabled = v.GetBool("PUBLISHER_ENABLED")
	}

	// Single chain configuration via env vars
	chainID := v.GetUint64("CHAIN_ID")
	chainRPC := v.GetString("CHAIN_RPC")
	mailboxAddr := v.GetString("MAILBOX_ADDRESS")
	coordinatorKey := v.GetString("COORDINATOR_KEY")

	if chainID > 0 && chainRPC != "" {
		// Add or update the chain configuration
		found := false
		for i := range c.Chains.Chains {
			if c.Chains.Chains[i].ID == chainID {
				c.Chains.Chains[i].RPC = chainRPC
				if mailboxAddr != "" {
					c.Chains.Chains[i].MailboxAddress = mailboxAddr
				}
				if coordinatorKey != "" {
					c.Chains.Chains[i].CoordinatorKey = coordinatorKey
				}
				found = true
				break
			}
		}
		if !found {
			c.Chains.Chains = append(c.Chains.Chains, ChainConfig{
				ID:             chainID,
				Name:           fmt.Sprintf("chain-%d", chainID),
				RPC:            chainRPC,
				MailboxAddress: mailboxAddr,
				CoordinatorKey: coordinatorKey,
			})
		}
	}

	// Peer sidecars configuration via env vars
	// Format: SIDECAR_PEER_A_ADDR, SIDECAR_PEER_B_ADDR, etc.
	for _, suffix := range []string{"A", "B", "C", "D"} {
		peerAddr := v.GetString(fmt.Sprintf("PEER_%s_ADDR", suffix))
		peerChainID := v.GetUint64(fmt.Sprintf("PEER_%s_CHAIN_ID", suffix))
		if peerAddr != "" {
			// Derive chain ID from suffix if not specified
			if peerChainID == 0 {
				// Convention: A=901, B=902, C=903, D=904
				peerChainID = 900 + uint64(suffix[0]-'A'+1)
			}
			c.Peers.Sidecars = append(c.Peers.Sidecars, PeerSidecarConfig{
				ChainID: peerChainID,
				Addr:    peerAddr,
			})
		}
	}
}

// GetChainByID returns chain configuration by ID.
func (c *Config) GetChainByID(id uint64) (*ChainConfig, bool) {
	for i := range c.Chains.Chains {
		if c.Chains.Chains[i].ID == id {
			return &c.Chains.Chains[i], true
		}
	}
	return nil, false
}

// ChainIDs returns a slice of all configured chain IDs.
func (c *Config) ChainIDs() []uint64 {
	ids := make([]uint64, len(c.Chains.Chains))
	for i, chain := range c.Chains.Chains {
		ids[i] = chain.ID
	}
	return ids
}

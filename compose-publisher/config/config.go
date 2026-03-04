package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config holds the complete application configuration
type Config struct {
	Server    ServerConfig    `mapstructure:"server"    yaml:"server"`
	API       APIServerConfig `mapstructure:"api"       yaml:"api"`
	Consensus ConsensusConfig `mapstructure:"consensus" yaml:"consensus"`
	Metrics   MetricsConfig   `mapstructure:"metrics"   yaml:"metrics"`
	Log       LogConfig       `mapstructure:"log"       yaml:"log"`
}

// ServerConfig holds QUIC server configuration
type ServerConfig struct {
	ListenAddr   string        `mapstructure:"listen_addr"   yaml:"listen_addr"   env:"SERVER_LISTEN_ADDR"`
	WriteTimeout time.Duration `mapstructure:"write_timeout" yaml:"write_timeout" env:"SERVER_WRITE_TIMEOUT"`
}

// APIServerConfig holds HTTP API server configuration
type APIServerConfig struct {
	ListenAddr        string        `mapstructure:"listen_addr"         yaml:"listen_addr"`
	ReadHeaderTimeout time.Duration `mapstructure:"read_header_timeout" yaml:"read_header_timeout"`
	ReadTimeout       time.Duration `mapstructure:"read_timeout"        yaml:"read_timeout"`
	WriteTimeout      time.Duration `mapstructure:"write_timeout"       yaml:"write_timeout"`
	IdleTimeout       time.Duration `mapstructure:"idle_timeout"        yaml:"idle_timeout"`
	MaxHeaderBytes    int           `mapstructure:"max_header_bytes"    yaml:"max_header_bytes"`
}

// ConsensusConfig holds consensus configuration
type ConsensusConfig struct {
	Timeout time.Duration `mapstructure:"timeout" yaml:"timeout" env:"CONSENSUS_TIMEOUT"`
	Role    string        `mapstructure:"role"    yaml:"role"    env:"CONSENSUS_ROLE"`
}

// MetricsConfig holds metrics configuration
type MetricsConfig struct {
	Enabled bool   `mapstructure:"enabled" yaml:"enabled" env:"METRICS_ENABLED"`
	Port    int    `mapstructure:"port"    yaml:"port"    env:"METRICS_PORT"`
	Path    string `mapstructure:"path"    yaml:"path"    env:"METRICS_PATH"`
}

// LogConfig holds logging configuration
type LogConfig struct {
	Level  string `mapstructure:"level"  yaml:"level"  env:"LOG_LEVEL"`
	Pretty bool   `mapstructure:"pretty" yaml:"pretty" env:"LOG_PRETTY"`
	Output string `mapstructure:"output" yaml:"output" env:"LOG_OUTPUT"`
	File   string `mapstructure:"file"   yaml:"file"   env:"LOG_FILE"`
}

// Load loads configuration from file and environment
func Load(configPath string) (*Config, error) {
	v := viper.New()

	v.SetConfigFile(configPath)
	v.SetConfigType("yaml")
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	setDefaults(v)

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", configPath, err)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return &cfg, nil
}

// setDefaults sets default configuration values
func setDefaults(v *viper.Viper) {
	v.SetDefault("server.listen_addr", ":8080")
	v.SetDefault("server.write_timeout", "30s")

	v.SetDefault("api.listen_addr", ":8081")
	v.SetDefault("api.read_header_timeout", "5s")
	v.SetDefault("api.read_timeout", "15s")
	v.SetDefault("api.write_timeout", "30s")
	v.SetDefault("api.idle_timeout", "120s")
	v.SetDefault("api.max_header_bytes", 1048576)

	v.SetDefault("consensus.timeout", "60s")

	v.SetDefault("metrics.enabled", true)
	v.SetDefault("metrics.port", 8081)
	v.SetDefault("metrics.path", "/metrics")

	v.SetDefault("log.level", "info")
	v.SetDefault("log.pretty", false)
	v.SetDefault("log.output", "stdout")
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if err := c.validateServer(); err != nil {
		return err
	}
	if err := c.validateConsensus(); err != nil {
		return err
	}
	if err := c.validateMetrics(); err != nil {
		return err
	}
	return nil
}

func (c *Config) validateServer() error {
	if strings.TrimSpace(c.Server.ListenAddr) == "" {
		return fmt.Errorf("server.listen_addr must not be empty")
	}
	return nil
}

func (c *Config) validateConsensus() error {
	if c.Consensus.Timeout <= 0 {
		return fmt.Errorf("consensus.timeout must be positive")
	}
	return nil
}

func (c *Config) validateMetrics() error {
	if c.Metrics.Enabled && (c.Metrics.Port <= 0 || c.Metrics.Port > 65535) {
		return fmt.Errorf("metrics.port must be between 1-65535 when metrics enabled, got %d", c.Metrics.Port)
	}
	return nil
}

// Default returns default configuration
func Default() *Config {
	return &Config{
		Server: ServerConfig{
			ListenAddr:   ":8080",
			WriteTimeout: 30 * time.Second,
		},
		API: APIServerConfig{
			ListenAddr:        ":8081",
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       15 * time.Second,
			WriteTimeout:      30 * time.Second,
			IdleTimeout:       120 * time.Second,
			MaxHeaderBytes:    1 << 20,
		},
		Consensus: ConsensusConfig{
			Timeout: 60 * time.Second,
			Role:    "leader",
		},
		Metrics: MetricsConfig{
			Enabled: true,
			Port:    8081,
			Path:    "/metrics",
		},
		Log: LogConfig{
			Level:  "info",
			Pretty: false,
			Output: "stdout",
		},
	}
}

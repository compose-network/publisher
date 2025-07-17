package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	Server  ServerConfig  `mapstructure:"server"`
	Metrics MetricsConfig `mapstructure:"metrics"`
	Log     LogConfig     `mapstructure:"log"`
}

type ServerConfig struct {
	ListenAddr     string        `mapstructure:"listen_addr" env:"SERVER_LISTEN_ADDR"`
	WriteTimeout   time.Duration `mapstructure:"write_timeout" env:"SERVER_WRITE_TIMEOUT"`
	MaxMessageSize int           `mapstructure:"max_message_size" env:"SERVER_MAX_MESSAGE_SIZE"`
	MaxConnections int           `mapstructure:"max_connections" env:"SERVER_MAX_CONNECTIONS"`
}

type MetricsConfig struct {
	Enabled bool   `mapstructure:"enabled" env:"METRICS_ENABLED"`
	Port    int    `mapstructure:"port" env:"METRICS_PORT"`
	Path    string `mapstructure:"path" env:"METRICS_PATH"`
}

type LogConfig struct {
	Level  string `mapstructure:"level" env:"LOG_LEVEL"`
	Pretty bool   `mapstructure:"pretty" env:"LOG_PRETTY"`
	Output string `mapstructure:"output" env:"LOG_OUTPUT"` // stdout, stderr, file
	File   string `mapstructure:"file" env:"LOG_FILE"`     // log file path if output=file
}

func Load(path string) (*Config, error) {
	viper.SetConfigFile(path)
	viper.SetConfigType("yaml")

	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	// Set defaults
	viper.SetDefault("server.listen_addr", ":8080")
	viper.SetDefault("server.write_timeout", "30s")
	viper.SetDefault("server.max_message_size", 10*1024*1024) // 10MB
	viper.SetDefault("server.max_connections", 100)

	viper.SetDefault("metrics.enabled", true)
	viper.SetDefault("metrics.port", 8081)
	viper.SetDefault("metrics.path", "/metrics")

	viper.SetDefault("log.level", "info")
	viper.SetDefault("log.pretty", false)
	viper.SetDefault("log.output", "stdout")

	if err := viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return &cfg, nil
}

func (c *Config) Validate() error {
	if c.Server.MaxMessageSize <= 0 {
		return fmt.Errorf("server.max_message_size must be positive")
	}
	if c.Server.MaxConnections <= 0 {
		return fmt.Errorf("server.max_connections must be positive")
	}

	if c.Metrics.Enabled && c.Metrics.Port <= 0 {
		return fmt.Errorf("metrics.port must be positive when metrics enabled")
	}

	return nil
}

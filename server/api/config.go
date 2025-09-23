package api

import "time"

// Config defines runtime parameters for the HTTP API server.
type Config struct {
	ListenAddr        string        `mapstructure:"listen_addr" yaml:"listen_addr"`
	ReadHeaderTimeout time.Duration `mapstructure:"read_header_timeout" yaml:"read_header_timeout"`
	ReadTimeout       time.Duration `mapstructure:"read_timeout" yaml:"read_timeout"`
	WriteTimeout      time.Duration `mapstructure:"write_timeout" yaml:"write_timeout"`
	IdleTimeout       time.Duration `mapstructure:"idle_timeout" yaml:"idle_timeout"`
	MaxHeaderBytes    int           `mapstructure:"max_header_bytes" yaml:"max_header_bytes"`
}

func DefaultConfig() Config {
	return Config{
		ListenAddr:        ":8081",
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1MB
	}
}

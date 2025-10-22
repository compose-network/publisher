package main

import (
	"fmt"
	"os"
	"runtime"

	"github.com/compose-network/publisher/log"
	"github.com/spf13/cobra"

	"github.com/compose-network/publisher/publisher-leader-app/config"
)

var (
	cfgFile string
	rootCmd = &cobra.Command{
		Use:   "publisher",
		Short: "Publisher",
		Long:  banner + "\n\nA shared publisher for coordinating cross-chain transactions across rollups.",
		RunE:  runApp,
	}

	versionCmd = &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run:   runVersion,
	}
)

const banner = `
██████╗ ██╗   ██╗██████╗ ██╗     ██╗███████╗██╗  ██╗███████╗██████╗
██╔══██╗██║   ██║██╔══██╗██║     ██║██╔════╝██║  ██║██╔════╝██╔══██╗
██████╔╝██║   ██║██████╔╝██║     ██║███████╗███████║█████╗  ██████╔╝
██╔═══╝ ██║   ██║██╔══██╗██║     ██║╚════██║██╔══██║██╔══╝  ██╔══██╗
██║     ╚██████╔╝██████╔╝███████╗██║███████║██║  ██║███████╗██║  ██║
╚═╝      ╚═════╝ ╚═════╝ ╚══════╝╚═╝╚══════╝╚═╝  ╚═╝╚══════╝╚═╝  ╚═╝`

func main() {
	if err := execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func execute() error {
	initCommands()
	return rootCmd.Execute()
}

func initCommands() {
	cobra.OnInitialize(initConfig)

	// Add subcommands
	rootCmd.AddCommand(versionCmd)

	// Global flags
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config",
		"publisher-leader-app/configs/config.yaml", "config file path")
	rootCmd.PersistentFlags().String("log-level", "", "log level (trace, debug, info, warn, error)")
	rootCmd.PersistentFlags().Bool("log-pretty", false, "enable pretty logging")

	// Server flags
	rootCmd.PersistentFlags().String("listen-addr", "", "server listen address")
	rootCmd.PersistentFlags().Int("max-connections", 0, "maximum concurrent connections")
	rootCmd.PersistentFlags().Duration("read-timeout", 0, "connection read timeout")
	rootCmd.PersistentFlags().Duration("write-timeout", 0, "connection write timeout")

	// Metrics flags
	rootCmd.PersistentFlags().Bool("metrics", false, "enable metrics")
	rootCmd.PersistentFlags().Int("metrics-port", 0, "metrics server port")

	// Registry flags
	rootCmd.PersistentFlags().String("registry.path", "", "path to override the registry directory")

	// L1 flags
	rootCmd.PersistentFlags().Uint64("l1.chain-id", 0, "L1 chain id used to select network from registry")
}

func initConfig() {
	if cfgFile == "" {
		cfgFile = "publisher-leader-app/configs/config.yaml"
	}
}

func runApp(cmd *cobra.Command, _ []string) error {
	fmt.Println(banner)
	fmt.Println()

	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	applyFlags(cmd, cfg)

	log := log.New(cfg.Log.Level, cfg.Log.Pretty)

	log.Info().
		Str("version", Version).
		Str("build_time", BuildTime).
		Str("git_commit", GitCommit).
		Str("go_version", runtime.Version()).
		Msg("Build information")

	log.Info().
		Str("config_file", cfgFile).
		Str("listen_addr", cfg.Server.ListenAddr).
		Int("metrics_port", cfg.Metrics.Port).
		Bool("metrics_enabled", cfg.Metrics.Enabled).
		Str("log_level", cfg.Log.Level).
		Msg("Configuration loaded")

	application, err := NewApp(cmd.Context(), cfg, log.Logger)
	if err != nil {
		return fmt.Errorf("failed to create application: %w", err)
	}

	return application.Run(cmd.Context())
}

func runVersion(*cobra.Command, []string) {
	fmt.Println(banner)
	fmt.Println()
	fmt.Printf("Publisher\n")
	fmt.Printf("Version:    %s\n", Version)
	fmt.Printf("Build Time: %s\n", BuildTime)
	fmt.Printf("Git Commit: %s\n", GitCommit)
	fmt.Printf("Go Version: %s\n", runtime.Version())
	fmt.Printf("OS/Arch:    %s/%s\n", runtime.GOOS, runtime.GOARCH)
}

func applyFlags(cmd *cobra.Command, cfg *config.Config) {
	if cmd.Flag("log-level").Changed {
		cfg.Log.Level, _ = cmd.Flags().GetString("log-level")
	}
	if cmd.Flag("log-pretty").Changed {
		cfg.Log.Pretty, _ = cmd.Flags().GetBool("log-pretty")
	}

	if cmd.Flag("listen-addr").Changed {
		cfg.Server.ListenAddr, _ = cmd.Flags().GetString("listen-addr")
	}
	if cmd.Flag("max-connections").Changed {
		cfg.Server.MaxConnections, _ = cmd.Flags().GetInt("max-connections")
	}
	if cmd.Flag("read-timeout").Changed {
		cfg.Server.ReadTimeout, _ = cmd.Flags().GetDuration("read-timeout")
	}
	if cmd.Flag("write-timeout").Changed {
		cfg.Server.WriteTimeout, _ = cmd.Flags().GetDuration("write-timeout")
	}

	if cmd.Flag("metrics").Changed {
		cfg.Metrics.Enabled, _ = cmd.Flags().GetBool("metrics")
	}
	if cmd.Flag("metrics-port").Changed {
		cfg.Metrics.Port, _ = cmd.Flags().GetInt("metrics-port")
	}

	if cmd.Flag("registry.path").Changed {
		cfg.Registry.Path, _ = cmd.Flags().GetString("registry.path")
	}
	if cmd.Flag("l1.chain-id").Changed {
		if v, err := cmd.Flags().GetUint64("l1.chain-id"); err == nil {
			cfg.L1.ChainID = v
		}
	}
}

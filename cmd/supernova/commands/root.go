package commands

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/lmittmann/tint"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	cmtcfg "github.com/cometbft/cometbft/config"
	"github.com/cometbft/cometbft/libs/cli"
)

var (
	verbose bool
	config  = DefaultConfig()
)

func DefaultConfig() *cmtcfg.Config {
	config := cmtcfg.DefaultConfig()
	config.BaseConfig.RootDir = os.ExpandEnv(filepath.Join("$HOME", ".supernova"))
	config.ProxyApp = "noop"
	return config
}

func init() {
	registerFlagsRootCmd(RootCmd)
}

func registerFlagsRootCmd(cmd *cobra.Command) {
	cmd.PersistentFlags().String("log_level", config.LogLevel, "log level")
}

// ParseConfig retrieves the default environment configuration,
// sets up the CometBFT root and ensures that the root exists.
func ParseConfig(cmd *cobra.Command) (*cmtcfg.Config, error) {
	conf := DefaultConfig()
	err := viper.Unmarshal(conf)
	if err != nil {
		return nil, err
	}

	var home string
	switch {
	case os.Getenv("CMTHOME") != "":
		home = os.Getenv("CMTHOME")
	case os.Getenv("TMHOME") != "":
		// XXX: Deprecated.
		home = os.Getenv("TMHOME")
		slog.Error("Deprecated environment variable TMHOME identified. CMTHOME should be used instead.")
	default:
		home, err = cmd.Flags().GetString(cli.HomeFlag)
		if err != nil {
			return nil, err
		}
	}

	conf.RootDir = home

	conf.SetRoot(conf.RootDir)
	cmtcfg.EnsureRoot(conf.RootDir)
	if err := conf.ValidateBasic(); err != nil {
		return nil, fmt.Errorf("error in config file: %v", err)
	}
	if warnings := conf.CheckDeprecated(); len(warnings) > 0 {
		for _, warning := range warnings {
			slog.Info("deprecated usage found in configuration file", "usage", warning)
		}
	}
	fmt.Println("P2P Listen Address: ", conf.P2P.ListenAddress)
	return conf, nil
}

// RootCmd is the root command for CometBFT core.
var RootCmd = &cobra.Command{
	Use:   "supernova",
	Short: "Hotstuff2 state machine replication for applications in any programming languages",
	PersistentPreRunE: func(cmd *cobra.Command, _ []string) (err error) {
		if cmd.Name() == VersionCmd.Name() {
			return nil
		}

		config, err = ParseConfig(cmd)
		if err != nil {
			return err
		}

		InitLogger(config)

		return nil
	},
}

func InitLogger(config *cmtcfg.Config) {
	lvl := config.BaseConfig.LogLevel
	logLevel := slog.LevelDebug
	switch lvl {
	case "INFO":
	case "info":
		logLevel = slog.LevelInfo
		break
	case "WARN":
	case "warn":
		logLevel = slog.LevelWarn
		break
	case "ERROR":
	case "error":
		logLevel = slog.LevelError
		break
	}
	slog.Info(fmt.Sprintf("set slog default level: %v", logLevel))
	// set global logger with custom options
	w := os.Stdout

	// set global logger with custom options
	slog.SetDefault(slog.New(
		tint.NewHandler(w, &tint.Options{
			Level:      logLevel,
			TimeFormat: time.DateTime,
		}),
	))
}

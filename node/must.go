// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package node

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	cmtdb "github.com/cometbft/cometbft-db"
	cmtcfg "github.com/cometbft/cometbft/config"
	"github.com/lmittmann/tint"
	"github.com/meterio/supernova/chain"
)

func InitLogger(config *cmtcfg.Config) {
	lvl := config.BaseConfig.LogLevel
	logLevel := slog.LevelDebug
	switch lvl {
	case "DEBUG":
	case "debug":
		logLevel = slog.LevelDebug
		break
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
	default:
		logLevel = slog.LevelInfo
	}
	fmt.Println("cmtlog level: ", lvl)
	fmt.Println("slog   level: ", logLevel)
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

func NewChain(mainDB cmtdb.DB) *chain.Chain {

	chain, err := chain.New(mainDB, true)
	if err != nil {
		fatal("initialize block chain:", err)
	}

	return chain
}

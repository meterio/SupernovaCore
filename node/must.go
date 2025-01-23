// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package node

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	db "github.com/cometbft/cometbft-db"
	cmtcfg "github.com/cometbft/cometbft/config"
	"github.com/ethereum/go-ethereum/common/fdlimit"
	"github.com/lmittmann/tint"
	"github.com/meterio/supernova/chain"
	"github.com/meterio/supernova/libs/lvldb"
	cli "gopkg.in/urfave/cli.v1"
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

func OpenMainDB(ctx *cli.Context, dataDir string) *lvldb.LevelDB {
	if _, err := fdlimit.Raise(5120 * 4); err != nil {
		fatal("failed to increase fd limit", err)
	}
	limit, err := fdlimit.Current()
	if err != nil {
		fatal("failed to get fd limit:", err)
	}
	if limit <= 1024 {
		slog.Warn("low fd limit, increase it if possible", "limit", limit)
	} else {
		slog.Info("fd limit", "limit", limit)
	}

	fileCache := limit / 2
	if fileCache > 1024 {
		fileCache = 1024
	}
	if fileCache > 4096 {
		fileCache = 4096
	}

	dir := filepath.Join(dataDir, "main.db")
	db, err := lvldb.New(dir, lvldb.Options{
		CacheSize:              128,
		OpenFilesCacheCapacity: fileCache,
	})
	if err != nil {
		fatal(fmt.Sprintf("open chain database [%v]: %v", dir, err))
	}
	return db
}

func NewChain(mainDB db.DB) *chain.Chain {

	chain, err := chain.New(mainDB, true)
	if err != nil {
		fatal("initialize block chain:", err)
	}

	return chain
}

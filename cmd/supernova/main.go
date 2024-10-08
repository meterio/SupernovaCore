// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package main

import (
	"os"
	"path/filepath"
	"time"

	_ "net/http/pprof"

	"github.com/cometbft/cometbft/cmd/cometbft/commands/debug"
	cmtcfg "github.com/cometbft/cometbft/config"
	"github.com/cometbft/cometbft/libs/cli"
	"github.com/meterio/supernova/cmd/supernova/commands"
	"github.com/meterio/supernova/node"
	"github.com/meterio/supernova/txpool"
)

var (
	gitCommit string
	gitTag    string
	keyStr    string

	defaultTxPoolOptions = txpool.Options{
		Limit:           200000,
		LimitPerAccount: 1024, /*16,*/ //XXX: increase to 1024 from 16 during the testing
		MaxLifetime:     20 * time.Minute,
	}
)

const (
	statePruningBatch = 1024
	indexPruningBatch = 256
	// indexFlatterningBatch = 1024
	GCInterval = 5 * 60 * 1000 // 5 min in millisecond

	blockPruningBatch = 1024
)

func main() {
	rootCmd := commands.RootCmd
	rootCmd.AddCommand(
		node.RunNodeCmd,
		debug.DebugCmd,
		commands.InitFilesCmd,
		cli.NewCompletionCmd(rootCmd, true),
	)

	cmd := cli.PrepareBaseCmd(rootCmd, "CMT", os.ExpandEnv(filepath.Join("$HOME", cmtcfg.DefaultCometDir)))
	if err := cmd.Execute(); err != nil {
		panic(err)
	}
}

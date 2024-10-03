// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package main

import (
	"fmt"
	"os"
	"time"

	"net/http"
	_ "net/http/pprof"

	cmtproxy "github.com/cometbft/cometbft/proxy"
	"github.com/meterio/supernova/node"
	"github.com/meterio/supernova/txpool"
	cli "gopkg.in/urfave/cli.v1"
)

var (
	version   string
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

func fullVersion() string {
	versionMeta := "release"
	if gitTag == "" {
		versionMeta = "dev"
	}
	return fmt.Sprintf("%s-%s-%s", version, gitCommit, versionMeta)
}

func main() {
	go func() {
		fmt.Println(http.ListenAndServe("localhost:6060", nil))
	}()
	app := cli.App{
		Version:   fullVersion(),
		Name:      "Supernova",
		Usage:     "Node of Supernova",
		Copyright: "2024 Decentralized Finance Lab",
		Flags:     node.Flags,
		Action:    defaultAction,
		Commands:  []cli.Command{},
	}

	if err := app.Run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func defaultAction(ctx *cli.Context) error {
	clientCreator := cmtproxy.NewLocalClientCreator(NewDumbApplication())

	return node.New(
		ctx,
		fullVersion(),
		clientCreator).
		Run()
}

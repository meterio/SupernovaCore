// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"net/http"
	_ "net/http/pprof"

	cmtproxy "github.com/cometbft/cometbft/proxy"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/meterio/supernova/api"
	"github.com/meterio/supernova/api/doc"
	"github.com/meterio/supernova/cmd/supernova/node"
	"github.com/meterio/supernova/consensus"
	"github.com/meterio/supernova/genesis"
	"github.com/meterio/supernova/txpool"
	"github.com/meterio/supernova/types"
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
		Name:      "Meter",
		Usage:     "Node of Meter.io",
		Copyright: "2018 Meter Foundation <https://types.io/>",
		Flags: []cli.Flag{
			dataDirFlag,
			apiAddrFlag,
			apiCorsFlag,
			apiTimeoutFlag,
			apiBacktraceLimitFlag,
			verbosityFlag,
			maxPeersFlag,
			p2pPortFlag,
			natFlag,
			peersFlag,
			noDiscoverFlag,
			minCommitteeSizeFlag,
			maxCommitteeSizeFlag,
			discoServerFlag,
			discoTopicFlag,
			epochBlockCountFlag,
		},
		Action: defaultAction,
		Commands: []cli.Command{
			{Name: "keys", Usage: "export keys", Flags: []cli.Flag{dataDirFlag}, Action: keysAction},
			{Name: "enode-id", Usage: "display enode-id", Flags: []cli.Flag{dataDirFlag, p2pPortFlag}, Action: showEnodeIDAction},
			{Name: "peers", Usage: "export peers", Flags: []cli.Flag{dataDirFlag}, Action: peersAction},
		},
	}

	if err := app.Run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func showEnodeIDAction(ctx *cli.Context) error {
	key, err := loadOrGeneratePrivateKey(filepath.Join(ctx.String("data-dir"), "p2p.key"))
	if err != nil {
		fatal("load or generate P2P key:", err)
	}
	node := enode.NewV4(&key.PublicKey, net.IP{}, 0, 0)
	// id := node.ID()
	port := ctx.Int(p2pPortFlag.Name)
	// fmt.Printf("enode://%v@[]:%d\n", id, port)
	fmt.Printf("%v@[]:%d\n", node.String(), port)
	return nil
}

func peersAction(ctx *cli.Context) error {
	initLogger(ctx)

	fmt.Println("Peers from peers.cache")

	baseDir := ctx.String(dataDirFlag.Name)
	gene := genesis.LoadGenesis(baseDir)
	dirConfigs := ensureDirs(ctx, gene)

	peersCachePath := path.Join(dirConfigs.InstanceDir, "peers.cache")
	nodes := make([]string, 0)
	if data, err := os.ReadFile(peersCachePath); err != nil {
		if !os.IsNotExist(err) {
			fmt.Println("failed to load peers cache", "err", err)
			return err
		}
	} else {
		// fmt.Println("loaded from peers.cache: ", string(data))
		nodes = strings.Split(string(data), "\n")
	}
	for i, n := range nodes {
		fmt.Printf("Node #%d: %s\n", i, n)
	}
	fmt.Println("End.")
	return nil
}

func defaultAction(ctx *cli.Context) error {
	exitSignal := handleExitSignal()
	debug.SetMemoryLimit(5 * 1024 * 1024 * 1024) // 5GB

	fmt.Println("ensure dir: ", ctx.String("data-dir"))
	defer func() { slog.Info("exited") }()

	initLogger(ctx)

	baseDir := ctx.String(dataDirFlag.Name)
	gene := genesis.LoadGenesis(baseDir)
	dirConfig := ensureDirs(ctx, gene)

	slog.Info("Meter Start ...")
	mainDB := openMainDB(ctx, dirConfig.InstanceDir)
	defer func() { slog.Info("closing main database..."); mainDB.Close() }()

	chain := initChain(gene, mainDB)

	// if flattern index start is not set, or pruning is not complete
	// start the pruning routine right now

	keyLoader := types.NewKeyLoader(ctx.String("data-dir"))
	blsMaster, err := keyLoader.Load()
	if err != nil {
		panic(err)
	}

	config := consensus.ReactorConfig{
		MinCommitteeSize: ctx.Int("committee-min-size"),
		MaxCommitteeSize: ctx.Int("committee-max-size"),
		EpochMBlockCount: consensus.MIN_MBLOCKS_AN_EPOCH,
	}

	// set magic
	topic := ctx.String("disco-topic")
	version := doc.Version()
	versionItems := strings.Split(version, ".")
	maskedVersion := version
	if len(versionItems) > 1 {
		maskedVersion = strings.Join(versionItems[:len(versionItems)-1], ".") + ".0"
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%v %v", maskedVersion, topic)))
	slog.Info("Version", "maskedVersion", maskedVersion, "version", version, "topic", topic, "sum", hex.EncodeToString(sum[:]), "magic", hex.EncodeToString(sum[:4]))

	// Split magic to p2p_magic and consensus_magic
	copy(p2pMagic[:], sum[:4])
	copy(consensusMagic[:], sum[:4])

	txPool := txpool.New(chain, defaultTxPoolOptions)
	defer func() { slog.Info("closing tx pool..."); txPool.Close() }()

	p2pcom := newP2PComm(ctx, exitSignal, chain, txPool, dirConfig.InstanceDir, p2pMagic)

	proxyApp := cmtproxy.NewLocalClientCreator(NewDumbApplication())

	apiHandler, apiCloser := api.New(chain, txPool, p2pcom.comm, ctx.String(apiCorsFlag.Name), uint32(ctx.Int(apiBacktraceLimitFlag.Name)), p2pcom.p2pSrv)
	defer func() { slog.Info("closing API..."); apiCloser() }()

	apiURL, srvCloser := startAPIServer(ctx, apiHandler, chain.GenesisBlock().ID())
	defer func() { slog.Info("stopping API server..."); srvCloser() }()

	printStartupMessage(topic, gene, chain, blsMaster, dirConfig.InstanceDir, apiURL)

	p2pcom.Start()
	defer p2pcom.Stop()

	return node.New(
		fullVersion(),
		chain,
		blsMaster,
		txPool,
		filepath.Join(dirConfig.InstanceDir, "tx.stash"),
		p2pcom.comm,
		proxyApp, config).
		Run(exitSignal)
}

func keysAction(ctx *cli.Context) error {
	keyloader := types.NewKeyLoader(ctx.String("data-dir"))
	blsMaster, err := keyloader.Load()
	if err != nil {
		fmt.Println("Err: ", err)
	} else {
		blsMaster.Print()
	}
	return nil
}

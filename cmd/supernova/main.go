// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"net/http"
	_ "net/http/pprof"

	cmtproxy "github.com/cometbft/cometbft/proxy"
	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/google/uuid"
	isatty "github.com/mattn/go-isatty"
	"github.com/meterio/meter-pov/api"
	"github.com/meterio/meter-pov/api/doc"
	"github.com/meterio/meter-pov/cmd/supernova/node"
	"github.com/meterio/meter-pov/consensus"
	"github.com/meterio/meter-pov/meter"
	"github.com/meterio/meter-pov/preset"
	"github.com/meterio/meter-pov/txpool"
	"github.com/meterio/meter-pov/types"
	"github.com/pkg/errors"
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
		Copyright: "2018 Meter Foundation <https://meter.io/>",
		Flags: []cli.Flag{
			networkFlag,
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
			maxDelegateSizeFlag,
			discoServerFlag,
			discoTopicFlag,
			initCfgdDelegatesFlag,
			epochBlockCountFlag,
			httpsCertFlag,
			httpsKeyFlag,
		},
		Action: defaultAction,
		Commands: []cli.Command{
			{Name: "master-key", Usage: "import and export master key", Flags: []cli.Flag{dataDirFlag, importMasterKeyFlag, exportMasterKeyFlag}, Action: masterKeyAction},
			{Name: "enode-id", Usage: "display enode-id", Flags: []cli.Flag{dataDirFlag, p2pPortFlag}, Action: showEnodeIDAction},
			{Name: "public-key", Usage: "export public key", Flags: []cli.Flag{dataDirFlag}, Action: publicKeyAction},
			{Name: "peers", Usage: "export peers", Flags: []cli.Flag{networkFlag, dataDirFlag}, Action: peersAction},
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

func publicKeyAction(ctx *cli.Context) error {
	makeDataDir(ctx)
	keyLoader := NewKeyLoader(ctx)
	blsMaster, err := keyLoader.Load()
	if err != nil {
		fatal("error load keys", err)
	}

	fmt.Println(hex.EncodeToString(blsMaster.PrivKey.Marshal()))
	return nil
}

func peersAction(ctx *cli.Context) error {
	initLogger(ctx)

	fmt.Println("Peers from peers.cache")
	// init blockchain config
	meter.InitBlockChainConfig(ctx.String(networkFlag.Name))

	gene := selectGenesis(ctx)
	instanceDir := makeInstanceDir(ctx, gene)
	peersCachePath := path.Join(instanceDir, "peers.cache")
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

	defer func() { slog.Info("exited") }()

	initLogger(ctx)

	// init blockchain config
	meter.InitBlockChainConfig(ctx.String(networkFlag.Name))

	gene := selectGenesis(ctx)
	instanceDir := makeInstanceDir(ctx, gene)
	makeSnapshotDir(ctx)

	slog.Info("Meter Start ...")
	mainDB := openMainDB(ctx, instanceDir)
	defer func() { slog.Info("closing main database..."); mainDB.Close() }()

	chain := initChain(gene, mainDB)

	// if flattern index start is not set, or pruning is not complete
	// start the pruning routine right now

	keyLoader := NewKeyLoader(ctx)
	blsMaster, err := keyLoader.Load()
	if err != nil {
		panic(err)
	}

	// load preset config
	if "warringstakes" == ctx.String(networkFlag.Name) {
		config := preset.TestnetPresetConfig
		if ctx.IsSet("committee-min-size") {
			config.CommitteeMinSize = ctx.Int("committee-min-size")
		} else {
			ctx.Set("committee-min-size", strconv.Itoa(config.CommitteeMinSize))
		}

		if ctx.IsSet("committee-max-size") {
			config.CommitteeMaxSize = ctx.Int("committee-max-size")
		} else {
			ctx.Set("committee-max-size", strconv.Itoa(config.CommitteeMaxSize))
		}

		if ctx.IsSet("delegate-max-size") {
			config.DelegateMaxSize = ctx.Int("committee-max-size")
		} else {
			ctx.Set("delegate-max-size", strconv.Itoa(config.DelegateMaxSize))
		}

		if ctx.IsSet("disco-topic") {
			config.DiscoTopic = ctx.String("disco-topic")
		} else {
			ctx.Set("disco-topic", config.DiscoTopic)
		}

		if ctx.IsSet("disco-server") {
			config.DiscoServer = ctx.String("disco-server")
		} else {
			ctx.Set("disco-server", config.DiscoServer)
		}
	} else if "main" == ctx.String(networkFlag.Name) {
		config := preset.MainnetPresetConfig
		if ctx.IsSet("committee-min-size") {
			config.CommitteeMinSize = ctx.Int("committee-min-size")
		} else {
			ctx.Set("committee-min-size", strconv.Itoa(config.CommitteeMinSize))
		}

		if ctx.IsSet("committee-max-size") {
			config.CommitteeMaxSize = ctx.Int("committee-max-size")
		} else {
			ctx.Set("committee-max-size", strconv.Itoa(config.CommitteeMaxSize))
		}

		if ctx.IsSet("delegate-max-size") {
			config.DelegateMaxSize = ctx.Int("committee-max-size")
		} else {
			ctx.Set("delegate-max-size", strconv.Itoa(config.DelegateMaxSize))
		}

		if ctx.IsSet("disco-topic") {
			config.DiscoTopic = ctx.String("disco-topic")
		} else {
			ctx.Set("disco-topic", config.DiscoTopic)
		}

		if ctx.IsSet("disco-server") {
			config.DiscoServer = ctx.String("disco-server")
		} else {
			ctx.Set("disco-server", config.DiscoServer)
		}
	} else if "staging" == ctx.String(networkFlag.Name) {
		config := preset.MainnetPresetConfig
		if ctx.IsSet("committee-min-size") {
			config.CommitteeMinSize = ctx.Int("committee-min-size")
		} else {
			ctx.Set("committee-min-size", strconv.Itoa(config.CommitteeMinSize))
		}

		if ctx.IsSet("committee-max-size") {
			config.CommitteeMaxSize = ctx.Int("committee-max-size")
		} else {
			ctx.Set("committee-max-size", strconv.Itoa(config.CommitteeMaxSize))
		}

		if ctx.IsSet("delegate-max-size") {
			config.DelegateMaxSize = ctx.Int("committee-max-size")
		} else {
			ctx.Set("delegate-max-size", strconv.Itoa(config.DelegateMaxSize))
		}
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

	// load delegates (from binary or from file)
	initDelegates := types.LoadDelegatesFile(ctx.String("network"), ctx.String("data-dir"), blsMaster)
	printDelegates(initDelegates)

	txPool := txpool.New(chain, defaultTxPoolOptions)
	defer func() { slog.Info("closing tx pool..."); txPool.Close() }()

	p2pcom := newP2PComm(ctx, exitSignal, chain, txPool, instanceDir, p2pMagic)

	proxyApp := cmtproxy.NewLocalClientCreator(NewDumbApplication())

	config := consensus.ReactorConfig{
		InitCfgdDelegates: ctx.Bool("init-configured-delegates"),
		EpochMBlockCount:  uint32(ctx.Uint("epoch-mblock-count")),
		MinCommitteeSize:  ctx.Int("committee-min-size"),
		MaxCommitteeSize:  ctx.Int("committee-max-size"),
		MaxDelegateSize:   ctx.Int("delegate-max-size"),
		InitDelegates:     initDelegates,
	}

	apiHandler, apiCloser := api.New(chain, txPool, p2pcom.comm, ctx.String(apiCorsFlag.Name), uint32(ctx.Int(apiBacktraceLimitFlag.Name)), p2pcom.p2pSrv)
	defer func() { slog.Info("closing API..."); apiCloser() }()

	apiURL, srvCloser := startAPIServer(ctx, apiHandler, chain.GenesisBlock().ID())
	defer func() { slog.Info("stopping API server..."); srvCloser() }()

	printStartupMessage(topic, gene, chain, blsMaster, instanceDir, apiURL)

	p2pcom.Start()
	defer p2pcom.Stop()

	return node.New(
		fullVersion(),
		chain,
		blsMaster,
		txPool,
		filepath.Join(instanceDir, "tx.stash"),
		p2pcom.comm,
		proxyApp, config).
		Run(exitSignal)
}

func masterKeyAction(ctx *cli.Context) error {
	hasImportFlag := ctx.Bool(importMasterKeyFlag.Name)
	hasExportFlag := ctx.Bool(exportMasterKeyFlag.Name)
	if hasImportFlag && hasExportFlag {
		return fmt.Errorf("flag %s and %s are exclusive", importMasterKeyFlag.Name, exportMasterKeyFlag.Name)
	}

	if !hasImportFlag && !hasExportFlag {
		return fmt.Errorf("missing flag, either %s or %s", importMasterKeyFlag.Name, exportMasterKeyFlag.Name)
	}

	if hasImportFlag {
		if isatty.IsTerminal(os.Stdin.Fd()) {
			fmt.Println("Input JSON keystore (end with ^d):")
		}
		keyjson, err := io.ReadAll(os.Stdin)
		if err != nil {
			return err
		}

		if err := json.Unmarshal(keyjson, &map[string]interface{}{}); err != nil {
			return errors.WithMessage(err, "unmarshal")
		}
		password, err := readPasswordFromNewTTY("Enter passphrase: ")
		if err != nil {
			return err
		}

		key, err := keystore.DecryptKey(keyjson, password)
		if err != nil {
			return errors.WithMessage(err, "decrypt")
		}

		if err := crypto.SaveECDSA(masterKeyPath(ctx), key.PrivateKey); err != nil {
			return err
		}
		fmt.Println("Master key imported:", meter.Address(key.Address))
		return nil
	}

	if hasExportFlag {
		masterKey, err := loadOrGeneratePrivateKey(masterKeyPath(ctx))
		if err != nil {
			return err
		}

		password, err := readPasswordFromNewTTY("Enter passphrase: ")
		if err != nil {
			return err
		}
		if password == "" {
			return errors.New("non-empty passphrase required")
		}
		confirm, err := readPasswordFromNewTTY("Confirm passphrase: ")
		if err != nil {
			return err
		}

		if password != confirm {
			return errors.New("passphrase confirmation mismatch")
		}
		id, _ := uuid.NewRandom()
		keyjson, err := keystore.EncryptKey(&keystore.Key{
			PrivateKey: masterKey,
			Address:    crypto.PubkeyToAddress(masterKey.PublicKey),
			Id:         id},
			password, keystore.StandardScryptN, keystore.StandardScryptP)
		if err != nil {
			return err
		}
		if isatty.IsTerminal(os.Stdout.Fd()) {
			fmt.Println("=== JSON keystore ===")
		}
		_, err = fmt.Println(string(keyjson))
		return err
	}
	return nil
}

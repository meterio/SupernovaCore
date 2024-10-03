// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package node

import (
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common/fdlimit"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/nat"
	"github.com/lmittmann/tint"
	"github.com/meterio/supernova/chain"
	"github.com/meterio/supernova/cmd/supernova/probe"
	"github.com/meterio/supernova/consensus"
	"github.com/meterio/supernova/genesis"
	"github.com/meterio/supernova/libs/co"
	"github.com/meterio/supernova/libs/comm"
	"github.com/meterio/supernova/libs/lvldb"
	"github.com/meterio/supernova/p2psrv"
	"github.com/meterio/supernova/txpool"
	"github.com/meterio/supernova/types"
	"github.com/prysmaticlabs/prysm/v5/crypto/bls"
	cli "gopkg.in/urfave/cli.v1"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	p2pMagic       [4]byte
	consensusMagic [4]byte
)

func InitLogger(ctx *cli.Context) {
	logLevel := ctx.Int(verbosityFlag.Name)
	fmt.Println("slog level: ", slog.Level(logLevel))
	// set global logger with custom options
	w := os.Stderr

	// set global logger with custom options
	slog.SetDefault(slog.New(
		tint.NewHandler(w, &tint.Options{
			Level:      slog.Level(logLevel),
			TimeFormat: time.DateTime,
		}),
	))
}

type ConfigDirs struct {
	BaseDir     string
	InstanceDir string
	SnapshotDir string
}

func EnsureDirs(ctx *cli.Context, gene *genesis.Genesis) ConfigDirs {
	baseDir := ctx.String(dataDirFlag.Name)
	err := os.MkdirAll(baseDir, 0700)
	if err != nil {
		fatal("could not create data-dir")
	}

	instanceDir := filepath.Join(baseDir, fmt.Sprintf("instance-%v", gene.ChainId))
	if err := os.MkdirAll(instanceDir, 0700); err != nil {
		fatal(fmt.Sprintf("create instance dir [%v]: %v", instanceDir, err))
	}

	snapshotDir := filepath.Join(baseDir, "snapshot")
	if err := os.MkdirAll(snapshotDir, 0700); err != nil {
		fatal(fmt.Sprintf("create snapshot dir [%v]: %v", snapshotDir, err))
	}
	return ConfigDirs{
		BaseDir:     baseDir,
		InstanceDir: instanceDir,
		SnapshotDir: snapshotDir,
	}
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

func InitChain(gene *genesis.Genesis, mainDB *lvldb.LevelDB) *chain.Chain {
	genesisBlock, err := gene.Build()
	if err != nil {
		fatal("build genesis block: ", err)
	}

	chain, err := chain.New(mainDB, genesisBlock, gene.ValidatorSet(), true)
	if err != nil {
		fatal("initialize block chain:", err)
	}

	return chain
}

func discoServerParse(ctx *cli.Context) ([]*enode.Node, bool, error) {

	nd := ctx.StringSlice(discoServerFlag.Name)
	if len(nd) == 0 {
		return []*enode.Node{}, false, nil
	}

	nodes := make([]*enode.Node, 0)
	for _, n := range nd {
		node, err := enode.ParseV4(n)
		if err != nil {
			return []*enode.Node{}, false, err
		}

		nodes = append(nodes, node)
	}

	return nodes, true, nil
}

type p2pComm struct {
	comm           *comm.Communicator
	p2pSrv         *p2psrv.Server
	peersCachePath string
}

func NewP2PComm(cliCtx *cli.Context, ctx context.Context, chain *chain.Chain, txPool *txpool.TxPool, instanceDir string, magic [4]byte) *p2pComm {
	key, err := loadOrGeneratePrivateKey(filepath.Join(cliCtx.String("data-dir"), "p2p.key"))
	if err != nil {
		fatal("load or generate P2P key:", err)
	}

	nat, err := nat.Parse(cliCtx.String(natFlag.Name))
	if err != nil {
		cli.ShowAppHelp(cliCtx)
		fmt.Println("parse -nat flag:", err)
		os.Exit(1)
	}

	discoSvr, overrided, err := discoServerParse(cliCtx)
	if err != nil {
		cli.ShowAppHelp(cliCtx)
		fmt.Println("parse bootstrap nodes failed:", err)
		os.Exit(1)
	}

	// if the discoverServerFlag is not set, use default hardcoded nodes
	var BootstrapNodes []*enode.Node
	if overrided == true {
		BootstrapNodes = discoSvr
	}

	opts := &p2psrv.Options{
		Name:           types.MakeName("supernova", "v0.0.1" /* TODO: change this */),
		PrivateKey:     key,
		MaxPeers:       cliCtx.Int(maxPeersFlag.Name),
		ListenAddr:     fmt.Sprintf(":%v", cliCtx.Int(p2pPortFlag.Name)),
		BootstrapNodes: BootstrapNodes,
		NAT:            nat,
		NoDiscovery:    cliCtx.Bool("no-discover"),
	}

	peersCachePath := filepath.Join(instanceDir, "peers.cache")

	cachedPeers := make([]string, 0)
	if data, err := os.ReadFile(peersCachePath); err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("failed to load peers cache", "err", err)
		}
	} else {
		cachedPeers = strings.Split(string(data), "\n")
	}

	// load peers from peers.cache
	for _, p := range cachedPeers {
		node, err := enode.ParseV4(p)
		if err == nil {
			opts.BootstrapNodes = append(opts.BootstrapNodes, node)
			slog.Info("load peer from cache", "peer", node.String())
		} else {
			slog.Warn("cant parse peer from cache", "peer", p)
		}
	}

	// load peers from cli flags
	inputPeers := cliCtx.StringSlice("peers")
	for _, p := range inputPeers {
		node, err := enode.ParseV4(p)
		if err == nil {
			opts.BootstrapNodes = append(opts.BootstrapNodes, node)
		} else {
			fmt.Println("could not parse peer: ", p)
		}
	}

	topic := cliCtx.String("disco-topic")

	return &p2pComm{
		comm:           comm.New(ctx, chain, txPool, topic, magic),
		p2pSrv:         p2psrv.New(opts),
		peersCachePath: peersCachePath,
	}
}

func (p *p2pComm) Start() {
	start := time.Now()
	if err := p.p2pSrv.Start(p.comm.Protocols()); err != nil {
		fatal("start P2P server:", err)
	}
	slog.Info("P2P server started", "elapsed", types.PrettyDuration(time.Since(start)))
	start = time.Now()
	p.comm.Start()
	slog.Info("communicator started", "elapsed", types.PrettyDuration(time.Since(start)))
}

func (p *p2pComm) Stop() {
	slog.Info("stopping communicator...")
	p.comm.Stop()

	slog.Info("stopping P2P server...")
	p.p2pSrv.Stop()

	nodes := p.p2pSrv.KnownNodes()
	slog.Info("saving peers cache...", "#peers", len(nodes))
	strs := make([]string, 0)
	for _, n := range nodes {
		strs = append(strs, n.String())
	}
	data := strings.Join(strs, "\n")
	if err := os.WriteFile(p.peersCachePath, []byte(data), 0600); err != nil {
		slog.Warn("failed to write peers cache", "err", err)
	}
}

type Dispatcher struct {
	cons *consensus.Reactor
}

func (d *Dispatcher) handlePeers(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	// api_utils.WriteJSON(w, d.nw.PeersStats())
}

func StartObserveServer(cons *consensus.Reactor, blsPubKey bls.PublicKey, nw probe.Network, chain *chain.Chain) (string, func()) {
	addr := ":8670"
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		fatal(fmt.Sprintf("listen observe addr [%v]: %v", addr, err))
	}
	probe := &probe.Probe{cons, blsPubKey, chain, "v0.0.1", nw}
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/probe", probe.HandleProbe)
	mux.HandleFunc("/probe/version", probe.HandleVersion)
	mux.HandleFunc("/probe/peers", probe.HandlePeers)

	// dispatch the msg to reactor/pacemaker
	mux.HandleFunc("/pacemaker", cons.OnReceiveMsg)

	srv := &http.Server{
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
	var goes co.Goes
	goes.Go(func() {
		err := srv.Serve(listener)
		if err != nil {
			if err != http.ErrServerClosed {
				fmt.Println("observe server stopped, error:", err)
			}
		}

	})
	return "http://" + listener.Addr().String() + "/", func() {
		err := srv.Close()
		if err != nil {
			fmt.Println("can't close observe http service, error:", err)
		}
		goes.Wait()
	}
}

func StartAPIServer(ctx *cli.Context, handler http.Handler, genesisID types.Bytes32) (string, func()) {
	addr := ctx.String(apiAddrFlag.Name)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		fatal(fmt.Sprintf("listen API addr [%v]: %v", addr, err))
	}

	timeout := ctx.Int(apiTimeoutFlag.Name)
	if timeout > 0 {
		handler = handleAPITimeout(handler, time.Duration(timeout)*time.Millisecond)
	}
	handler = handleXGenesisID(handler, genesisID)
	handler = handleXVersion(handler)
	handler = requestBodyLimit(handler)
	srv := &http.Server{
		Handler:      handler,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 18 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
	var goes co.Goes
	goes.Go(func() {
		err := srv.Serve(listener)
		if err != nil {
			slog.Warn(err.Error())
		}
	})

	returnStr := "http://" + listener.Addr().String() + "/"
	var tlsSrv *http.Server
	returnStr = returnStr + " | https service is disabled due to missing cert/key file"
	return returnStr, func() {
		err := srv.Close()
		if err != nil {
			fmt.Println("could not close API service, error:", err)
		}
		if tlsSrv != nil {
			err = tlsSrv.Close()
			if err != nil {
				fmt.Println("can't close API https service, error:", err)
			}
		}

		goes.Wait()
	}
}

func printStartupMessage(
	topic string,
	gene *genesis.Genesis,
	chain *chain.Chain,
	blsMaster *types.BlsMaster,
	dataDir string,
	apiURL string,
) {
	bestBlock := chain.BestBlock()

	fmt.Printf(`Starting %v
    Discover Topic  [ %v ]
    Magic           [ %v p2p & consensus ]
    Network         [ %v %v ]    
    Best block      [ %v #%v @%v ]
    Forks           [ %v ]
    PubKey          [ %v ]
    Instance dir    [ %v ]
    API portal      [ %v ]
`,
		types.MakeName("Supernova", "v0.0.1"),
		topic,
		hex.EncodeToString(p2pMagic[:]),
		gene.ID(), gene.Name,
		bestBlock.ID(), bestBlock.Number(), time.Unix(int64(bestBlock.Timestamp()), 0),
		types.GetForkConfig(gene.ID()),
		hex.EncodeToString(blsMaster.PubKey.Marshal()),
		dataDir,
		apiURL)
}

func openMemMainDB() *lvldb.LevelDB {
	db, err := lvldb.NewMem()
	if err != nil {
		fatal(fmt.Sprintf("open chain database: %v", err))
	}
	return db
}

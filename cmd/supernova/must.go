// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package main

import (
	"context"
	"crypto/tls"
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
	"github.com/meterio/meter-pov/chain"
	"github.com/meterio/meter-pov/cmd/supernova/probe"
	"github.com/meterio/meter-pov/co"
	"github.com/meterio/meter-pov/comm"
	"github.com/meterio/meter-pov/consensus"
	"github.com/meterio/meter-pov/genesis"
	"github.com/meterio/meter-pov/lvldb"
	"github.com/meterio/meter-pov/meter"
	"github.com/meterio/meter-pov/p2psrv"
	"github.com/meterio/meter-pov/txpool"
	"github.com/meterio/meter-pov/types"
	"github.com/prysmaticlabs/prysm/v5/crypto/bls"
	cli "gopkg.in/urfave/cli.v1"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	p2pMagic       [4]byte
	consensusMagic [4]byte
)

type Leveler struct {
	level slog.Level
}

func NewLeveler(level int) *Leveler {
	return &Leveler{level: slog.Level(level)}
}

func (l *Leveler) Level() slog.Level {
	return l.level
}

func initLogger(ctx *cli.Context) {
	logLevel := ctx.Int(verbosityFlag.Name)
	fmt.Println("logLevel: ", logLevel)
	fmt.Println("slog: ", slog.Level(logLevel))
	// set global logger with custom options
	w := os.Stderr

	// create a new logger
	// logger := slog.New(tint.NewHandler(w, nil))

	// set global logger with custom options
	slog.SetDefault(slog.New(
		tint.NewHandler(w, &tint.Options{
			Level:      slog.Level(logLevel),
			TimeFormat: time.DateTime,
		}),
	))
}

func selectGenesis(ctx *cli.Context) *genesis.Genesis {
	network := ctx.String(networkFlag.Name)
	switch network {
	case "warringstakes":
		fallthrough
	case "test":
		return genesis.NewTestnet()
	case "main":
		return genesis.NewMainnet()
	case "staging":
		return genesis.NewMainnet()
	default:
		cli.ShowAppHelp(ctx)
		if network == "" {
			fmt.Printf("network flag not specified: -%s\n", networkFlag.Name)
		} else {
			fmt.Printf("unrecognized value '%s' for flag -%s\n", network, networkFlag.Name)
		}
		os.Exit(1)
		return nil
	}
}

func printDelegates(delegates []*types.Delegate) {
	// fmt.Println("--------------------------------------------------")
	fmt.Printf("Delegates Initialized (size:%d)\n", len(delegates))
	// fmt.Println(------------------------------------------------")

	// for i, d := range delegates {
	// 	fmt.Printf("#%d: %s\n", i+1, d.String())
	// }
	// fmt.Println("--------------------------------------------------")
}

func makeDataDir(ctx *cli.Context) string {
	dataDir := ctx.String(dataDirFlag.Name)
	if dataDir == "" {
		fatal(fmt.Sprintf("unable to infer default data dir, use -%s to specify", dataDirFlag.Name))
	}
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		fatal(fmt.Sprintf("create data dir [%v]: %v", dataDir, err))
	}
	return dataDir
}

func makeInstanceDir(ctx *cli.Context, gene *genesis.Genesis) string {
	dataDir := makeDataDir(ctx)

	instanceDir := filepath.Join(dataDir, fmt.Sprintf("instance-%x", gene.ID().Bytes()[24:]))
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		fatal(fmt.Sprintf("create data dir [%v]: %v", instanceDir, err))
	}
	return instanceDir
}

func makeSnapshotDir(ctx *cli.Context) string {
	dataDir := makeDataDir(ctx)

	snapshotDir := filepath.Join(dataDir, "snapshot")
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		fatal(fmt.Sprintf("create data dir [%v]: %v", snapshotDir, err))
	}
	return snapshotDir
}

func openMainDB(ctx *cli.Context, dataDir string) *lvldb.LevelDB {
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

func initChain(gene *genesis.Genesis, mainDB *lvldb.LevelDB) *chain.Chain {
	genesisBlock, err := gene.Build()
	if err != nil {
		fatal("build genesis block: ", err)
	}

	chain, err := chain.New(mainDB, genesisBlock, true)
	if err != nil {
		fatal("initialize block chain:", err)
	}

	return chain
}

func masterKeyPath(ctx *cli.Context) string {
	return filepath.Join(ctx.String("data-dir"), "master.key")
}

func publicKeyPath(ctx *cli.Context) string {
	return filepath.Join(ctx.String("data-dir"), "public.key")
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

func loadNodeMaster(ctx *cli.Context) *types.BlsMaster {

	keyLoader := NewKeyLoader(ctx)
	blsMaster, err := keyLoader.Load()
	if err != nil {
		fatal("load key error: ", err)
	}
	return blsMaster
}

type p2pComm struct {
	comm           *comm.Communicator
	p2pSrv         *p2psrv.Server
	peersCachePath string
}

func newP2PComm(cliCtx *cli.Context, ctx context.Context, chain *chain.Chain, txPool *txpool.TxPool, instanceDir string, magic [4]byte) *p2pComm {
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
	} else {
		BootstrapNodes = bootstrapNodes
	}

	opts := &p2psrv.Options{
		Name:           meter.MakeName("meter", fullVersion()),
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
	slog.Info("P2P server started", "elapsed", meter.PrettyDuration(time.Since(start)))
	start = time.Now()
	p.comm.Start()
	slog.Info("communicator started", "elapsed", meter.PrettyDuration(time.Since(start)))
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

func pubkeyHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(fmt.Sprintf("version = %s", fullVersion())))
}

type Dispatcher struct {
	cons *consensus.Reactor
}

func handleVersion(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(fullVersion()))
}

func (d *Dispatcher) handlePeers(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	// api_utils.WriteJSON(w, d.nw.PeersStats())
}

func startObserveServer(cons *consensus.Reactor, blsPubKey bls.PublicKey, nw probe.Network, chain *chain.Chain) (string, func()) {
	addr := ":8670"
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		fatal(fmt.Sprintf("listen observe addr [%v]: %v", addr, err))
	}
	probe := &probe.Probe{cons, blsPubKey, chain, fullVersion(), nw}
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

func startAPIServer(ctx *cli.Context, handler http.Handler, genesisID meter.Bytes32) (string, func()) {
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
	handler = handleXMeterVersion(handler)
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
	dataDir := ctx.String(dataDirFlag.Name)
	httpsCertFile := filepath.Join(dataDir, ctx.String(httpsCertFlag.Name))
	httpsKeyFile := filepath.Join(dataDir, ctx.String(httpsKeyFlag.Name))
	if fileExists(httpsCertFile) && fileExists(httpsKeyFile) {
		cer, err := tls.LoadX509KeyPair(httpsCertFile, httpsKeyFile)
		if err != nil {
			panic(err)
		}

		tlsConfig := &tls.Config{Certificates: []tls.Certificate{cer}}
		tlsSrv = &http.Server{
			Handler:      handler,
			TLSConfig:    tlsConfig,
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 18 * time.Second,
			IdleTimeout:  120 * time.Second,
		}
		tlsListener, err := tls.Listen("tcp", ":8667", tlsConfig)
		if err != nil {
			panic(err)
		}
		goes.Go(func() {
			err := tlsSrv.Serve(tlsListener)
			if err != nil {
				if err != http.ErrServerClosed {
					fmt.Println("observe server stopped, error:", err)
				}
			}

		})
		returnStr = returnStr + " | https://" + tlsListener.Addr().String() + "/"
	} else {
		returnStr = returnStr + " | https service is disabled due to missing cert/key file"
	}
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
    PubKey       [ %v ]
    Instance dir    [ %v ]
    API portal      [ %v ]
`,
		meter.MakeName("Meter", fullVersion()),
		topic,
		hex.EncodeToString(p2pMagic[:]),
		gene.ID(), gene.Name(),
		bestBlock.ID(), bestBlock.Number(), time.Unix(int64(bestBlock.Timestamp()), 0),
		meter.GetForkConfig(gene.ID()),
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

// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package node

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	db "github.com/cometbft/cometbft-db"
	cmtcfg "github.com/cometbft/cometbft/config"
	cmtp2p "github.com/cometbft/cometbft/p2p"
	"github.com/ethereum/go-ethereum/common/fdlimit"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/nat"
	"github.com/lmittmann/tint"
	"github.com/meterio/supernova/chain"
	"github.com/meterio/supernova/consensus"
	"github.com/meterio/supernova/genesis"
	"github.com/meterio/supernova/libs/comm"
	"github.com/meterio/supernova/libs/lvldb"
	"github.com/meterio/supernova/p2psrv"
	"github.com/meterio/supernova/txpool"
	"github.com/meterio/supernova/types"
	cli "gopkg.in/urfave/cli.v1"
)

var (
	p2pMagic       [4]byte
	consensusMagic [4]byte
)

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
	fmt.Println("slog level: ", logLevel)
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

func InitChain(gene *genesis.Genesis, mainDB db.DB) *chain.Chain {
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

type p2pComm struct {
	comm           *comm.Communicator
	p2pSrv         *p2psrv.Server
	peersCachePath string // FIXME: init this value
}

func NewP2PComm(ctx context.Context, config *cmtcfg.Config, nodeKey *cmtp2p.NodeKey, chain *chain.Chain, txPool *txpool.TxPool, magic [4]byte) *p2pComm {
	var BootstrapNodes []*enode.Node

	// FIXME: init bootstrap nodes
	privkey, err := crypto.ToECDSA(nodeKey.PrivKey.Bytes())
	if err != nil {
		panic(err)
	}

	opts := &p2psrv.Options{
		Name:           types.MakeName(config.BaseConfig.Moniker, config.BaseConfig.Version),
		PrivateKey:     privkey,
		MaxPeers:       config.P2P.MaxNumInboundPeers,
		ListenAddr:     config.P2P.ListenAddress,
		BootstrapNodes: BootstrapNodes,
		NAT:            nat.Any(),
	}

	return &p2pComm{
		comm:   comm.New(ctx, chain, txPool, magic),
		p2pSrv: p2psrv.New(opts),
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

func openMemMainDB() *lvldb.LevelDB {
	db, err := lvldb.NewMem()
	if err != nil {
		fatal(fmt.Sprintf("open chain database: %v", err))
	}
	return db
}

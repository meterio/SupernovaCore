// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package node

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"sort"
	"time"

	"github.com/beevik/ntp"
	"github.com/cometbft/cometbft/proxy"
	cmtproxy "github.com/cometbft/cometbft/proxy"
	"github.com/ethereum/go-ethereum/common/mclock"
	"github.com/ethereum/go-ethereum/event"
	"github.com/meterio/supernova/api"
	"github.com/meterio/supernova/block"
	"github.com/meterio/supernova/chain"
	"github.com/meterio/supernova/cmd/supernova/probe"
	"github.com/meterio/supernova/consensus"
	"github.com/meterio/supernova/genesis"
	"github.com/meterio/supernova/libs/cache"
	"github.com/meterio/supernova/libs/co"
	"github.com/meterio/supernova/libs/comm"
	"github.com/meterio/supernova/libs/lvldb"
	"github.com/meterio/supernova/txpool"
	"github.com/meterio/supernova/types"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prysmaticlabs/prysm/v5/crypto/bls"
	"gopkg.in/urfave/cli.v1"
)

var (
	GlobNodeInst           *Node
	errCantExtendBestBlock = errors.New("can't extend best block")
)

type Node struct {
	goes    co.Goes
	ctx     context.Context
	reactor *consensus.Reactor

	chain       *chain.Chain
	txPool      *txpool.TxPool
	txStashPath string
	comm        *comm.Communicator
	logger      *slog.Logger

	proxyApp cmtproxy.AppConns
}

func New(
	ctx *cli.Context,
	version string,
	clientCreator cmtproxy.ClientCreator,
) *Node {
	exitSignal := HandleExitSignal()
	debug.SetMemoryLimit(5 * 1024 * 1024 * 1024) // 5GB

	fmt.Println("ensure dir: ", ctx.String("data-dir"))
	defer func() { slog.Info("exited") }()

	InitLogger(ctx)

	baseDir := ctx.String("data-dir")
	gene := genesis.LoadGenesis(baseDir)
	dirConfig := EnsureDirs(ctx, gene)

	slog.Info("Meter Start ...")
	mainDB := OpenMainDB(ctx, dirConfig.InstanceDir)
	defer func() { slog.Info("closing main database..."); mainDB.Close() }()

	chain := InitChain(gene, mainDB)

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
	sum := sha256.Sum256([]byte(fmt.Sprintf("%v %v", version, topic)))

	// Split magic to p2p_magic and consensus_magic
	copy(p2pMagic[:], sum[:4])
	copy(consensusMagic[:], sum[:4])

	txPool := txpool.New(chain, txpool.DefaultTxPoolOptions)
	defer func() { slog.Info("closing tx pool..."); txPool.Close() }()

	// Create the proxyApp and establish connections to the ABCI app (consensus, mempool, query).
	proxyApp, err := createAndStartProxyAppConns(clientCreator, cmtproxy.NopMetrics())
	if err != nil {
		panic(err)
	}

	p2pcom := NewP2PComm(ctx, exitSignal, chain, txPool, dirConfig.InstanceDir, p2pMagic)

	reactor := consensus.NewConsensusReactor(config, chain, p2pcom.comm, txPool, blsMaster, proxyApp)

	startObserveServer(reactor, version, blsMaster.GetPublicKey(), p2pcom.comm, chain)

	apiHandler, apiCloser := api.New(chain, txPool, p2pcom.comm, ctx.String(apiCorsFlag.Name), uint32(ctx.Int(apiBacktraceLimitFlag.Name)), p2pcom.p2pSrv)
	defer func() { slog.Info("closing API..."); apiCloser() }()

	apiURL, srvCloser := StartAPIServer(ctx, apiHandler, chain.GenesisBlock().ID())
	defer func() { slog.Info("stopping API server..."); srvCloser() }()

	printStartupMessage(topic, gene, chain, blsMaster, dirConfig.InstanceDir, apiURL)

	p2pcom.Start()
	defer p2pcom.Stop()

	node := &Node{
		ctx:      context.TODO(),
		reactor:  reactor,
		chain:    chain,
		txPool:   txPool,
		comm:     p2pcom.comm,
		logger:   slog.With("pkg", "node"),
		proxyApp: proxyApp,
	}

	return node
}

func startObserveServer(cons *consensus.Reactor, version string, blsPubKey bls.PublicKey, nw probe.Network, chain *chain.Chain) (string, func()) {
	addr := ":8670"
	listener, err := net.Listen("tcp", addr)
	if err != nil {
	}
	probe := &probe.Probe{cons, blsPubKey, chain, version, nw}
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

func createAndStartProxyAppConns(clientCreator cmtproxy.ClientCreator, metrics *cmtproxy.Metrics) (proxy.AppConns, error) {
	proxyApp := proxy.NewAppConns(clientCreator, metrics)
	if err := proxyApp.Start(); err != nil {
		return nil, fmt.Errorf("error starting proxy app connections: %v", err)
	}
	return proxyApp, nil
}

func (n *Node) Run() error {
	n.comm.Sync(n.handleBlockStream)

	n.goes.Go(func() { n.houseKeeping(n.ctx) })
	n.goes.Go(func() { n.txStashLoop(n.ctx) })

	n.goes.Go(func() { n.reactor.OnStart(n.ctx) })

	n.goes.Wait()
	return nil
}

func (n *Node) handleBlockStream(ctx context.Context, stream <-chan *block.EscortedBlock) (err error) {
	n.logger.Debug("start to process block stream")
	defer n.logger.Debug("process block stream done", "err", err)
	var stats blockStats
	startTime := mclock.Now()

	report := func(block *block.Block, pending int) {
		n.logger.Info(fmt.Sprintf("imported blocks (%v) ", stats.processed), stats.LogContext(block.Header(), pending)...)
		stats = blockStats{}
		startTime = mclock.Now()
	}

	var blk *block.EscortedBlock
	for blk = range stream {
		n.logger.Debug("handle block", "block", blk.Block.ID().ToBlockShortID())
		if isTrunk, err := n.processBlock(blk.Block, blk.EscortQC, &stats); err != nil {
			if err == errCantExtendBestBlock {
				best := n.chain.BestBlock()
				n.logger.Warn("process block failed", "num", blk.Block.Number(), "id", blk.Block.ID(), "best", best.Number(), "err", err.Error())
			} else {
				n.logger.Error("process block failed", "num", blk.Block.Number(), "id", blk.Block.ID(), "err", err.Error())
			}
			return err
		} else if isTrunk {
			// this processBlock happens after consensus SyncDone, need to broadcast
			if n.reactor.SyncDone {
				n.comm.BroadcastBlock(blk)
			}
		}

		if stats.processed > 0 &&
			mclock.Now()-startTime > mclock.AbsTime(time.Second*2) {
			report(blk.Block, len(stream))
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
	if blk != nil && stats.processed > 0 {
		report(blk.Block, len(stream))
	}
	return nil
}

func (n *Node) houseKeeping(ctx context.Context) {
	n.logger.Debug("enter house keeping")
	defer n.logger.Debug("leave house keeping")

	var scope event.SubscriptionScope
	defer scope.Close()

	newBlockCh := make(chan *comm.NewBlockEvent)
	scope.Track(n.comm.SubscribeBlock(newBlockCh))

	futureTicker := time.NewTicker(time.Duration(types.BlockInterval) * time.Second)
	defer futureTicker.Stop()

	connectivityTicker := time.NewTicker(time.Second)
	defer connectivityTicker.Stop()

	var noPeerTimes int

	futureBlocks := cache.NewRandCache(32)

	for {
		select {
		case <-ctx.Done():
			return
		case newBlock := <-newBlockCh:
			var stats blockStats
			if newBlock.Block.IsSBlock() {
				n.logger.Warn("got new sblock", "num", newBlock.Block.Number(), "id", newBlock.Block.ID().ToBlockShortID())
			} else {
				if isTrunk, err := n.processBlock(newBlock.Block, newBlock.EscortQC, &stats); err != nil {
					if consensus.IsFutureBlock(err) ||
						(consensus.IsParentMissing(err) && futureBlocks.Contains(newBlock.Block.Header().ParentID)) {
						n.logger.Debug("future block added", "id", newBlock.Block.ID())
						futureBlocks.Set(newBlock.Block.ID(), newBlock)
					}
				} else if isTrunk {
					n.comm.BroadcastBlock(newBlock.EscortedBlock)
					// n.logger.Info(fmt.Sprintf("imported blocks (%v)", stats.processed), stats.LogContext(newBlock.Block.Header())...)
				}
			}
		case <-futureTicker.C:
			// process future blocks
			var blocks []*block.EscortedBlock
			futureBlocks.ForEach(func(ent *cache.Entry) bool {
				blocks = append(blocks, ent.Value.(*block.EscortedBlock))
				return true
			})
			sort.Slice(blocks, func(i, j int) bool {
				return blocks[i].Block.Number() < blocks[j].Block.Number()
			})
			var stats blockStats
			for i, block := range blocks {
				if block.Block.IsSBlock() {
					n.logger.Warn("got future sblock", "num", block.Block.Number(), "id", block.Block.ID().ToBlockShortID())
					continue
				}
				if isTrunk, err := n.processBlock(block.Block, block.EscortQC, &stats); err == nil || consensus.IsKnownBlock(err) {
					n.logger.Debug("future block consumed", "id", block.Block.ID())
					futureBlocks.Remove(block.Block.ID())
					if isTrunk {
						n.comm.BroadcastBlock(block)
					}
				}

				if stats.processed > 0 && i == len(blocks)-1 {
					// n.logger.Info(fmt.Sprintf("imported blocks (%v)", stats.processed), stats.LogContext(block.Header())...)
				}
			}
		case <-connectivityTicker.C:
			if n.comm.PeerCount() == 0 {
				noPeerTimes++
				if noPeerTimes > 30 {
					noPeerTimes = 0
					go checkClockOffset()
				}
			} else {
				noPeerTimes = 0
			}
		}
	}
}

func (n *Node) txStashLoop(ctx context.Context) {
	n.logger.Debug("enter tx stash loop")
	defer n.logger.Debug("leave tx stash loop")

	db, err := lvldb.New(n.txStashPath, lvldb.Options{})
	if err != nil {
		n.logger.Error("create tx stash", "err", err)
		return
	}
	defer db.Close()

	stash := newTxStash(db, 1000)

	{
		txs := stash.LoadAll()
		bestBlock := n.chain.BestBlock()
		n.txPool.Fill(txs, func(txID []byte) bool {
			if _, err := n.chain.GetTransactionMeta(txID, bestBlock.ID()); err != nil {
				return false
			} else {
				return true
			}
		})
		n.logger.Debug("loaded txs from stash", "count", len(txs))
	}

	var scope event.SubscriptionScope
	defer scope.Close()

	txCh := make(chan *txpool.TxEvent)
	scope.Track(n.txPool.SubscribeTxEvent(txCh))
	for {
		select {
		case <-ctx.Done():
			return
		case txEv := <-txCh:
			// skip executables
			if txEv.Executable != nil && *txEv.Executable {
				continue
			}
			// only stash non-executable txs
			if err := stash.Save(txEv.Tx); err != nil {
				n.logger.Warn("stash tx", "id", txEv.Tx.Hash(), "err", err)
			} else {
				n.logger.Debug("stashed tx", "id", txEv.Tx.Hash())
			}
		}
	}
}

func (n *Node) processBlock(blk *block.Block, escortQC *block.QuorumCert, stats *blockStats) (bool, error) {
	now := uint64(time.Now().Unix())

	best := n.chain.BestBlock()
	if !bytes.Equal(best.ID().Bytes(), blk.ParentID().Bytes()) {
		return false, errCantExtendBestBlock
	}
	if blk.Timestamp()+types.BlockInterval > now {
		QCValid := n.reactor.ValidateQC(blk, escortQC)
		if !QCValid {
			return false, errors.New(fmt.Sprintf("invalid %s on Block %s", escortQC.String(), blk.ID().ToBlockShortID()))
		}
	}
	start := time.Now()
	err := n.reactor.ProcessSyncedBlock(blk, now)
	if time.Since(start) > time.Millisecond*500 {
		n.logger.Debug("slow processed block", "blk", blk.Number(), "elapsed", types.PrettyDuration(time.Since(start)))
	}

	if err != nil {
		switch {
		case consensus.IsKnownBlock(err):
			return false, nil
		case consensus.IsFutureBlock(err) || consensus.IsParentMissing(err):
			return false, nil
		case consensus.IsCritical(err):
			msg := fmt.Sprintf(`failed to process block due to consensus failure \n%v\n`, blk.Header())
			n.logger.Error(msg, "err", err)
		default:
			n.logger.Error("failed to process block", "err", err)
		}
		return false, err
	}

	stats.UpdateProcessed(1, len(blk.Txs))
	// FIXME: process fork
	// n.processFork(fork)

	// shortcut to refresh epoch
	updated, _ := n.reactor.UpdateCurEpoch()

	if blk.IsKBlock() && n.reactor.SyncDone && updated {
		n.logger.Info("synced a kblock, schedule regulate", "num", blk.Number(), "id", blk.ID())
		n.reactor.SchedulePacemakerRegulate()
	}
	// end of shortcut
	// return len(fork.Trunk) > 0, nil
	//  FIXME: help
	return true, nil
}

func (n *Node) commitBlock(newBlock *block.Block, escortQC *block.QuorumCert) (*chain.Fork, error) {
	start := time.Now()
	// fmt.Println("Calling AddBlock from node.commitBlock, newBlock=", newBlock.ID())
	fork, err := n.chain.AddBlock(newBlock, escortQC)
	if err != nil {
		return nil, err
	}

	// skip logdb access if no txs
	if len(newBlock.Transactions()) > 0 {
		forkIDs := make([]types.Bytes32, 0, len(fork.Branch))
		for _, header := range fork.Branch {
			forkIDs = append(forkIDs, header.ID())
		}

	}

	if n.reactor.SyncDone {
		n.logger.Info(fmt.Sprintf("* synced %v", newBlock.ShortID()), "txs", len(newBlock.Txs), "epoch", newBlock.GetBlockEpoch(), "elapsed", types.PrettyDuration(time.Since(start)))
	} else {
		if time.Since(start) > time.Millisecond*500 {
			n.logger.Info(fmt.Sprintf("* slow synced %v", newBlock.ShortID()), "txs", len(newBlock.Txs), "epoch", newBlock.GetBlockEpoch(), "elapsed", types.PrettyDuration(time.Since(start)))
		}
	}
	return fork, nil
}

func (n *Node) processFork(fork *chain.Fork) {
	if len(fork.Branch) >= 2 {
		trunkLen := len(fork.Trunk)
		branchLen := len(fork.Branch)
		n.logger.Warn(fmt.Sprintf(
			`⑂⑂⑂⑂⑂⑂⑂⑂ FORK HAPPENED ⑂⑂⑂⑂⑂⑂⑂⑂
ancestor: %v
trunk:    %v  %v
branch:   %v  %v`, fork.Ancestor,
			trunkLen, fork.Trunk[trunkLen-1],
			branchLen, fork.Branch[branchLen-1]))
	}
	for _, header := range fork.Branch {
		body, err := n.chain.GetBlockBody(header.ID())
		if err != nil {
			n.logger.Warn("failed to get block body", "err", err, "blockid", header.ID())
			continue
		}
		for _, tx := range body.Txs {
			if err := n.txPool.Add(tx); err != nil {
				n.logger.Debug("failed to add tx to tx pool", "err", err, "id", tx.Hash())
			}
		}
	}
}

func checkClockOffset() {
	resp, err := ntp.Query("ap.pool.ntp.org")
	if err != nil {
		slog.Debug("failed to access NTP", "err", err)
		return
	}
	if resp.ClockOffset > time.Duration(types.BlockInterval)*time.Second/2 {
		slog.Warn("clock offset detected", "offset", types.PrettyDuration(resp.ClockOffset))
	}
}

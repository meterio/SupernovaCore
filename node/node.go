// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package node

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"time"

	"github.com/beevik/ntp"
	cmtcfg "github.com/cometbft/cometbft/config"
	"github.com/cometbft/cometbft/libs/log"
	"github.com/cometbft/cometbft/privval"
	"github.com/cometbft/cometbft/proxy"
	cmttypes "github.com/cometbft/cometbft/types"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/meterio/supernova/libs/p2p"
	"github.com/meterio/supernova/libs/rpc"

	db "github.com/cometbft/cometbft-db"
	cmtnode "github.com/cometbft/cometbft/node"
	cmtproxy "github.com/cometbft/cometbft/proxy"
	"github.com/ethereum/go-ethereum/common/mclock"
	"github.com/ethereum/go-ethereum/event"
	"github.com/meterio/supernova/api"
	"github.com/meterio/supernova/block"
	"github.com/meterio/supernova/chain"
	"github.com/meterio/supernova/consensus"
	"github.com/meterio/supernova/libs/cache"
	"github.com/meterio/supernova/libs/co"
	"github.com/meterio/supernova/txpool"
	"github.com/meterio/supernova/types"
	"github.com/pkg/errors"
)

var (
	GlobNodeInst           *Node
	errCantExtendBestBlock = errors.New("can't extend best block")
	genesisDocHashKey      = []byte("genesisDocHash")
)

func LoadGenesisDoc(
	mainDB db.DB,
	genesisDocProvider cmtnode.GenesisDocProvider,
) (*cmttypes.GenesisDoc, error) { // originally, LoadStateFromDBOrGenesisDocProvider
	// Get genesis doc hash
	genDocHash, err := mainDB.Get(genesisDocHashKey)
	if err != nil {
		return nil, fmt.Errorf("error retrieving genesis doc hash: %w", err)
	}
	csGenDoc, err := genesisDocProvider()
	if err != nil {
		return nil, err
	}

	if err = csGenDoc.GenesisDoc.ValidateAndComplete(); err != nil {
		return nil, fmt.Errorf("error in genesis doc: %w", err)
	}

	if len(genDocHash) == 0 {
		// Save the genDoc hash in the store if it doesn't already exist for future verification
		if err = mainDB.SetSync(genesisDocHashKey, csGenDoc.Sha256Checksum); err != nil {
			return nil, fmt.Errorf("failed to save genesis doc hash to db: %w", err)
		}
	} else {
		if !bytes.Equal(genDocHash, csGenDoc.Sha256Checksum) {
			return nil, errors.New("genesis doc hash in db does not match loaded genesis doc")
		}
	}

	return csGenDoc.GenesisDoc, nil
}

type Node struct {
	goes          co.Goes
	config        *cmtcfg.Config
	ctx           context.Context
	genesisDoc    *cmttypes.GenesisDoc   // initial validator set
	privValidator cmttypes.PrivValidator // local node's validator key

	apiServer *api.APIServer

	// network
	nodeKey *types.NodeKey // our node privkey
	reactor *consensus.Reactor

	chain       *chain.Chain
	txPool      *txpool.TxPool
	txStashPath string
	rpc         *rpc.RPCServer
	logger      *slog.Logger

	mainDB db.DB
	p2pSrv p2p.P2P

	proxyApp cmtproxy.AppConns
}

func NewNode(
	ctx context.Context,
	config *cmtcfg.Config,
	privValidator *privval.FilePV,
	nodeKey *types.NodeKey,
	clientCreator cmtproxy.ClientCreator,
	genesisDocProvider cmtnode.GenesisDocProvider,
	dbProvider cmtcfg.DBProvider,
	metricProvider cmtnode.MetricsProvider,
	logger log.Logger,
) (*Node, error) {
	InitLogger(config)

	mainDB, err := dbProvider(&cmtcfg.DBContext{ID: "maindb", Config: config})

	genDoc, err := LoadGenesisDoc(mainDB, genesisDocProvider)
	if err != nil {
		return nil, err
	}

	chain := NewChain(mainDB)

	// if flattern index start is not set, or pruning is not complete
	// start the pruning routine right now

	privValidator.Key.PubKey.Bytes()
	blsMaster := types.NewBlsMasterWithCometKeys(privValidator.Key.PrivKey, privValidator.Key.PubKey)

	slog.Info("Supernova start ...", "version", config.BaseConfig.Version)

	// Create the proxyApp and establish connections to the ABCI app (consensus, mempool, query).
	proxyApp, err := createAndStartProxyAppConns(clientCreator, cmtproxy.NopMetrics())
	if err != nil {
		return nil, err
	}

	eventBus, err := createAndStartEventBus(logger)
	if err != nil {
		return nil, err
	}
	err = doHandshake(ctx, chain, genDoc, eventBus, proxyApp)
	if err != nil {
		logger.Error("Handshake failed", "err", err)
		return nil, err
	}

	txPool := txpool.New(chain, txpool.DefaultTxPoolOptions)
	defer func() { slog.Info("closing tx pool..."); txPool.Close() }()

	var BootstrapNodes []string
	// BootstrapNodes = append(BootstrapNodes, "enr:-MK4QGZ6np5N03sJeQPI1ep3L_13ckTJQ5TXcj81mk_UV3oeA-mMtcw7JViYP3cgSBmvxQV74MRTTfUNM5TUqr_D2BiGAZRynhEfh2F0dG5ldHOIAAAAAAAAAACEZXRoMpBLDKxQAQAAAAAiAQAAAAAAgmlkgnY0gmlwhKwfWYOJc2VjcDI1NmsxoQMkZ9waUAVNMFXOY3B5VlDTqLZHqb4MqKOFXSvh-k4dUohzeW5jbmV0cwCDdGNwgjLIg3VkcIIu4A") // nova-3
	BootstrapNodes = append(BootstrapNodes, "enr:-MK4QOQzYBuKsestT0uZsQ2L7dDgD6EfE81oLfFflzurOHq7B4pY5r-8kozd9PRpE0Z3I994DxXmRc7mC8v23ABysCmGAZRsaVWnh2F0dG5ldHOIAAAAAAAAAACEZXRoMpBLDKxQAQAAAAAiAQAAAAAAgmlkgnY0gmlwhKwfEoiJc2VjcDI1NmsxoQLZ9EQkKk4n9OCfErexZJ6m-auSEcBdVngrAgh1UlWMp4hzeW5jbmV0cwCDdGNwgjLIg3VkcIIu4A") // nova-2
	BootstrapNodes = append(BootstrapNodes, "enr:-MK4QLXP9wqWWwRhi4To_3TJ_8rEMYOwN1fZIPeHg7uH__O-K2jBnFYwRy7oFoLYfUYFyP7XlXn5Ibq3Ltqfuzl-VrqGAZRsacAUh2F0dG5ldHOIAAAAAAAAAACEZXRoMpBLDKxQAQAAAAAiAQAAAAAAgmlkgnY0gmlwhKwfHXiJc2VjcDI1NmsxoQO4P_0L80DH2OIc3pd9GfjqevVK0tV2Z9NZqZ6_qAxSMYhzeW5jbmV0cwCDdGNwgjLIg3VkcIIu4A") // nova-1

	// BootstrapNodes = append(BootstrapNodes, "enr:-MK4QMWkLjGkpPB2iP84pdrBqyB-SjJiodPu0oLYQLVLXhgmPMeqN8Nk24Al9mElveJXJFaZUkjwWHAsz1oJN_A-hYeGAZSlsvkzh2F0dG5ldHOIAAAAAAAAAACEZXRoMpAWc8IXAQAAAAAiAQAAAAAAgmlkgnY0gmlwhKwfEoiJc2VjcDI1NmsxoQMog1olklG4kSkaiGepYTRoy0OseZus8-cOKqzsOqlkBIhzeW5jbmV0cwCDdGNwgjLIg3VkcIIu4A") // simd nova2
	// BootstrapNodes = append(BootstrapNodes, "enr:-MK4QGD2XTHBtQ_r17bA3MHvUqrhVfKvKKqIeDN3sD-YVhkSM2j6oiv2fKHTK_5lvCn6OPa-WHZ3m9Ao1C6oz9P6i9KGAZSlsvidh2F0dG5ldHOIAAAAAAAAAACEZXRoMpAWc8IXAQAAAAAiAQAAAAAAgmlkgnY0gmlwhKwfHXiJc2VjcDI1NmsxoQOucvYee5KxdMkhPqF4W8KGJGSuOhqzk59ZJiPyogCn6ohzeW5jbmV0cwCDdGNwgjLIg3VkcIIu4A") // simd nova1

	geneBlock, err := chain.GetTrunkBlock(0)
	if err != nil {
		return nil, err
	}
	p2pSrv := newP2PService(ctx, config, BootstrapNodes, geneBlock.NextValidatorsHash())
	reactor := consensus.NewConsensusReactor(ctx, config, chain, p2pSrv, txPool, blsMaster, proxyApp)

	// p2pSrv.SetStreamHandler(p2p.RPCProtocolPrefix+"/ssz_snappy", func(s network.Stream) {
	// 	fmt.Println("!!!!!!!!! received: /block/sync")
	// 	env := &message.RPCEnvelope{}
	// 	bs := make([]byte, 120000)
	// 	n, err := s.Read(bs)
	// 	if err != nil {
	// 		fmt.Println("Error reading")
	// 	}
	// 	err = env.UnmarshalSSZ(bs[:n])
	// 	if err != nil {
	// 		fmt.Println("Unmarshal error")
	// 	}
	// 	fmt.Println("env.MsgType", env.Enum)
	// 	fmt.Println("env.Raw", hex.EncodeToString(env.Raw))
	// 	newBlk := &block.Block{}
	// 	err = rlp.DecodeBytes(env.Raw, newBlk)
	// 	if err != nil {
	// 		fmt.Println("rlp decode error")
	// 	}
	// 	fmt.Println("decoded block: ", newBlk.Number(), newBlk.ID())

	// })

	pubkey, err := privValidator.GetPubKey()

	apiAddr := ":8670"
	chainId, err := strconv.ParseUint(genDoc.ChainID, 10, 64)
	apiServer := api.NewAPIServer(apiAddr, chainId, config.BaseConfig.Version, chain, txPool, reactor, pubkey.Bytes(), p2pSrv)

	bestBlock := chain.BestBlock()

	fmt.Printf(`Starting %v
    Network         [ %v ]    
    Best block      [ %v #%v @%v ]
    Forks           [ %v ]
    PubKey          [ %v ]
    API portal      [ %v ]
`,
		types.MakeName("Supernova", config.BaseConfig.Version),
		geneBlock.ID(),
		bestBlock.ID(), bestBlock.Number(), time.Unix(int64(bestBlock.Timestamp()), 0),
		types.GetForkConfig(geneBlock.ID()),
		hex.EncodeToString(pubkey.Bytes()),
		apiAddr)

	node := &Node{
		ctx:           ctx,
		config:        config,
		genesisDoc:    genDoc,
		rpc:           rpc.NewRPCServer(p2pSrv, chain, txPool),
		privValidator: privValidator,
		nodeKey:       nodeKey,
		apiServer:     apiServer,
		reactor:       reactor,
		chain:         chain,
		txPool:        txPool,
		p2pSrv:        p2pSrv,
		logger:        slog.With("pkg", "node"),
		proxyApp:      proxyApp,
	}

	return node, nil
}

func createAndStartProxyAppConns(clientCreator cmtproxy.ClientCreator, metrics *cmtproxy.Metrics) (proxy.AppConns, error) {
	proxyApp := proxy.NewAppConns(clientCreator, metrics)
	if err := proxyApp.Start(); err != nil {
		return nil, fmt.Errorf("error starting proxy app connections: %v", err)
	}
	return proxyApp, nil
}

func newP2PService(ctx context.Context, config *cmtcfg.Config, bootstrapNodes []string, geneValidatorSetHash []byte) p2p.P2P {
	svc, err := p2p.NewService(ctx, int64(time.Now().Unix()), geneValidatorSetHash, &p2p.Config{
		NoDiscovery: false,
		// StaticPeers:          slice.SplitCommaSeparated(cliCtx.StringSlice(cmd.StaticPeers.Name)),
		Discv5BootStrapAddrs: p2p.ParseBootStrapAddrs(bootstrapNodes),
		// RelayNodeAddr:        cliCtx.String(cmd.RelayNode.Name),
		DataDir: config.RootDir,
		// LocalIP:              cliCtx.String(cmd.P2PIP.Name),
		// HostAddress: config.P2P.ExternalAddress,

		// HostDNS:      cliCtx.String(cmd.P2PHostDNS.Name),
		PrivateKey:   "",
		StaticPeerID: true,
		// MetaDataDir:          cliCtx.String(cmd.P2PMetadata.Name),
		QUICPort:  13000,
		TCPPort:   13000,
		UDPPort:   12000,
		MaxPeers:  uint(config.P2P.MaxNumInboundPeers),
		QueueSize: 1000,
		// AllowListCIDR:        cliCtx.String(cmd.P2PAllowList.Name),
		// DenyListCIDR:         slice.SplitCommaSeparated(cliCtx.StringSlice(cmd.P2PDenyList.Name)),
		// EnableUPnP:           cliCtx.Bool(cmd.EnableUPnPFlag.Name),
		// StateNotifier: n,
		// DB:            n.mainDB,
	})
	if err != nil {
		return nil
	}
	pubsub.WithSubscriptionFilter(pubsub.NewAllowlistSubscriptionFilter(p2p.ConsensusTopic))
	p := svc.PubSub()

	for _, s := range p.GetTopics() {
		fmt.Println("Valid topic: ", s)
	}
	p.RegisterTopicValidator(p2p.ConsensusTopic, customTopicValidator)

	go svc.Start()
	return svc
}
func customTopicValidator(ctx context.Context, peerID peer.ID, msg *pubsub.Message) (pubsub.ValidationResult, error) {
	// Perform custom validation
	// Example: Check message size, content, etc.
	fmt.Println("Validating consensus topic message", msg.ID)
	if len(msg.Data) > 0 { // Simple validation for non-empty messages
		return pubsub.ValidationAccept, nil
	}
	return pubsub.ValidationReject, errors.New("rejected")
}

func doHandshake(
	ctx context.Context,
	c *chain.Chain,
	genDoc *cmttypes.GenesisDoc,
	eventBus cmttypes.BlockEventPublisher,
	proxyApp proxy.AppConns,
) error {
	handshaker := consensus.NewHandshaker(c, genDoc)
	handshaker.SetEventBus(eventBus)
	if err := handshaker.Handshake(ctx, proxyApp); err != nil {
		return fmt.Errorf("error during handshake: %v", err)
	}
	return nil
}

func createAndStartEventBus(logger log.Logger) (*cmttypes.EventBus, error) {
	eventBus := cmttypes.NewEventBus()
	eventBus.SetLogger(logger.With("module", "events"))
	if err := eventBus.Start(); err != nil {
		return nil, err
	}
	return eventBus, nil
}

func (n *Node) Start() error {
	n.logger.Info("Node Start")
	n.rpc.Start(n.ctx)
	n.rpc.Sync(n.handleBlockStream)

	n.goes.Go(func() { n.apiServer.Start(n.ctx) })
	n.goes.Go(func() { n.houseKeeping(n.ctx) })
	// n.goes.Go(func() { n.txStashLoop(n.ctx) })
	n.goes.Go(func() { n.reactor.Start(n.ctx) })

	n.goes.Wait()
	return nil
}

func (n *Node) Stop() error {
	n.rpc.Stop()
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
		n.logger.Debug("process block", "block", blk.Block.ID().ToBlockShortID())
		if err := n.processBlock(blk.Block, blk.EscortQC, &stats); err != nil {
			if err == errCantExtendBestBlock {
				best := n.chain.BestBlock()
				n.logger.Warn("process block failed", "num", blk.Block.Number(), "id", blk.Block.ID(), "best", best.Number(), "err", err.Error())
			} else {
				n.logger.Error("process block failed", "num", blk.Block.Number(), "id", blk.Block.ID(), "err", err.Error())
			}
			return err
		} else {
			// this processBlock happens after consensus SyncDone, need to broadcast
			// if n.comm.Synced {
			// FIXME: skip broadcast blocks only if synced
			// n.comm.BroadcastBlock(blk)
			// }
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

	newBlockCh := make(chan *rpc.NewBlockEvent)
	// scope.Track(n.comm.SubscribeBlock(newBlockCh))

	futureTicker := time.NewTicker(time.Duration(types.BlockInterval) * time.Second)
	defer futureTicker.Stop()

	connectivityTicker := time.NewTicker(time.Second)
	defer connectivityTicker.Stop()

	futureBlocks := cache.NewRandCache(32)

	for {
		select {
		case <-ctx.Done():
			return
		case newBlock := <-newBlockCh:
			var stats blockStats

			if err := n.processBlock(newBlock.Block, newBlock.EscortQC, &stats); err != nil {
				if consensus.IsFutureBlock(err) ||
					(consensus.IsParentMissing(err) && futureBlocks.Contains(newBlock.Block.Header().ParentID)) {
					n.logger.Debug("future block added", "id", newBlock.Block.ID())
					futureBlocks.Set(newBlock.Block.ID(), newBlock)
				}
			} else {
				// TODO: maybe I should not broadcast future blocks？
				// n.comm.BroadcastBlock(newBlock.EscortedBlock)
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
				if err := n.processBlock(block.Block, block.EscortQC, &stats); err == nil || consensus.IsKnownBlock(err) {
					n.logger.Debug("future block consumed", "id", block.Block.ID())
					futureBlocks.Remove(block.Block.ID())
					// n.comm.BroadcastBlock(block)
				}

				if stats.processed > 0 && i == len(blocks)-1 {
					// n.logger.Info(fmt.Sprintf("imported blocks (%v)", stats.processed), stats.LogContext(block.Header())...)
				}
			}
		case <-connectivityTicker.C:
			// if n.comm.PeerCount() == 0 {
			// 	noPeerTimes++
			// 	if noPeerTimes > 30 {
			// 		noPeerTimes = 0
			// 		go checkClockOffset()
			// 	}
			// } else {
			// 	noPeerTimes = 0
			// }
		}
	}
}

// func (n *Node) txStashLoop(ctx context.Context) {
// 	n.logger.Debug("enter tx stash loop")
// 	defer n.logger.Debug("leave tx stash loop")

// 	db, err := lvldb.New(n.txStashPath, lvldb.Options{})
// 	if err != nil {
// 		n.logger.Error("create tx stash", "err", err)
// 		return
// 	}
// 	defer db.Close()

// 	stash := newTxStash(db, 1000)

// 	{
// 		txs := stash.LoadAll()
// 		bestBlock := n.chain.BestBlock()
// 		n.txPool.Fill(txs, func(txID []byte) bool {
// 			if _, err := n.chain.GetTransactionMeta(txID, bestBlock.ID()); err != nil {
// 				return false
// 			} else {
// 				return true
// 			}
// 		})
// 		n.logger.Debug("loaded txs from stash", "count", len(txs))
// 	}

// 	var scope event.SubscriptionScope
// 	defer scope.Close()

// 	txCh := make(chan *txpool.TxEvent)
// 	scope.Track(n.txPool.SubscribeTxEvent(txCh))
// 	for {
// 		select {
// 		case <-ctx.Done():
// 			return
// 		case txEv := <-txCh:
// 			// skip executables
// 			if txEv.Executable != nil && *txEv.Executable {
// 				continue
// 			}
// 			// only stash non-executable txs
// 			if err := stash.Save(txEv.Tx); err != nil {
// 				n.logger.Warn("stash tx", "id", txEv.Tx.Hash(), "err", err)
// 			} else {
// 				n.logger.Debug("stashed tx", "id", txEv.Tx.Hash())
// 			}
// 		}
// 	}
// }

func (n *Node) processBlock(blk *block.Block, escortQC *block.QuorumCert, stats *blockStats) error {
	now := uint64(time.Now().Unix())

	best := n.chain.BestBlock()
	if !bytes.Equal(best.ID().Bytes(), blk.ParentID().Bytes()) {
		return errCantExtendBestBlock
	}
	if blk.Timestamp()+types.BlockInterval > now {
		QCValid := n.reactor.Pacemaker.ValidateQC(blk, escortQC)
		if !QCValid {
			return errors.New(fmt.Sprintf("invalid %s on Block %s", escortQC.String(), blk.ID().ToBlockShortID()))
		}
	}
	start := time.Now()
	err := n.reactor.ValidateSyncedBlock(blk, now)
	if time.Since(start) > time.Millisecond*500 {
		n.logger.Debug("slow processed block", "blk", blk.Number(), "elapsed", types.PrettyDuration(time.Since(start)))
	}

	if err != nil {
		switch {
		case consensus.IsKnownBlock(err):
			return nil
		case consensus.IsFutureBlock(err) || consensus.IsParentMissing(err):
			return nil
		case consensus.IsCritical(err):
			msg := fmt.Sprintf(`failed to process block due to consensus failure \n%v\n`, blk.Header())
			n.logger.Error(msg, "err", err)
		default:
			n.logger.Error("failed to process block", "err", err)
		}
		return err
	}
	start = time.Now()
	err = n.reactor.Pacemaker.CommitBlock(blk, escortQC)
	if err != nil {
		if !n.chain.IsBlockExist(err) {
			n.logger.Error("failed to commit block", "err", err)
		}
		return err
	}

	stats.UpdateProcessed(1, len(blk.Txs))
	// n.processFork(fork)

	return nil
}

// func (n *Node) processFork(fork *chain.Fork) {
// 	if len(fork.Branch) >= 2 {
// 		trunkLen := len(fork.Trunk)
// 		branchLen := len(fork.Branch)
// 		n.logger.Warn(fmt.Sprintf(
// 			`⑂⑂⑂⑂⑂⑂⑂⑂ FORK HAPPENED ⑂⑂⑂⑂⑂⑂⑂⑂
// ancestor: %v
// trunk:    %v  %v
// branch:   %v  %v`, fork.Ancestor,
// 			trunkLen, fork.Trunk[trunkLen-1],
// 			branchLen, fork.Branch[branchLen-1]))
// 	}
// 	for _, header := range fork.Branch {
// 		body, err := n.chain.GetBlockBody(header.ID())
// 		if err != nil {
// 			n.logger.Warn("failed to get block body", "err", err, "blockid", header.ID())
// 			continue
// 		}
// 		for _, tx := range body.Txs {
// 			if err := n.txPool.Add(tx); err != nil {
// 				n.logger.Debug("failed to add tx to tx pool", "err", err, "id", tx.Hash())
// 			}
// 		}
// 	}
// }

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

func (n *Node) IsRunning() bool {
	// FIXME: set correct value
	return true
}

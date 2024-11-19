// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package comm

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/meterio/supernova/block"
	"github.com/meterio/supernova/chain"
	"github.com/meterio/supernova/libs/co"
	"github.com/meterio/supernova/libs/comm/proto"
	"github.com/meterio/supernova/txpool"
	"github.com/meterio/supernova/types"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	peersCountGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "peers_count",
		Help: "Count of connected peers",
	})
)

// Communicator communicates with remote p2p peers to exchange blocks and txs, etc.
type Communicator struct {
	p2pSrv *p2p.Server
	chain  *chain.Chain
	txPool *txpool.TxPool
	ctx    context.Context
	// cancel         context.CancelFunc
	peerSet        *PeerSet
	syncedCh       chan struct{}
	newBlockFeed   event.Feed
	announcementCh chan *announcement
	feedScope      event.SubscriptionScope
	goes           co.Goes
	onceSynced     sync.Once
	peersCachePath string
	Synced         bool

	magic  [4]byte
	logger *slog.Logger
}

// New create a new Communicator instance.
func NewCommunicator(ctx context.Context, chain *chain.Chain, txPool *txpool.TxPool, magic [4]byte, p2pConfig p2p.Config, rootDir string) *Communicator {

	c := &Communicator{
		chain:  chain,
		txPool: txPool,
		ctx:    ctx,
		// cancel:         cancel,
		peerSet:        newPeerSet(),
		syncedCh:       make(chan struct{}),
		announcementCh: make(chan *announcement),
		magic:          magic,
		logger:         slog.With("pkg", "comm"),
		peersCachePath: filepath.Join("peers.cache", rootDir),
	}
	p2pConfig.Protocols = []p2p.Protocol{
		p2p.Protocol{
			Name:    proto.Name,
			Version: proto.Version,
			Length:  proto.Length,
			Run:     c.servePeer,
		}}
	c.p2pSrv = &p2p.Server{Config: p2pConfig}
	return c
}

// Synced returns a channel indicates if synchronization process passed.
func (c *Communicator) SyncedCh() <-chan struct{} {
	return c.syncedCh
}

func (c *Communicator) GetPeers() []*p2p.Peer {
	return c.p2pSrv.Peers()
}

// Sync start synchronization process.
func (c *Communicator) Sync(handler HandleBlockStream) {
	const initSyncInterval = 500 * time.Millisecond
	const syncInterval = 6 * time.Second

	c.goes.Go(func() {
		timer := time.NewTimer(0)
		defer timer.Stop()
		delay := initSyncInterval
		syncCount := 0

		shouldSynced := func() bool {
			bestBlockTime := c.chain.BestBlock().Timestamp()
			now := uint64(time.Now().Unix())
			if bestBlockTime+types.BlockInterval >= now && c.chain.BestQC().BlockID == c.chain.BestBlock().ID() {
				return true
			}
			if syncCount > 2 {
				return true
			}
			return false
		}

		for {
			timer.Stop()
			timer = time.NewTimer(delay)
			select {
			case <-c.ctx.Done():
				c.logger.Warn("stop communicator due to context end")
				return
			case <-timer.C:
				c.logger.Debug("synchronization start")

				best := c.chain.BestBlock().Header()
				// choose peer which has the head block with higher total score
				peer := c.peerSet.Slice().Find(func(peer *Peer) bool {
					_, num := peer.Head()
					c.logger.Debug("compare score from peer", "myScore", best.Number(), "peerScore", num, "peer", peer.Node().IP())
					return num >= best.Number()
				})
				if peer == nil {
					// original setting was 3, changed to 1 for cold start
					if c.peerSet.Len() < 1 {
						c.logger.Debug("no suitable peer to sync")
						break
					}
					// if more than 3 peers connected, we are assumed to be the best
					c.logger.Debug("synchronization done, best assumed")
				} else {
					if err := c.sync(peer, best.Number(), handler); err != nil {
						peer.logger.Debug("synchronization failed", "err", err)
						break
					}
					peer.logger.Debug("synchronization done")
				}
				syncCount++

				if shouldSynced() {
					delay = syncInterval
					c.onceSynced.Do(func() {
						c.Synced = true
						close(c.syncedCh)
					})
				}
			}
		}
	})
}

// Start the communicator.
func (c *Communicator) Start() {
	start := time.Now()
	if err := c.p2pSrv.Start(); err != nil {
		panic(err)
	}
	id := c.p2pSrv.Self().ID()
	// pubkey := c.p2pSrv.Self().Pubkey()
	c.logger.Info("P2P server started", "enodeID", id, "self", c.p2pSrv.Self(), "elapsed", types.PrettyDuration(time.Since(start)))
	start = time.Now()
	// c.goes.Go(c.txsLoop)
	c.goes.Go(c.announcementLoop)
	c.logger.Info("Communicator started", "elapsed", types.PrettyDuration(time.Since(start)))
	//c.goes.Go(c.powsLoop)
}

// Stop stop the communicator.
func (c *Communicator) Stop() {

	c.logger.Info("stopping P2P server...")
	c.p2pSrv.Stop()

	nodes := c.p2pSrv.Peers()
	c.logger.Info("saving peers cache...", "#peers", len(nodes))
	strs := make([]string, 0)
	for _, n := range nodes {
		strs = append(strs, n.String())
	}
	data := strings.Join(strs, "\n")
	if err := os.WriteFile(c.peersCachePath, []byte(data), 0600); err != nil {
		c.logger.Warn("failed to write peers cache", "err", err)
	}
	c.feedScope.Close()
	c.goes.Wait()
}

type txsToSync struct {
	txs    types.Transactions
	synced bool
}

func (c *Communicator) servePeer(p *p2p.Peer, rw p2p.MsgReadWriter) error {
	c.logger.Info("Serve Peer", "p", p.Node().URLv4())
	peer, dir := newPeer(p, rw, c.magic)
	curIP := peer.RemoteAddr().String()
	lastIndex := strings.LastIndex(curIP, ":")
	if lastIndex >= 0 {
		curIP = curIP[:lastIndex]
	}
	for _, knownPeer := range c.peerSet.Slice() {
		knownIP := knownPeer.RemoteAddr().String()
		lastIndex = strings.LastIndex(knownIP, ":")
		if lastIndex >= 0 {
			knownIP = knownIP[:lastIndex]
		}
		if knownIP == curIP {
			return errors.New("duplicate IP address: " + curIP)
		}
	}
	counter := c.peerSet.DirectionCount()
	if counter.Outbound*4 < counter.Inbound && dir == "inbound" {
		return errors.New("too much inbound from: " + curIP)
	}
	c.goes.Go(func() {
		c.runPeer(peer, dir)
	})

	var txsToSync txsToSync

	return peer.Serve(func(msg *p2p.Msg, w func(interface{})) error {
		return c.handleRPC(peer, msg, w, &txsToSync)
	}, proto.MaxMsgSize)
}

func (c *Communicator) runPeer(peer *Peer, dir string) {
	defer peer.Disconnect(p2p.DiscRequested)

	c.logger.Info("Run peer", "p", peer.Node().URLv4())
	// 5sec timeout for handshake
	ctx, cancel := context.WithTimeout(c.ctx, time.Second*5)
	defer cancel()

	status, err := proto.GetStatus(ctx, peer)
	if err != nil {
		peer.logger.Error("Failed to get status", "err", err.Error())
		return
	}
	if status.GenesisBlockID != c.chain.GenesisBlock().ID() {
		peer.logger.Error("Failed to handshake", "err", "genesis id mismatch")
		return
	}
	localClock := uint64(time.Now().Unix())
	remoteClock := status.SysTimestamp

	diff := localClock - remoteClock
	if localClock < remoteClock {
		diff = remoteClock - localClock
	}
	if diff > types.BlockInterval*2 {
		peer.logger.Error("Failed to handshake", "err", "sys time diff too large")
		return
	}

	peer.UpdateHead(status.BestBlockID, status.BestBlockNum)
	c.peerSet.Add(peer, dir)
	peersCountGauge.Set(float64(c.peerSet.Len()))
	peer.logger.Info(fmt.Sprintf("peer added (%v)", c.peerSet.Len()))

	defer func() {
		c.peerSet.Remove(peer.ID())
		peersCountGauge.Set(float64(c.peerSet.Len()))
		peer.logger.Info(fmt.Sprintf("peer removed (%v)", c.peerSet.Len()))
	}()

	select {
	case <-peer.Done():
	case <-c.ctx.Done():
	case <-c.syncedCh:
		c.syncTxs(peer)
		select {
		case <-peer.Done():
		case <-c.ctx.Done():
		}
	}
}

// SubscribeBlock subscribe the event that new block received.
func (c *Communicator) SubscribeBlock(ch chan *NewBlockEvent) event.Subscription {
	return c.feedScope.Track(c.newBlockFeed.Subscribe(ch))
}

// BroadcastBlock broadcast a block to remote peers.
func (c *Communicator) BroadcastBlock(blk *block.EscortedBlock) {
	h := blk.Block.Header()
	qc := blk.EscortQC
	c.logger.Debug("Broadcast escorted block",
		"num", h.Number(),
		"id", h.ID(),
		"lastKblock", h.LastKBlock,
		"escortQC", qc.String())

	peers := c.peerSet.Slice().Filter(func(p *Peer) bool {
		return !p.IsBlockKnown(blk.Block.ID())
	})

	p := int(math.Sqrt(float64(len(peers))))
	toPropagate := peers[:p]
	toAnnounce := peers[p:]

	for _, peer := range toPropagate {
		peer := peer
		peer.MarkBlock(blk.Block.ID())
		c.goes.Go(func() {
			c.logger.Debug(fmt.Sprintf("propagate %s to %s", blk.Block.CompactString(), types.Addr2IP(peer.RemoteAddr())))
			if err := proto.NotifyNewBlock(c.ctx, peer, blk); err != nil {
				peer.logger.Error(fmt.Sprintf("Failed to propagate %s", blk.Block.CompactString()), "err", err)
			}
		})
	}

	for _, peer := range toAnnounce {
		peer := peer
		peer.MarkBlock(blk.Block.ID())
		c.goes.Go(func() {
			c.logger.Debug(fmt.Sprintf("announce %s to %s", blk.Block.CompactString(), types.Addr2IP(peer.RemoteAddr())))
			if err := proto.NotifyNewBlockID(c.ctx, peer, blk.Block.ID()); err != nil {
				peer.logger.Error(fmt.Sprintf("Failed to announce %s", blk.Block.CompactString()), "err", err)
			}
		})
	}
}

// PeerCount returns count of peers.
func (c *Communicator) PeerCount() int {
	return c.peerSet.Len()
}

// PeersStats returns all peers' stats
func (c *Communicator) PeersStats() []*PeerStats {
	var stats []*PeerStats
	for _, peer := range c.peerSet.Slice() {
		bestID, bestNum := peer.Head()
		stats = append(stats, &PeerStats{
			Name:         peer.Name(),
			BestBlockID:  bestID,
			BestBlockNum: bestNum,
			PeerID:       peer.ID().String(),
			NetAddr:      peer.RemoteAddr().String(),
			Inbound:      peer.Inbound(),
			Duration:     uint64(time.Duration(peer.Duration()) / time.Second),
		})
	}
	sort.Slice(stats, func(i, j int) bool {
		return stats[i].Duration < stats[j].Duration
	})
	return stats
}

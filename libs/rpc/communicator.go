// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package rpc

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/meterio/supernova/block"
	"github.com/meterio/supernova/chain"
	"github.com/meterio/supernova/libs/co"
	"github.com/meterio/supernova/libs/p2p"
	"github.com/meterio/supernova/libs/p2p/peers"
	"github.com/meterio/supernova/libs/pb"
	"github.com/meterio/supernova/txpool"
	"github.com/meterio/supernova/types"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"storj.io/drpc/drpcconn"
)

var (
	peersCountGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "peers_count",
		Help: "Count of connected peers",
	})
)

// Communicator communicates with remote p2p peers to exchange blocks and txs, etc.
type Communicator struct {
	p2pSrv *p2p.Service
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
	// peersCachePath string
	Synced bool

	logger *slog.Logger
}

// New create a new Communicator instance.
func NewCommunicator(ctx context.Context, chain *chain.Chain, txPool *txpool.TxPool, p2pSrv *p2p.Service) *Communicator {
	c := &Communicator{
		chain:  chain,
		txPool: txPool,
		ctx:    context.Background(),
		// cancel:         cancel,
		peerSet:        newPeerSet(),
		syncedCh:       make(chan struct{}),
		announcementCh: make(chan *announcement),
		logger:         slog.With("pkg", "comm"),
		// peersCachePath: filepath.Join("peers.cache", rootDir),
		p2pSrv: p2pSrv,
	}

	return c
}

// type Notifiee interface {
// 	Listen(network.Network, ma.Multiaddr)       // called when network starts listening on an addr
// 	ListenClose(network.Network, ma.Multiaddr)  // called when network stops listening on an addr
// 	Connected(network.Network, network.Conn)    // called when a connection opened
// 	Disconnected(network.Network, network.Conn) // called when a connection closed
// }

func (c *Communicator) Listen(net network.Network, addr ma.Multiaddr) {
	c.logger.Info("P2P Listen on ", "addr", addr.String())
}

func (c *Communicator) ListenClose(net network.Network, addr ma.Multiaddr) {
	c.logger.Info("P2P Listen close on ", "addr", addr.String())
}

func (c *Communicator) Connected(net network.Network, conn network.Conn) {
	dir := "unknown"
	if conn.Stat().Direction == network.DirOutbound {
		dir = "outbound"
	}
	if conn.Stat().Direction == network.DirInbound {
		dir = "inbound"
	}

	c.logger.Info("Peer connected", "peer", conn.RemotePeer(), "dir", conn.Stat().Direction)
	c.servePeer(conn.RemotePeer(), dir)
}

func (c *Communicator) Disconnected(network network.Network, conn network.Conn) {
	c.peerSet.Remove(conn.RemotePeer())
	c.logger.Info("Peer disconnected", "peer", conn.RemotePeer())
}

func (c *Communicator) GetRPCClient(peerID peer.ID) pb.DRPCSyncClient {
	stream, err := c.p2pSrv.Host().NewStream(c.ctx, peerID, "sync")
	if err != nil {
		fmt.Println("can't establish stream")
	}
	conn := drpcconn.New(stream)
	client := pb.NewDRPCSyncClient(conn)

	return client
}

func (c *Communicator) Start() {
	// Set a stream handler for the RPC protocol
	c.goes.Go(c.announcementLoop)
}

// Synced returns a channel indicates if synchronization process passed.
func (c *Communicator) SyncedCh() <-chan struct{} {
	return c.syncedCh
}

func (c *Communicator) GetPeers() *peers.Status {
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
			bestBlockNano := c.chain.BestBlock().NanoTimestamp()
			nowNano := uint64(time.Now().UnixNano())
			if bestBlockNano+types.BlockIntervalNano >= nowNano && c.chain.BestQC().BlockID == c.chain.BestBlock().ID() {
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
					c.logger.Debug("compare score from peer", "myScore", best.Number(), "peerScore", num, "peer", peer.ID())
					return num >= best.Number()
				})
				if peer == nil {
					c.logger.Warn("no suitable peer")
					// original setting was 3, changed to 1 for cold start
					if c.peerSet.Len() < 1 {
						c.logger.Debug("no suitable peer to sync")
						break
					}
					// if more than 3 peers connected, we are assumed to be the best
					c.logger.Debug("synchronization done, best assumed")
				} else {
					c.logger.Info("sync from ", peer)
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

func (c *Communicator) servePeer(peerID peer.ID, dir string) error {
	c.logger.Info("Serve Peer", "peerID", peerID)
	peer := newPeer(peerID)
	knownPeer := c.peerSet.Find(peerID)
	if knownPeer != nil {
		return errors.New("duplicate PeerID" + peerID.String())
	}

	counter := c.peerSet.DirectionCount()
	if counter.Outbound*4 < counter.Inbound && dir == "inbound" {
		return errors.New("too much inbound from ")
	}

	c.goes.Go(func() {
		c.runPeer(peer, dir)
	})
	return nil
}

func (c *Communicator) runPeer(peer *Peer, dir string) {
	// defer peer.Disconnect(p2p.DiscRequested)

	c.logger.Info("Run peer", "p", peer.peerId)
	// 5sec timeout for handshake
	// ctx, cancel := context.WithTimeout(c.ctx, time.Second*5)
	// defer cancel()

	client := c.GetRPCClient(peer.ID())
	defer client.DRPCConn().Close()

	res, err := client.GetStatus(context.Background(), &pb.GetStatusRequest{})
	if err != nil {
		peer.logger.Error("Failed to get status", "err", err.Error())
		return
	}
	if !bytes.Equal(res.GenesisBlockId, c.chain.GenesisBlock().ID().Bytes()) {
		peer.logger.Error("Failed to handshake", "err", "genesis id mismatch", "genesisInRes", hex.EncodeToString(res.GenesisBlockId), "localGenesis", hex.EncodeToString(c.chain.GenesisBlock().ID().Bytes()))
		return
	}
	localClock := time.Now().Nanosecond()
	remoteClock := res.SysNanoTimestamp

	diffNano := localClock - int(remoteClock)
	if localClock < int(remoteClock) {
		diffNano = int(remoteClock) - localClock
	}
	if diffNano > int(types.BlockIntervalNano*2) {
		peer.logger.Error("Failed to handshake", "err", "sys time diff too large", "diffNano", diffNano)
		return
	}

	peer.UpdateHead(types.BytesToBytes32(res.BestBlockId), uint32(res.BestBlockNum))
	c.peerSet.Add(peer, dir)
	peersCountGauge.Set(float64(c.peerSet.Len()))
	peer.logger.Info(fmt.Sprintf("peer added (%v)", c.peerSet.Len()))

	defer func() {
		c.peerSet.Remove(peer.ID())
		peersCountGauge.Set(float64(c.peerSet.Len()))
		peer.logger.Info(fmt.Sprintf("peer removed (%v)", c.peerSet.Len()))
	}()

	select {
	// case <-peer.Done():
	case <-c.ctx.Done():
	case <-c.syncedCh:
		c.syncTxs(peer)
		select {
		// case <-peer.Done():
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
	bbytes, _ := rlp.EncodeToBytes(blk)
	blkID := blk.Block.ID()

	for _, peer := range toPropagate {
		peer := peer
		peer.MarkBlock(blk.Block.ID())
		c.goes.Go(func() {
			defer func() {
				if r := recover(); r != nil {
					peer.logger.Error("drpc send recovered from panic: %v", "err", r)
				}
			}()
			c.logger.Debug(fmt.Sprintf("propagate %s to %s", blk.Block.CompactString(), peer.ID()))
			client := c.GetRPCClient(peer.ID())

			if _, err := client.NotifyBlock(context.Background(), &pb.NotifyBlockRequest{BlockBytes: bbytes}); err != nil {
				peer.logger.Error(fmt.Sprintf("Failed to propagate %s", blk.Block.CompactString()), "err", err)
			}
		})
	}

	for _, peer := range toAnnounce {
		peer := peer
		peer.MarkBlock(blk.Block.ID())
		c.goes.Go(func() {
			defer func() {
				if r := recover(); r != nil {
					peer.logger.Error("drpc send recovered from panic: %v", "err", r)
				}
			}()
			c.logger.Debug(fmt.Sprintf("announce %s to %s", blk.Block.CompactString(), peer.ID()))
			client := c.GetRPCClient(peer.ID())

			if _, err := client.NotifyBlockID(context.Background(), &pb.NotifyBlockIDRequest{BlockIdBytes: blkID[:]}); err != nil {
				peer.logger.Error(fmt.Sprintf("Failed to announce %s", blk.Block.CompactString()), "err", err)
			}
		})
	}
}

// PeerCount returns count of peers.
func (c *Communicator) PeerCount() int {
	return c.peerSet.Len()
}

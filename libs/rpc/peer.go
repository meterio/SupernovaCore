// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package rpc

import (
	"fmt"
	"log/slog"
	"math/rand"
	"sync"

	"github.com/ethereum/go-ethereum/common/mclock"
	lru "github.com/hashicorp/golang-lru"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/meterio/supernova/types"
)

const (
	maxKnownTxs    = 32768 // Maximum transactions IDs to keep in the known list (prevent DOS)
	maxKnownBlocks = 1024  // Maximum block IDs to keep in the known list (prevent DOS)
	// maxKnownPowBlocks = 1024
)

func init() {

}

// Peer extends p2p.Peer with RPC integrated.
type Peer struct {
	peerId peer.ID
	logger *slog.Logger

	createdTime mclock.AbsTime
	knownTxs    *lru.Cache
	knownBlocks *lru.Cache
	// knownPowBlocks *lru.Cache
	head struct {
		sync.Mutex
		id     types.Bytes32
		number uint32
	}
}

func newPeer(peerID peer.ID) *Peer {
	ctx := []interface{}{
		"peer", peerID,
	}
	knownTxs, err := lru.New(maxKnownTxs)
	if err != nil {
		fmt.Println("known tx init error:", err)
	}

	knownBlocks, err := lru.New(maxKnownBlocks)
	if err != nil {
		fmt.Println("known blocks init error:", err)
	}

	return &Peer{
		peerId:      peerID,
		logger:      slog.With(ctx...),
		createdTime: mclock.Now(),
		knownTxs:    knownTxs,
		knownBlocks: knownBlocks,
	}
}

func (p *Peer) ID() peer.ID {
	return p.peerId
}

// Head returns head block ID and total score.
func (p *Peer) Head() (id types.Bytes32, number uint32) {
	p.head.Lock()
	defer p.head.Unlock()
	return p.head.id, p.head.number
}

// UpdateHead update ID and total score of head block.
func (p *Peer) UpdateHead(id types.Bytes32, number uint32) {
	p.head.Lock()
	defer p.head.Unlock()
	if number > p.head.number {
		p.head.id = id
		p.head.number = number
	}
}

// MarkTransaction marks a transaction to known.
func (p *Peer) MarkTransaction(id []byte) {
	p.knownTxs.Add(id, struct{}{})
}

// func (p *Peer) MarkPowBlock(id types.Bytes32) {
// 	p.knownPowBlocks.Add(id, struct{}{})
// }

// MarkBlock marks a block to known.
func (p *Peer) MarkBlock(id types.Bytes32) {
	p.knownBlocks.Add(id, struct{}{})
}

// IsTransactionKnown returns if the transaction is known.
func (p *Peer) IsTransactionKnown(id []byte) bool {
	return p.knownTxs.Contains(id)
}

// func (p *Peer) IsPowBlockKnown(id types.Bytes32) bool {
// 	return p.knownPowBlocks.Contains(id)
// }

// IsBlockKnown returns if the block is known.
func (p *Peer) IsBlockKnown(id types.Bytes32) bool {
	return p.knownBlocks.Contains(id)
}

// Duration returns duration of connection.
func (p *Peer) Duration() mclock.AbsTime {
	return mclock.Now() - p.createdTime
}

func (p *Peer) String() string {
	return p.peerId.String()
}

func (p *Peer) Debug(msg string, ctx ...interface{}) {
	p.logger.Debug(msg, ctx...)
}

func (p *Peer) Info(msg string, ctx ...interface{}) {
	p.logger.Info(msg, ctx...)
}

func (p *Peer) Error(msg string, ctx ...interface{}) {
	p.logger.Error(msg, ctx...)
}

func (p *Peer) Warn(msg string, ctx ...interface{}) {
	p.logger.Warn(msg, ctx...)
}

// Peers slice of peers
type Peers []*Peer

// Filter filter out sub set of peers that satisfies the given condition.
func (ps Peers) Filter(cond func(*Peer) bool) Peers {
	ret := make(Peers, 0, len(ps))
	for _, peer := range ps {
		if cond(peer) {
			ret = append(ret, peer)
		}
	}
	return ret
}

// Find find one peer that satisfies the given condition.
func (ps Peers) Find(cond func(*Peer) bool) *Peer {
	for _, peer := range ps {
		if cond(peer) {
			return peer
		}
	}
	return nil
}

type DirectionCount struct {
	Inbound  int
	Outbound int
}

// PeerSet manages a set of peers, which mapped by NodeID.
type PeerSet struct {
	m       map[peer.ID]*Peer
	d       map[peer.ID]string
	counter DirectionCount
	lock    sync.Mutex
}

// NewSet create a peer set instance.
func newPeerSet() *PeerSet {
	return &PeerSet{
		m:       make(map[peer.ID]*Peer),
		d:       make(map[peer.ID]string),
		counter: DirectionCount{0, 0},
	}
}

// Add add a new peer.
func (ps *PeerSet) Add(peer *Peer, dir string) {
	ps.lock.Lock()
	defer ps.lock.Unlock()
	ps.m[peer.peerId] = peer
	ps.d[peer.peerId] = dir
	if dir == "inbound" {
		ps.counter.Inbound++
	} else {
		ps.counter.Outbound++
	}
}

// Find find peer for given nodeID.
func (ps *PeerSet) Find(peerID peer.ID) *Peer {
	ps.lock.Lock()
	defer ps.lock.Unlock()
	return ps.m[peerID]
}

// Remove removes peer for given nodeID.
func (ps *PeerSet) Remove(nodeID peer.ID) *Peer {
	ps.lock.Lock()
	defer ps.lock.Unlock()
	if dir, ok := ps.d[nodeID]; ok {
		delete(ps.d, nodeID)
		if dir == "inbound" {
			ps.counter.Inbound--
		} else {
			ps.counter.Outbound--
		}
	}

	if peer, ok := ps.m[nodeID]; ok {
		delete(ps.m, nodeID)
		return peer
	}
	return nil
}

// Slice dumps all peers into a slice.
// The dumped slice is a random permutation.
func (ps *PeerSet) Slice() Peers {
	ps.lock.Lock()
	defer ps.lock.Unlock()

	ret := make(Peers, len(ps.m))
	perm := rand.Perm(len(ps.m))
	i := 0
	for _, s := range ps.m {
		// randomly
		ret[perm[i]] = s
		i++
	}
	return ret
}

// Len returns length of set.
func (ps *PeerSet) Len() int {
	ps.lock.Lock()
	defer ps.lock.Unlock()

	return len(ps.m)
}

func (ps *PeerSet) DirectionCount() DirectionCount {
	ps.lock.Lock()
	defer ps.lock.Unlock()
	return ps.counter
}

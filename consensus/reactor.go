// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package consensus

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	"github.com/meterio/supernova/libs/p2p"
	"github.com/prometheus/client_golang/prometheus"

	cmtcfg "github.com/cometbft/cometbft/config"
	cmtproxy "github.com/cometbft/cometbft/proxy"
	lru "github.com/hashicorp/golang-lru"
	"github.com/meterio/supernova/block"
	"github.com/meterio/supernova/chain"
	"github.com/meterio/supernova/txpool"
	"github.com/meterio/supernova/types"
)

var (
	validQCs, _ = lru.New(256)
	inQueue     *IncomingQueue
)

type ReactorConfig struct {
	EpochMBlockCount uint32
	MinCommitteeSize int
	MaxCommitteeSize int
}

// -----------------------------------------------------------------------------
// Reactor defines a reactor for the consensus service.
type Reactor struct {
	Pacemaker

	chain    *chain.Chain
	logger   *slog.Logger
	config   *cmtcfg.Config
	SyncDone bool

	lastKBlock uint32
	curNonce   uint64
}

func init() {
	inQueue = NewIncomingQueue()
}

// NewConsensusReactor returns a new Reactor with config
func NewConsensusReactor(ctx context.Context, config *cmtcfg.Config, chain *chain.Chain, p2pSrv p2p.P2P, txpool *txpool.TxPool, blsMaster *types.BlsMaster, proxyApp cmtproxy.AppConns) *Reactor {
	prometheus.Register(pmRoundGauge)
	prometheus.Register(curEpochGauge)
	prometheus.Register(inCommitteeGauge)
	prometheus.Register(pmRoleGauge)

	r := &Reactor{
		Pacemaker: *NewPacemaker(ctx, config.Version, chain, txpool, p2pSrv, blsMaster, proxyApp),
		chain:     chain,
		logger:    slog.With("pkg", "r"),
		SyncDone:  false,
		config:    config,
	}

	// initialize consensus common
	r.logger.Info("my pubkey", "pubkey", hex.EncodeToString(blsMaster.PubKey.Marshal()))

	return r
}

// OnStart implements BaseService by subscribing to events, which later will be
// broadcasted to other peers and starting state if we're not in fast sync.
func (r *Reactor) Start(ctx context.Context) error {

	vset := r.chain.GetBestNextValidatorSet()
	if vset.Size() <= 1 {
		r.Pacemaker.Start()
	} else {

		// select {
		// case <-ctx.Done():
		// 	r.logger.Warn("stop reactor due to context end")
		// 	return nil
		// case <-r.Pacemaker.communicator.SyncedCh():
		// 	r.SyncDone = true
		// 	r.logger.Info("syncing is done")
		// 	r.Pacemaker.Start()
		// }
		r.Pacemaker.Start()
	}

	return nil
}

// Process process a block.
func (r *Reactor) ValidateSyncedBlock(blk *block.Block, nowTimestamp uint64) error {
	header := blk.Header()

	if _, err := r.chain.GetBlockHeader(header.ID()); err != nil {
		if !r.chain.IsNotFound(err) {
			return err
		}
	} else {
		// we may already have this blockID. If it is after the best, still accept it
		if header.Number() <= r.chain.BestBlock().Number() {
			return errKnownBlock
		} else {
			r.logger.Debug("continue to process blk ...", "height", header.Number())
		}
	}

	parent, err := r.chain.GetBlock(header.ParentID)
	if err != nil {
		if !r.chain.IsNotFound(err) {
			return err
		}
		return errParentMissing
	}

	return r.validateBlock(blk, parent, nowTimestamp, true)
}

func (r *Reactor) validateBlock(
	block *block.Block,
	parent *block.Block,
	nowTimestamp uint64,
	forceValidate bool,
) error {
	header := block.Header()

	start := time.Now()
	if parent == nil {
		return consensusError("parent is nil")
	}

	if parent.ID() != block.QC.BlockID {
		return consensusError(fmt.Sprintf("parent.ID %v and QC.BlockID %v mismatch", parent.ID(), block.QC.BlockID))
	}

	if header.Timestamp <= parent.Timestamp() {
		return consensusError(fmt.Sprintf("block timestamp behind parents: parent %v, current %v", parent.Timestamp, header.Timestamp))
	}

	if header.Timestamp > nowTimestamp+types.BlockInterval {
		return errFutureBlock
	}

	if header.LastKBlock < parent.LastKBlock() {
		return consensusError(fmt.Sprintf("block LastKBlock invalid: parent %v, current %v", parent.LastKBlock, header.LastKBlock))
	}

	proposedTxs := block.Transactions()
	if !bytes.Equal(header.TxsRoot, proposedTxs.RootHash()) {
		return consensusError(fmt.Sprintf("block txs root mismatch: want %v, have %v", header.TxsRoot, proposedTxs.RootHash()))
	}

	if forceValidate && header.LastKBlock != r.Pacemaker.EpochStartKBlockNum() {
		return consensusError(fmt.Sprintf("header LastKBlock invalid: header %v, local %v", header.LastKBlock, r.Pacemaker.EpochStartKBlockNum()))
	}

	r.logger.Debug("validated block", "id", block.CompactString(), "elapsed", types.PrettyDuration(time.Since(start)))
	return nil
}

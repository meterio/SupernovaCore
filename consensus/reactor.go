// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package consensus

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"runtime"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	cmtcfg "github.com/cometbft/cometbft/config"
	cmtproxy "github.com/cometbft/cometbft/proxy"
	lru "github.com/hashicorp/golang-lru"
	"github.com/meterio/supernova/block"
	"github.com/meterio/supernova/chain"
	"github.com/meterio/supernova/libs/comm"
	"github.com/meterio/supernova/txpool"
	"github.com/meterio/supernova/types"
)

var (
	validQCs, _ = lru.New(256)
	inQueue     *IncomingQueue
	outQueue    *OutgoingQueue
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
	outQueue = NewOutgoingQueue()
}

// NewConsensusReactor returns a new Reactor with config
func NewConsensusReactor(config *cmtcfg.Config, chain *chain.Chain, comm *comm.Communicator, txpool *txpool.TxPool, blsMaster *types.BlsMaster, proxyApp cmtproxy.AppConns) *Reactor {
	prometheus.Register(pmRoundGauge)
	prometheus.Register(curEpochGauge)
	prometheus.Register(inCommitteeGauge)
	prometheus.Register(pmRoleGauge)

	r := &Reactor{
		Pacemaker: *NewPacemaker(config.Version, chain, txpool, comm, blsMaster, proxyApp),
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
	go outQueue.Start(ctx)

	vset := r.chain.GetBestNextValidatorSet()
	if vset.Size() <= 1 {
		r.Pacemaker.Start()
	} else {

		select {
		case <-ctx.Done():
			r.logger.Warn("stop reactor due to context end")
			return nil
		case <-r.Pacemaker.communicator.SyncedCh():
			r.SyncDone = true
			r.logger.Info("syncing is done")
			r.Pacemaker.Start()
		}
	}

	return nil
}

// ------------------------------------
// UTILITY
// ------------------------------------
func (r *Reactor) OnReceiveMsg(w http.ResponseWriter, req *http.Request) {
	defer req.Body.Close()

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// r.logger.Debug("before receive", "alloc", types.PrettyStorage(m.Alloc), "sys", types.PrettyStorage(m.Sys))

	data, err := io.ReadAll(req.Body)
	if err != nil {
		r.logger.Error("Unrecognized payload", "err", err)
		return
	}
	mi, err := r.UnmarshalMsg(data)
	if err != nil {
		r.logger.Error("Unmarshal error", "err", err, "from", req.RemoteAddr)
		return
	}
	defer func() {
		var ma runtime.MemStats
		runtime.ReadMemStats(&ma)
		// r.logger.Debug(fmt.Sprintf("after receive %s", mi.Msg.GetType()), "allocDiff", types.PrettyStorage(ma.Alloc-m.Alloc), "sysDiff(KB)", types.PrettyStorage(ma.Sys-m.Sys))
	}()

	r.AddIncoming(*mi, data)

}

func (r *Reactor) AddIncoming(mi IncomingMsg, data []byte) {
	msg, peer := mi.Msg, mi.Peer
	typeName := mi.Msg.GetType()

	if msg.GetEpoch() < r.Pacemaker.epochState.epoch {
		r.logger.Info(fmt.Sprintf("outdated %s, dropped ...", msg.String()), "peer", peer.String())
		return
	}

	if msg.GetEpoch() == r.Pacemaker.epochState.epoch {
		signerIndex := msg.GetSignerIndex()
		if signerIndex >= r.Pacemaker.epochState.CommitteeSize() {
			r.logger.Warn("index out of range for signer, dropped ...", "peer", peer, "msg", msg.GetType())
			return
		}
		_, signer := r.Pacemaker.epochState.GetValidatorByIndex(int(signerIndex))

		if !msg.VerifyMsgSignature(signer.PubKey) {
			r.logger.Error("invalid signature, dropped ...", "peer", peer, "msg", msg.String(), "signer", signer.Name)
			return
		}
		mi.Signer.IP = signer.IP.String()
		mi.Signer.Name = signer.Name
	}

	// sanity check for messages
	switch m := msg.(type) {
	case *block.PMProposalMessage:
		blk := m.DecodeBlock()
		if blk == nil {
			r.logger.Error("Invalid PMProposal: could not decode proposed block")
			return
		}

	case *block.PMTimeoutMessage:
		qcHigh := m.DecodeQCHigh()
		if qcHigh == nil {
			r.logger.Error("Invalid QCHigh: could not decode qcHigh")
		}
	}

	fromMyself := r.Pacemaker.isMe(&peer)

	if msg.GetEpoch() == r.Pacemaker.epochState.epoch {
		err := inQueue.Add(mi)
		if err != nil {
			return
		}

		// relay the message if these two conditions are met:
		// 1. the original message is not sent by myself
		// 2. it's a proposal message
		if (!fromMyself || len(r.Pacemaker.addedValidators) > 0) && typeName == "PMProposal" {
			r.Relay(mi.Msg, data)
		}
	} else {
		time.AfterFunc(time.Second, func() {
			r.logger.Info(fmt.Sprintf("future message %s in epoch %d, process after 1s ...", msg.GetType(), msg.GetEpoch()), "curEpoch", r.Pacemaker.epochState.epoch)
			r.AddIncoming(mi, data)
		})
	}
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

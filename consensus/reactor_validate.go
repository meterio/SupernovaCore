// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package consensus

import (
	"bytes"
	"fmt"
	"time"

	"github.com/pkg/errors"

	"github.com/meterio/supernova/block"
	"github.com/meterio/supernova/types"
)

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

	return r.Validate(blk, parent, nowTimestamp, true)
}

func (r *Reactor) Validate(
	block *block.Block,
	parent *block.Block,
	nowTimestamp uint64,
	forceValidate bool,
) error {
	header := block.Header()
	epoch := block.Epoch()

	vHeaderStart := time.Now()
	if err := r.validateBlockHeader(header, parent.Header(), nowTimestamp, forceValidate, epoch); err != nil {
		r.logger.Info("validate header error", "blk", block.ID().ToBlockShortID(), "err", err)
		return err
	}
	vHeaderElapsed := time.Since(vHeaderStart)

	vProposerStart := time.Now()
	if err := r.validateProposer(header, parent.Header()); err != nil {
		r.logger.Info("validate proposer error", "blk", block.ID().ToBlockShortID(), "err", err)
		return err
	}
	vProposerElapsed := time.Since(vProposerStart)

	vBodyStart := time.Now()
	if err := r.validateBlockBody(block, parent, forceValidate); err != nil {
		r.logger.Info("validate body error", "blk", block.ID().ToBlockShortID(), "err", err)
		return err
	}
	vBodyElapsed := time.Since(vBodyStart)

	r.logger.Debug("validated!", "id", block.CompactString(), "validateHeadElapsed", types.PrettyDuration(vHeaderElapsed), "validateProposerElapsed", types.PrettyDuration(vProposerElapsed), "validateBodyElapsed", types.PrettyDuration(vBodyElapsed))

	return nil
}

func (r *Reactor) validateBlockHeader(header *block.Header, parent *block.Header, nowTimestamp uint64, forceValidate bool, epoch uint64) error {
	if header.Timestamp <= parent.Timestamp {
		return consensusError(fmt.Sprintf("block timestamp behind parents: parent %v, current %v", parent.Timestamp, header.Timestamp))
	}

	if header.Timestamp > nowTimestamp+types.BlockInterval {
		return errFutureBlock
	}

	if header.LastKBlock < parent.LastKBlock {
		return consensusError(fmt.Sprintf("block LastKBlock invalid: parent %v, current %v", parent.LastKBlock, header.LastKBlock))
	}

	if forceValidate && header.LastKBlock != r.lastKBlock {
		return consensusError(fmt.Sprintf("header LastKBlock invalid: header %v, local %v", header.LastKBlock, r.lastKBlock))
	}

	return nil
}

func (r *Reactor) validateProposer(header *block.Header, parent *block.Header) error {
	return nil
	// _, err := header.Signer()
	// if err != nil {
	// 	return consensusError(fmt.Sprintf("block signer unavailable: %v", err))
	// }
	// // fmt.Println("signer", signer)
	// return nil
}

func (r *Reactor) validateBlockBody(blk *block.Block, parent *block.Block, forceValidate bool) error {
	header := blk.Header()
	proposedTxs := blk.Transactions()
	if !bytes.Equal(header.TxsRoot, proposedTxs.RootHash()) {
		return consensusError(fmt.Sprintf("block txs root mismatch: want %v, have %v", header.TxsRoot, proposedTxs.RootHash()))
	}

	txUniteHashs := make(map[types.Bytes32]int)
	clauseUniteHashs := make(map[types.Bytes32]int)

	if parent == nil {
		r.logger.Error("parent is nil")
		return errors.New("parent is nil")
	}

	if len(txUniteHashs) != 0 {
		for key, value := range txUniteHashs {
			if value != 0 {
				return consensusError(fmt.Sprintf("local kblock has %v more tx with uniteHash: %v", value, key))
			}
		}
	}

	if len(clauseUniteHashs) != 0 {
		for key, value := range clauseUniteHashs {
			if value < 0 {
				return consensusError(fmt.Sprintf("local kblock has %v more clause with uniteHash: %v", value, key))
			}
		}
	}

	return nil
}

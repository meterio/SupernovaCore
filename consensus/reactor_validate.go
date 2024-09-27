// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package consensus

import (
	"fmt"
	"time"

	"github.com/pkg/errors"

	"github.com/meterio/meter-pov/block"
	"github.com/meterio/meter-pov/meter"
	"github.com/meterio/meter-pov/runtime"
	"github.com/meterio/meter-pov/state"
	"github.com/meterio/meter-pov/tx"
	"github.com/meterio/meter-pov/xenv"
)

// Process process a block.
func (c *Reactor) ProcessSyncedBlock(blk *block.Block, nowTimestamp uint64) (*state.Stage, tx.Receipts, error) {
	header := blk.Header()

	if _, err := c.chain.GetBlockHeader(header.ID()); err != nil {
		if !c.chain.IsNotFound(err) {
			return nil, nil, err
		}
	} else {
		// we may already have this blockID. If it is after the best, still accept it
		if header.Number() <= c.chain.BestBlock().Number() {
			return nil, nil, errKnownBlock
		} else {
			c.logger.Debug("continue to process blk ...", "height", header.Number())
		}
	}

	parent, err := c.chain.GetBlock(header.ParentID())
	if err != nil {
		if !c.chain.IsNotFound(err) {
			return nil, nil, err
		}
		return nil, nil, errParentMissing
	}

	state, err := c.stateCreator.NewState(parent.StateRoot())
	if err != nil {
		return nil, nil, err
	}

	stage, receipts, err := c.Validate(state, blk, parent, nowTimestamp, false)
	if err != nil {
		return nil, nil, err
	}

	return stage, receipts, nil
}

func (c *Reactor) ProcessProposedBlock(parent *block.Block, blk *block.Block, nowTimestamp uint64) (*state.Stage, tx.Receipts, error) {
	if _, err := c.chain.GetBlockHeader(blk.ID()); err != nil {
		if !c.chain.IsNotFound(err) {
			c.logger.Info("get block header error", "blk", blk.ShortID(), "err", err)
			return nil, nil, err
		}
	} else {
		c.logger.Info("known block", "blk", blk.ShortID())
		return nil, nil, errKnownBlock
	}

	if parent == nil {
		c.logger.Info("parent missing", "blk", blk.ShortID())
		return nil, nil, errParentHeaderMissing
	}

	state, err := c.stateCreator.NewState(parent.StateRoot())
	if err != nil {
		c.logger.Info("new state error", "blk", blk.ShortID(), "err", err)
		return nil, nil, err
	}

	stage, receipts, err := c.Validate(state, blk, parent, nowTimestamp, true)
	if err != nil {
		c.logger.Info("validate error", "blk", blk.ShortID(), "err", err)
		return nil, nil, err
	}

	return stage, receipts, nil
}

func (c *Reactor) Validate(
	state *state.State,
	block *block.Block,
	parent *block.Block,
	nowTimestamp uint64,
	forceValidate bool,
) (*state.Stage, tx.Receipts, error) {
	header := block.Header()
	epoch := block.GetBlockEpoch()

	vHeaderStart := time.Now()
	if err := c.validateBlockHeader(header, parent.Header(), nowTimestamp, forceValidate, epoch); err != nil {
		c.logger.Info("validate header error", "blk", block.ID().ToBlockShortID(), "err", err)
		return nil, nil, err
	}
	vHeaderElapsed := time.Since(vHeaderStart)

	vProposerStart := time.Now()
	if err := c.validateProposer(header, parent.Header(), state); err != nil {
		c.logger.Info("validate proposer error", "blk", block.ID().ToBlockShortID(), "err", err)
		return nil, nil, err
	}
	vProposerElapsed := time.Since(vProposerStart)

	vBodyStart := time.Now()
	if err := c.validateBlockBody(block, parent, forceValidate); err != nil {
		c.logger.Info("validate body error", "blk", block.ID().ToBlockShortID(), "err", err)
		return nil, nil, err
	}
	vBodyElapsed := time.Since(vBodyStart)

	verifyBlockStart := time.Now()
	stage, receipts, err := c.VerifyBlock(block, state, forceValidate)
	if err != nil {
		c.logger.Error("validate block error", "blk", block.ID().ToBlockShortID(), "err", err)
		return nil, nil, err
	}
	verifyBlockElapsed := time.Since(verifyBlockStart)
	c.logger.Debug("validated!", "id", block.ShortID(), "validateHeadElapsed", meter.PrettyDuration(vHeaderElapsed), "validateProposerElapsed", meter.PrettyDuration(vProposerElapsed), "validateBodyElapsed", meter.PrettyDuration(vBodyElapsed), "verifyBlockElapsed", meter.PrettyDuration(verifyBlockElapsed))

	return stage, receipts, nil
}

func (c *Reactor) validateBlockHeader(header *block.Header, parent *block.Header, nowTimestamp uint64, forceValidate bool, epoch uint64) error {
	if header.Timestamp() <= parent.Timestamp() {
		return consensusError(fmt.Sprintf("block timestamp behind parents: parent %v, current %v", parent.Timestamp(), header.Timestamp()))
	}

	if header.Timestamp() > nowTimestamp+meter.BlockInterval {
		return errFutureBlock
	}

	if !block.GasLimit(header.GasLimit()).IsValid(parent.GasLimit()) {
		return consensusError(fmt.Sprintf("block gas limit invalid: parent %v, current %v", parent.GasLimit(), header.GasLimit()))
	}

	if header.GasUsed() > header.GasLimit() {
		return consensusError(fmt.Sprintf("block gas used exceeds limit: limit %v, used %v", header.GasLimit(), header.GasUsed()))
	}

	if header.TotalScore() <= parent.TotalScore() {
		return consensusError(fmt.Sprintf("block total score invalid: parent %v, current %v", parent.TotalScore(), header.TotalScore()))
	}

	if epoch != meter.KBlockEpoch && header.LastKBlockHeight() < parent.LastKBlockHeight() {
		return consensusError(fmt.Sprintf("block LastKBlockHeight invalid: parent %v, current %v", parent.LastKBlockHeight(), header.LastKBlockHeight()))
	}

	if forceValidate && header.LastKBlockHeight() != c.lastKBlockHeight {
		return consensusError(fmt.Sprintf("header LastKBlockHeight invalid: header %v, local %v", header.LastKBlockHeight(), c.lastKBlockHeight))
	}

	return nil
}

func (c *Reactor) validateProposer(header *block.Header, parent *block.Header, st *state.State) error {
	_, err := header.Signer()
	if err != nil {
		return consensusError(fmt.Sprintf("block signer unavailable: %v", err))
	}
	// fmt.Println("signer", signer)
	return nil
}

func (c *Reactor) validateBlockBody(blk *block.Block, parent *block.Block, forceValidate bool) error {
	header := blk.Header()
	proposedTxs := blk.Transactions()
	if header.TxsRoot() != proposedTxs.RootHash() {
		return consensusError(fmt.Sprintf("block txs root mismatch: want %v, have %v", header.TxsRoot(), proposedTxs.RootHash()))
	}
	if blk.GetMagic() != block.BlockMagicVersion1 {
		return consensusError(fmt.Sprintf("block magic mismatch, has %v, expect %v", blk.GetMagic(), block.BlockMagicVersion1))
	}

	txUniteHashs := make(map[meter.Bytes32]int)
	clauseUniteHashs := make(map[meter.Bytes32]int)

	if parent == nil {
		c.logger.Error("parent is nil")
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

func (c *Reactor) VerifyBlock(blk *block.Block, state *state.State, forceValidate bool) (*state.Stage, tx.Receipts, error) {
	var totalGasUsed uint64
	txs := blk.Transactions()
	receipts := make(tx.Receipts, 0, len(txs))
	processedTxs := make(map[meter.Bytes32]bool)
	header := blk.Header()
	signer, _ := header.Signer()
	rt := runtime.New(
		c.chain.NewSeeker(header.ParentID()),
		state,
		&xenv.BlockContext{
			Signer:     signer,
			Number:     header.Number(),
			Time:       header.Timestamp(),
			GasLimit:   header.GasLimit(),
			TotalScore: header.TotalScore(),
		})

	findTx := func(txID meter.Bytes32) (found bool, reverted bool, err error) {
		if reverted, ok := processedTxs[txID]; ok {
			return true, reverted, nil
		}
		meta, err := c.chain.GetTransactionMeta(txID, header.ParentID())
		if err != nil {
			if c.chain.IsNotFound(err) {
				return false, false, nil
			}
			return false, false, err
		}
		return true, meta.Reverted, nil
	}

	for _, tx := range txs {
		// Mint transaction critiers:
		// 1. no signature (no signer)
		// 2. only located in 1st transaction in kblock.
		signer, err := tx.Signer()
		if err != nil {
			return nil, nil, consensusError(fmt.Sprintf("tx signer unavailable: %v", err))
		}

		if signer.IsZero() {
			//TBD: check to addresses in clauses
			if !blk.IsKBlock() {
				return nil, nil, consensusError(fmt.Sprintf("tx signer unavailable"))
			}
		}

		// check if tx existed
		if found, _, err := findTx(tx.ID()); err != nil {
			return nil, nil, err
		} else if found {
			return nil, nil, consensusError("tx already exists")
		}

		// check depended tx
		if dep := tx.DependsOn(); dep != nil {
			found, reverted, err := findTx(*dep)
			if err != nil {
				return nil, nil, err
			}
			if !found {
				return nil, nil, consensusError("tx dep broken")
			}

			if reverted {
				return nil, nil, consensusError("tx dep reverted")
			}
		}

		receipt, err := rt.ExecuteTransaction(tx)
		if err != nil {
			c.logger.Info("exe tx error", "err", err)
			return nil, nil, err
		}

		totalGasUsed += receipt.GasUsed
		receipts = append(receipts, receipt)
		processedTxs[tx.ID()] = receipt.Reverted
	}

	if header.GasUsed() != totalGasUsed {
		return nil, nil, consensusError(fmt.Sprintf("block gas used mismatch: want %v, have %v", header.GasUsed(), totalGasUsed))
	}

	receiptsRoot := receipts.RootHash()
	if header.ReceiptsRoot() != receiptsRoot {
		return nil, nil, consensusError(fmt.Sprintf("block receipts root mismatch: want %v, have %v", header.ReceiptsRoot(), receiptsRoot))
	}

	if err := rt.Seeker().Err(); err != nil {
		return nil, nil, errors.WithMessage(err, "chain")
	}

	stage := state.Stage()
	stateRoot, err := stage.Hash()
	if err != nil {
		return nil, nil, err
	}

	if blk.Header().StateRoot() != stateRoot {
		return nil, nil, consensusError(fmt.Sprintf("block state root mismatch: want %v, have %v", header.StateRoot(), stateRoot))
	}

	return stage, receipts, nil
}

// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package consensus

import (
	"bytes"
	"context"
	"fmt"
	"time"

	v1 "github.com/cometbft/cometbft/api/cometbft/abci/v1"
	"github.com/pkg/errors"

	"github.com/meterio/supernova/block"
	"github.com/meterio/supernova/types"
)

// Process process a block.
func (c *Reactor) ProcessSyncedBlock(blk *block.Block, nowTimestamp uint64) error {
	header := blk.Header()

	if _, err := c.chain.GetBlockHeader(header.ID()); err != nil {
		if !c.chain.IsNotFound(err) {
			return err
		}
	} else {
		// we may already have this blockID. If it is after the best, still accept it
		if header.Number() <= c.chain.BestBlock().Number() {
			return errKnownBlock
		} else {
			c.logger.Debug("continue to process blk ...", "height", header.Number())
		}
	}

	_, err := c.chain.GetBlock(header.ParentID)
	if err != nil {
		if !c.chain.IsNotFound(err) {
			return err
		}
		return errParentMissing
	}
	return nil
}

func (c *Reactor) ProcessProposedBlock(parent *block.Block, blk *block.Block, nowTimestamp uint64) error {
	res, err := c.proxyApp.Consensus().ProcessProposal(context.TODO(), &v1.ProcessProposalRequest{Txs: blk.Txs.Convert(), Hash: blk.ID().Bytes(), Height: int64(blk.Number())})
	if err != nil {
		return err
	}

	if res.Status == v1.PROCESS_PROPOSAL_STATUS_ACCEPT {
		return nil
	} else if res.Status == v1.PROCESS_PROPOSAL_STATUS_REJECT {
		return ErrProposalRejected
	}
	return ErrProposalUnknown

}

func (c *Reactor) Validate(
	block *block.Block,
	parent *block.Block,
	nowTimestamp uint64,
	forceValidate bool,
) error {
	header := block.Header()
	epoch := block.GetBlockEpoch()

	vHeaderStart := time.Now()
	if err := c.validateBlockHeader(header, parent.Header(), nowTimestamp, forceValidate, epoch); err != nil {
		c.logger.Info("validate header error", "blk", block.ID().ToBlockShortID(), "err", err)
		return err
	}
	vHeaderElapsed := time.Since(vHeaderStart)

	vProposerStart := time.Now()
	if err := c.validateProposer(header, parent.Header()); err != nil {
		c.logger.Info("validate proposer error", "blk", block.ID().ToBlockShortID(), "err", err)
		return err
	}
	vProposerElapsed := time.Since(vProposerStart)

	vBodyStart := time.Now()
	if err := c.validateBlockBody(block, parent, forceValidate); err != nil {
		c.logger.Info("validate body error", "blk", block.ID().ToBlockShortID(), "err", err)
		return err
	}
	vBodyElapsed := time.Since(vBodyStart)

	verifyBlockStart := time.Now()
	err := c.VerifyBlock(block, forceValidate)
	if err != nil {
		c.logger.Error("validate block error", "blk", block.ID().ToBlockShortID(), "err", err)
		return err
	}
	verifyBlockElapsed := time.Since(verifyBlockStart)
	c.logger.Debug("validated!", "id", block.ShortID(), "validateHeadElapsed", types.PrettyDuration(vHeaderElapsed), "validateProposerElapsed", types.PrettyDuration(vProposerElapsed), "validateBodyElapsed", types.PrettyDuration(vBodyElapsed), "verifyBlockElapsed", types.PrettyDuration(verifyBlockElapsed))

	return nil
}

func (c *Reactor) validateBlockHeader(header *block.Header, parent *block.Header, nowTimestamp uint64, forceValidate bool, epoch uint64) error {
	if header.Timestamp <= parent.Timestamp {
		return consensusError(fmt.Sprintf("block timestamp behind parents: parent %v, current %v", parent.Timestamp, header.Timestamp))
	}

	if header.Timestamp > nowTimestamp+types.BlockInterval {
		return errFutureBlock
	}

	if epoch != types.KBlockEpoch && header.LastKBlockHeight < parent.LastKBlockHeight {
		return consensusError(fmt.Sprintf("block LastKBlockHeight invalid: parent %v, current %v", parent.LastKBlockHeight, header.LastKBlockHeight))
	}

	if forceValidate && header.LastKBlockHeight != c.lastKBlockHeight {
		return consensusError(fmt.Sprintf("header LastKBlockHeight invalid: header %v, local %v", header.LastKBlockHeight, c.lastKBlockHeight))
	}

	return nil
}

func (c *Reactor) validateProposer(header *block.Header, parent *block.Header) error {
	return nil
	// _, err := header.Signer()
	// if err != nil {
	// 	return consensusError(fmt.Sprintf("block signer unavailable: %v", err))
	// }
	// // fmt.Println("signer", signer)
	// return nil
}

func (c *Reactor) validateBlockBody(blk *block.Block, parent *block.Block, forceValidate bool) error {
	header := blk.Header()
	proposedTxs := blk.Transactions()
	if !bytes.Equal(header.TxsRoot, proposedTxs.RootHash()) {
		return consensusError(fmt.Sprintf("block txs root mismatch: want %v, have %v", header.TxsRoot, proposedTxs.RootHash()))
	}
	if blk.GetMagic() != block.BlockMagicVersion1 {
		return consensusError(fmt.Sprintf("block magic mismatch, has %v, expect %v", blk.GetMagic(), block.BlockMagicVersion1))
	}

	txUniteHashs := make(map[types.Bytes32]int)
	clauseUniteHashs := make(map[types.Bytes32]int)

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

func (c *Reactor) VerifyBlock(blk *block.Block, forceValidate bool) error {
	// FIXME: imple this
	return nil
	// var totalGasUsed uint64
	// txs := blk.Transactions()
	// receipts := make(tx.Receipts, 0, len(txs))
	// processedTxs := make(map[types.Bytes32]bool)
	// header := blk.Header()

	// findTx := func(txID types.Bytes32) (found bool, reverted bool, err error) {
	// 	if reverted, ok := processedTxs[txID]; ok {
	// 		return true, reverted, nil
	// 	}
	// 	meta, err := c.chain.GetTransactionMeta(txID, header.ParentID())
	// 	if err != nil {
	// 		if c.chain.IsNotFound(err) {
	// 			return false, false, nil
	// 		}
	// 		return false, false, err
	// 	}
	// 	return true, meta.Reverted, nil
	// }

	// for _, tx := range txs {
	// 	// Mint transaction critiers:
	// 	// 1. no signature (no signer)
	// 	// 2. only located in 1st transaction in kblock.
	// 	signer, err := tx.Signer()
	// 	if err != nil {
	// 		return nil, nil, consensusError(fmt.Sprintf("tx signer unavailable: %v", err))
	// 	}

	// 	if signer.IsZero() {
	// 		//TBD: check to addresses in clauses
	// 		if !blk.IsKBlock() {
	// 			return nil, nil, consensusError(fmt.Sprintf("tx signer unavailable"))
	// 		}
	// 	}

	// 	// check if tx existed
	// 	if found, _, err := findTx(tx.Hash()); err != nil {
	// 		return nil, nil, err
	// 	} else if found {
	// 		return nil, nil, consensusError("tx already exists")
	// 	}

	// 	// check depended tx
	// 	if dep := tx.DependsOn(); dep != nil {
	// 		found, reverted, err := findTx(*dep)
	// 		if err != nil {
	// 			return nil, nil, err
	// 		}
	// 		if !found {
	// 			return nil, nil, consensusError("tx dep broken")
	// 		}

	// 		if reverted {
	// 			return nil, nil, consensusError("tx dep reverted")
	// 		}
	// 	}

	// 	receipt, err := rt.ExecuteTransaction(tx)
	// 	if err != nil {
	// 		c.logger.Info("exe tx error", "err", err)
	// 		return nil, nil, err
	// 	}

	// 	totalGasUsed += receipt.GasUsed
	// 	receipts = append(receipts, receipt)
	// 	processedTxs[tx.Hash()] = receipt.Reverted
	// }

	// if header.GasUsed() != totalGasUsed {
	// 	return nil, nil, consensusError(fmt.Sprintf("block gas used mismatch: want %v, have %v", header.GasUsed(), totalGasUsed))
	// }

	// receiptsRoot := receipts.RootHash()
	// if header.ReceiptsRoot() != receiptsRoot {
	// 	return nil, nil, consensusError(fmt.Sprintf("block receipts root mismatch: want %v, have %v", header.ReceiptsRoot(), receiptsRoot))
	// }

	// if err := rt.Seeker().Err(); err != nil {
	// 	return nil, nil, errors.WithMessage(err, "chain")
	// }

	// stage := state.Stage()
	// stateRoot, err := stage.Hash()
	// if err != nil {
	// 	return nil, nil, err
	// }

	// if blk.Header().StateRoot() != stateRoot {
	// 	return nil, nil, consensusError(fmt.Sprintf("block state root mismatch: want %v, have %v", header.StateRoot(), stateRoot))
	// }

	// return stage, receipts, nil
}

// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package blocks

import (
	"encoding/hex"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/meterio/supernova/api/utils"
	"github.com/meterio/supernova/block"
	"github.com/meterio/supernova/chain"
	"github.com/meterio/supernova/types"
	"github.com/pkg/errors"
)

const MaxUint32 = 1<<32 - 1

type Blocks struct {
	chain  *chain.Chain
	logger *slog.Logger
}

func New(chain *chain.Chain) *Blocks {
	return &Blocks{
		chain,
		slog.With("api", "blk"),
	}
}

func (b *Blocks) handleGetBlock(w http.ResponseWriter, req *http.Request) error {
	revision, err := b.parseRevision(mux.Vars(req)["revision"])
	if err != nil {
		return utils.BadRequest(errors.WithMessage(err, "revision"))
	}
	expanded := req.URL.Query().Get("expanded")
	if expanded != "" && expanded != "false" && expanded != "true" {
		return utils.BadRequest(errors.WithMessage(errors.New("should be boolean"), "expanded"))
	}

	block, err := b.getBlock(revision)
	if err != nil {
		if b.chain.IsNotFound(err) {
			return utils.WriteJSON(w, nil)
		}
		return err
	}
	isTrunk, err := b.isTrunk(block.ID(), block.Number())
	if err != nil {
		return err
	}

	jSummary := buildJSONBlockSummary(block, isTrunk)
	if expanded == "true" {
		var err error
		var txs types.Transactions
		if block.ID().String() == b.chain.GenesisBlock().ID().String() {
			// if is genesis

		} else {
			txs = block.Txs
			if err != nil {
				return err
			}
		}

		return utils.WriteJSON(w, &JSONExpandedBlock{
			jSummary,
			buildJSONEmbeddedTxs(txs),
		})
	}
	txIds := make([]string, 0)
	for _, tx := range block.Txs {
		txIds = append(txIds, hex.EncodeToString(tx.Hash()))
	}
	return utils.WriteJSON(w, &JSONCollapsedBlock{jSummary, txIds})
}

func (b *Blocks) parseRevision(revision string) (interface{}, error) {
	if revision == "" || revision == "best" {
		return nil, nil
	}
	if len(revision) == 66 || len(revision) == 64 {
		blockID, err := types.ParseBytes32(revision)
		if err != nil {
			return nil, err
		}
		return blockID, nil
	}
	n, err := strconv.ParseUint(revision, 0, 0)
	if err != nil {
		return nil, err
	}
	if n > MaxUint32 {
		return nil, errors.New("block number out of max uint32")
	}
	return uint32(n), err
}

func (b *Blocks) parseEpoch(epoch string) (uint32, error) {
	n, err := strconv.ParseUint(epoch, 0, 0)
	if err != nil {
		return 0, err
	}
	if n > MaxUint32 {
		return 0, errors.New("block number out of max uint32")
	}
	return uint32(n), err
}

func (b *Blocks) getBlock(revision interface{}) (*block.Block, error) {
	switch revision.(type) {
	case types.Bytes32:
		blk, err := b.chain.GetBlock(revision.(types.Bytes32))
		if err != nil {
			return blk, err
		}
		best := b.chain.BestBlock()
		if blk.Number() > best.Number() {
			return nil, chain.ErrNotFound
		}
		return blk, err
	case uint32:
		best := b.chain.BestBlock()
		if revision.(uint32) > best.Number() {
			return nil, chain.ErrNotFound
		}
		return b.chain.GetTrunkBlock(revision.(uint32))
	default:
		return b.chain.BestBlock(), nil
	}
}

func (b *Blocks) isTrunk(blkID types.Bytes32, blkNum uint32) (bool, error) {
	best := b.chain.BestBlock()
	ancestorID, err := b.chain.GetAncestorBlockID(best.ID(), blkNum)
	if err != nil {
		return false, err
	}
	return ancestorID == blkID, nil
}

func (b *Blocks) handleGetQC(w http.ResponseWriter, req *http.Request) error {
	revision, err := b.parseRevision(mux.Vars(req)["revision"])
	if err != nil {
		return utils.BadRequest(errors.WithMessage(err, "revision"))
	}
	block, err := b.getBlock(revision)
	if err != nil {
		if b.chain.IsNotFound(err) {
			return utils.WriteJSON(w, nil)
		}
		return err
	}
	_, err = b.isTrunk(block.ID(), block.Number())
	if err != nil {
		return err
	}
	qc, err := convertQCWithRaw(block.QC)
	if err != nil {
		return err
	}
	return utils.WriteJSON(w, qc)
}

func (b *Blocks) Mount(root *mux.Router, pathPrefix string) {
	sub := root.PathPrefix(pathPrefix).Subrouter()
	sub.Path("/qc/{revision}").Methods("Get").HandlerFunc(utils.WrapHandlerFunc(b.handleGetQC))
}

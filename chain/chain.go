// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package chain

import (
	"bytes"
	"fmt"
	"log/slog"
	"sync"

	db "github.com/cometbft/cometbft-db"
	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/meterio/supernova/block"
	"github.com/meterio/supernova/libs/co"
	"github.com/meterio/supernova/types"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	blockCacheLimit = 512
)

var ErrNotFound = errors.New("not found")
var ErrBlockExist = errors.New("block already exists")
var errParentNotFinalized = errors.New("parent is not finalized")
var (
	bestHeightGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "best_height",
		Help: "BestBlock height",
	})
	bestQCHeightGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "best_qc_height",
		Help: "BestQC height",
	})
)
var ErrInvalidGenesis = errors.New("invalid genesis")

// Chain describes a persistent block chain.
// It's thread-safe.
type Chain struct {
	kv           db.DB
	genesisBlock *block.Block
	bestBlock    *block.Block
	bestQC       *block.QuorumCert
	tag          byte
	caches       caches
	rw           sync.RWMutex
	tick         co.Signal

	bestBlockBeforeIndexFlattern *block.Block
	proposalMap                  *ProposalMap
	drw                          sync.RWMutex
	bestPowNonce                 uint64

	logger *slog.Logger
}

type caches struct {
	rawBlocks *cache
}

var log = slog.With("pkg", "c")

// New create an instance of Chain.
func New(kv db.DB, genesisBlock *block.Block, genesisValidatorSet *types.ValidatorSet, verbose bool) (*Chain, error) {
	prometheus.Register(bestQCHeightGauge)
	prometheus.Register(bestHeightGauge)
	logger := slog.With("pkg", "c")

	if genesisBlock.Number() != 0 {
		fmt.Println(genesisBlock.Number())
		return nil, errors.New("genesis number != 0")
	}
	if len(genesisBlock.Transactions()) != 0 {
		return nil, errors.New("genesis block should not have transactions")
	}
	var bestBlock *block.Block

	if !bytes.Equal(genesisValidatorSet.Hash(), genesisBlock.ValidatorHash().Bytes()) {
		panic(ErrInvalidGenesis)
	}
	if _, err := loadValidatorSet(kv, genesisValidatorSet.Hash()); err != nil {
		err = saveValidatorSet(kv, genesisValidatorSet)
		if err != nil {
			panic(err)
		}
	}
	genesisID := genesisBlock.ID()

	if bestBlockID, _ := loadBestBlockID(kv); bytes.Equal(bestBlockID.Bytes(), (&types.Bytes32{}).Bytes()) {

		// no genesis yet
		raw, err := rlp.EncodeToBytes(genesisBlock)
		if err != nil {
			return nil, err
		}

		batch := kv.NewBatch()
		if err := saveBlockRaw(batch, genesisID, raw); err != nil {
			return nil, err
		}

		if err := batchSaveBestBlockID(batch, genesisID); err != nil {
			return nil, err
		}
		if err := saveBlockHash(batch, 0, genesisID); err != nil {
			return nil, err
		}

		if err := batch.Write(); err != nil {
			return nil, err
		}

		bestBlock = genesisBlock
		bestHeightGauge.Set(float64(bestBlock.Number()))
	} else {
		existGenesisID, err := loadBlockHash(kv, 0)
		if err != nil {
			return nil, err
		}
		if existGenesisID != genesisID {
			return nil, errors.New("genesis mismatch")
		}
		raw, err := loadBlockRaw(kv, bestBlockID)
		if err != nil {
			return nil, err
		}
		bestBlock, err = (&rawBlock{raw: raw}).Block()
		if err != nil {
			return nil, err
		}
		if bestBlock.Number() == 0 && bestBlock.QC == nil {
			logger.Info("QC of best block is empty, set it to genesis QC")
			saveBestQC(kv, block.GenesisEscortQC(bestBlock))
		}

		if bestBlock.IsSBlock() {
			logger.Info("Start fixing because best block is SBlock")
			lastBestBlock := bestBlock
			for bestBlock.IsSBlock() {
				// Error happend
				logger.Info("Load best block parent: ", bestBlock.ParentID())
				rawParent, err := loadBlockRaw(kv, bestBlock.ParentID())
				if err != nil {
					return nil, err
				}
				lastBestBlock = bestBlock
				bestBlock, _ = (&rawBlock{raw: rawParent}).Block()
			}
			logger.Info("save best qc", "blk", lastBestBlock.Number(), "qc", lastBestBlock.QC)
			saveBestQC(kv, lastBestBlock.QC)
			logger.Info("save best block", "num", bestBlock.Number(), "id", bestBlock.ID())
			saveBestBlockID(kv, bestBlock.ID())
		}

	}

	rawBlocksCache := newCache(blockCacheLimit, func(key interface{}) (interface{}, error) {
		raw, err := loadBlockRaw(kv, key.(types.Bytes32))
		if err != nil {
			return nil, err
		}
		if raw == nil {
			return nil, ErrNotFound
		}
		return &rawBlock{raw: raw}, nil
	})

	bestQC, err := loadBestQC(kv)
	if err != nil {
		logger.Debug("BestQC is empty, set it to use genesisEscortQC")
		bestQC = block.GenesisEscortQC(genesisBlock)
		bestQCHeightGauge.Set(float64(bestQC.QCHeight))
	}

	if bestBlock.Number() > bestQC.QCHeight {
		logger.Warn("best block > best QC, start to correct best block", "bestBlock", bestBlock.Number(), "bestQC", bestQC.QCHeight)
		matchBestBlockID, err := loadBlockHash(kv, bestQC.QCHeight)
		if err != nil {
			logger.Error("could not load match best block", "err", err)
		} else {
			matchBestBlockRaw, err := loadBlockRaw(kv, matchBestBlockID)
			if err != nil {
				logger.Error("could not load raw for bestBlockBeforeFlattern", "err", err)
			} else {
				bestBlock, _ = (&rawBlock{raw: matchBestBlockRaw}).Block()
				saveBestBlockID(kv, matchBestBlockID)
			}
		}
	}

	bestHeightGauge.Set(float64(bestBlock.Number()))
	bestQCHeightGauge.Set(float64(bestQC.QCHeight))

	if verbose {
		slog.Info("METER CHAIN INITIALIZED", "genesis: ", genesisBlock.ID(), "best", bestBlock.CompactString(), "bestQC", bestQC.String())
	}
	c := &Chain{
		kv:           kv,
		genesisBlock: genesisBlock,
		bestBlock:    bestBlock,
		bestQC:       bestQC,
		tag:          genesisBlock.ID()[31],
		caches: caches{
			rawBlocks: rawBlocksCache,
		},
		logger: logger,
	}

	c.proposalMap = NewProposalMap(c)
	return c, nil
}

// Tag returns chain tag, which is the last byte of genesis id.
func (c *Chain) Tag() byte {
	return c.tag
}

// GenesisBlock returns genesis block.
func (c *Chain) GenesisBlock() *block.Block {
	return c.genesisBlock
}

func (c *Chain) BestBlockBeforeIndexFlattern() *block.Block {
	c.rw.Lock()
	defer c.rw.Unlock()
	return c.bestBlockBeforeIndexFlattern
}

// BestBlock returns the newest block on trunk.
func (c *Chain) BestBlock() *block.Block {
	c.rw.RLock()
	defer c.rw.RUnlock()
	return c.bestBlock
}

func (c *Chain) BestPowNonce() uint64 {
	c.rw.RLock()
	defer c.rw.RUnlock()
	return c.bestPowNonce
}

func (c *Chain) BestKBlock() (*block.Block, error) {
	c.rw.RLock()
	defer c.rw.RUnlock()
	if c.bestBlock.IsKBlock() {
		return c.bestBlock, nil
	} else {
		lastKblockHeight := c.bestBlock.LastKBlockHeight()
		id, err := loadBlockHash(c.kv, lastKblockHeight)
		if err != nil {
			return nil, err
		}
		return c.getBlock(id)
	}
}

func (c *Chain) BestQC() *block.QuorumCert {
	c.rw.RLock()
	defer c.rw.RUnlock()
	return c.bestQC
}

// AddBlock add a new block into block chain.
// Once reorg happened (len(Trunk) > 0 && len(Branch) >0), Fork.Branch will be the chain transitted from trunk to branch.
// Reorg happens when isTrunk is true.
func (c *Chain) AddBlock(newBlock *block.Block, escortQC *block.QuorumCert) (*Fork, error) {
	c.rw.Lock()
	defer c.rw.Unlock()

	newBlockID := newBlock.ID()

	if header, err := c.getBlockHeader(newBlockID); err != nil {
		if !c.IsNotFound(err) {
			return nil, err
		}
	} else {
		parentFinalized := c.IsBlockFinalized(header.ParentID)

		// block already there
		newHeader := newBlock.Header()
		if header.Number() == newHeader.Number() &&
			header.ParentID == newHeader.ParentID &&
			string(header.Signature) == string(newHeader.Signature) &&
			header.Timestamp == newHeader.Timestamp &&
			parentFinalized {
			// if the current block is the finalized version of saved block, update it accordingly
			// do nothing
			selfFinalized := c.IsBlockFinalized(newHeader.ID())
			if selfFinalized {
				// if the new block has already been finalized, return directly
				return nil, ErrBlockExist
			}
		} else {
			return nil, ErrBlockExist
		}
	}

	// newBlock.Header().Finalized = finalize
	parent, err := c.getBlockHeader(newBlock.Header().ParentID)
	if err != nil {
		if c.IsNotFound(err) {
			return nil, errors.New("parent missing")
		}
		return nil, err
	}

	// finalized block need to have a finalized parent block
	raw := block.BlockEncodeBytes(newBlock)

	batch := c.kv.NewBatch()

	if err := saveBlockRaw(batch, newBlockID, raw); err != nil {
		return nil, err
	}

	if err := saveBlockHash(batch, newBlock.Number(), newBlockID); err != nil {
		return nil, err
	}

	for i, tx := range newBlock.Transactions() {
		c.logger.Debug(fmt.Sprintf("saving tx meta for %s", tx.Hash()), "block", newBlock.Number())
		meta, err := loadTxMeta(c.kv, tx.Hash())
		if err != nil {
			if !c.IsNotFound(err) {
				return nil, err
			}
		}
		meta = append(meta, TxMeta{
			BlockID: newBlockID,
			Index:   uint64(i),
		})
		if err := saveTxMeta(batch, tx.Hash(), meta); err != nil {
			return nil, err
		}
	}

	var fork *Fork
	isTrunk := c.isTrunk(newBlock.Header())
	// c.logger.Info("isTrunk", "blk", newBlock.Number(), "isTrunk", isTrunk)
	if isTrunk {
		if fork, err = c.buildFork(newBlock.Header(), c.bestBlock.Header()); err != nil {
			return nil, err
		}

		if err := batchSaveBestBlockID(batch, newBlockID); err != nil {
			return nil, err
		}
		c.bestBlock = newBlock
		bestHeightGauge.Set(float64(c.bestBlock.Number()))
		c.logger.Debug("saved best block", "blk", newBlock.ID())

		if escortQC == nil {
			return nil, errors.New("escort QC is nil")
		}
		err = batchSaveBestQC(batch, escortQC)
		if err != nil {
			fmt.Println("Error during update QC: ", err)
		}
		c.logger.Debug("saved best qc")
		c.bestQC = escortQC

		if newBlock.IsKBlock() {
			if err != nil {
				fmt.Println("Error during update pow nonce:", err)
			}
			c.logger.Info("saved best pow nonce", "powNonce", newBlock.Nonce())
		}

	} else {
		fork = &Fork{Ancestor: parent, Branch: []*block.Header{newBlock.Header()}}
	}

	if err := batch.Write(); err != nil {
		return nil, err
	}

	c.caches.rawBlocks.Add(newBlockID, newRawBlock(raw, newBlock))

	c.tick.Broadcast()
	return fork, nil
}

func (c *Chain) IsBlockFinalized(id types.Bytes32) bool {
	return block.Number(id) <= c.bestBlock.Number()
}

// GetBlockHeader get block header by block id.
func (c *Chain) GetBlockHeader(id types.Bytes32) (*block.Header, error) {
	c.rw.RLock()
	defer c.rw.RUnlock()
	return c.getBlockHeader(id)
}

// GetBlockBody get block body by block id.
func (c *Chain) GetBlockBody(id types.Bytes32) (*block.Body, error) {
	c.rw.RLock()
	defer c.rw.RUnlock()
	return c.getBlockBody(id)
}

// GetBlock get block by id.
func (c *Chain) GetBlock(id types.Bytes32) (*block.Block, error) {
	c.rw.RLock()
	defer c.rw.RUnlock()
	return c.getBlock(id)
}

// GetBlockRaw get block rlp encoded bytes for given id.
// Never modify the returned raw block.
func (c *Chain) GetBlockRaw(id types.Bytes32) (block.Raw, error) {
	c.rw.RLock()
	defer c.rw.RUnlock()
	raw, err := c.getRawBlock(id)
	if err != nil {
		return nil, err
	}
	return raw.raw, nil
}

// GetAncestorBlockID get ancestor block ID of descendant for given ancestor block.
func (c *Chain) GetAncestorBlockID(descendantID types.Bytes32, ancestorNum uint32) (types.Bytes32, error) {
	c.rw.RLock()
	defer c.rw.RUnlock()
	return loadBlockHash(c.kv, ancestorNum)

}

// GetTransactionMeta get transaction meta info, on the chain defined by head block ID.
func (c *Chain) GetTransactionMeta(txID []byte, headBlockID types.Bytes32) (*TxMeta, error) {
	c.rw.RLock()
	defer c.rw.RUnlock()
	return c.getTransactionMeta(txID, headBlockID)
}

// GetTransactionMeta get transaction meta info, on the chain defined by head block ID.
func (c *Chain) HasTransactionMeta(txID []byte) (bool, error) {
	c.rw.RLock()
	defer c.rw.RUnlock()
	return c.hasTransactionMeta(txID)
}

// GetTransaction get transaction for given block and index.
func (c *Chain) GetTransaction(blockID types.Bytes32, index uint64) (cmttypes.Tx, error) {
	c.rw.RLock()
	defer c.rw.RUnlock()
	return c.getTransaction(blockID, index)
}

// GetTrunkBlockID get block id on trunk by given block number.
func (c *Chain) GetTrunkBlockID(num uint32) (types.Bytes32, error) {
	c.rw.RLock()
	defer c.rw.RUnlock()
	return loadBlockHash(c.kv, num)
}

// GetTrunkBlockHeader get block header on trunk by given block number.
func (c *Chain) GetTrunkBlockHeader(num uint32) (*block.Header, error) {
	c.rw.RLock()
	defer c.rw.RUnlock()
	id, err := c.GetTrunkBlockID(num)
	if err != nil {
		return nil, err
	}
	return c.getBlockHeader(id)
}

// GetTrunkBlock get block on trunk by given block number.
func (c *Chain) GetTrunkBlock(num uint32) (*block.Block, error) {
	c.rw.RLock()
	defer c.rw.RUnlock()
	id, err := c.GetTrunkBlockID(num)
	if err != nil {
		return nil, err
	}
	return c.getBlock(id)
}

// GetTrunkBlockRaw get block raw on trunk by given block number.
func (c *Chain) GetTrunkBlockRaw(num uint32) (block.Raw, error) {
	c.rw.RLock()
	defer c.rw.RUnlock()
	bestNum := c.bestBlock.Number()

	// limit trunk block to numbers less than or equal to best
	if num > bestNum {
		return []byte{}, errors.New("no trunk block beyond best")
	}

	id, err := c.GetTrunkBlockID(num)
	if err != nil {
		return nil, err
	}
	raw, err := c.getRawBlock(id)
	if err != nil {
		return nil, err
	}
	return raw.raw, nil
}

// GetTrunkTransactionMeta get transaction meta info on trunk by given tx id.
func (c *Chain) GetTrunkTransactionMeta(txID []byte) (*TxMeta, error) {
	c.rw.RLock()
	defer c.rw.RUnlock()
	return c.getTransactionMeta(txID, c.bestBlock.ID())
}

// GetTrunkTransaction get transaction on trunk by given tx id.
func (c *Chain) GetTrunkTransaction(txID []byte) (cmttypes.Tx, *TxMeta, error) {
	c.rw.RLock()
	defer c.rw.RUnlock()
	meta, err := c.getTransactionMeta(txID, c.bestBlock.ID())
	if err != nil {
		return nil, nil, err
	}
	tx, err := c.getTransaction(meta.BlockID, meta.Index)
	if err != nil {
		return nil, nil, err
	}
	return tx, meta, nil
}

// NewSeeker returns a new seeker instance.
func (c *Chain) NewSeeker(headBlockID types.Bytes32) *Seeker {
	return newSeeker(c, headBlockID)
}

func (c *Chain) isTrunk(header *block.Header) bool {
	bestHeader := c.bestBlock.Header()
	// fmt.Println(fmt.Sprintf("IsTrunk: header: %s, bestHeader: %s", header.ID().String(), bestHeader.ID().String()))
	if header.Number() < bestHeader.Number() {
		return false
	}
	if header.Number() > bestHeader.Number() {
		return true
	}

	// total scores are equal
	if bytes.Compare(header.ID().Bytes(), bestHeader.ID().Bytes()) < 0 {
		// smaller ID is preferred, since block with smaller ID usually has larger average score.
		// also, it's a deterministic decision.
		return true
	}
	return false
}

// Think about the example below:
//
//	B1--B2--B3--B4--B5--B6
//	          \
//	           \
//	            b4--b5
//
// When call buildFork(B6, b5), the return values will be:
// ((B3, [B4, B5, B6], [b4, b5]), nil)
func (c *Chain) buildFork(trunkHead *block.Header, branchHead *block.Header) (*Fork, error) {
	var (
		trunk, branch []*block.Header
		err           error
		b1            = trunkHead
		b2            = branchHead
	)

	for {
		if b1.Number() > b2.Number() {
			trunk = append(trunk, b1)
			if b1, err = c.getBlockHeader(b1.ParentID); err != nil {
				return nil, err
			}
			continue
		}
		if b1.Number() < b2.Number() {
			branch = append(branch, b2)
			if b2, err = c.getBlockHeader(b2.ParentID); err != nil {
				return nil, err
			}
			continue
		}
		if b1.ID() == b2.ID() {
			// reverse trunk and branch
			for i, j := 0, len(trunk)-1; i < j; i, j = i+1, j-1 {
				trunk[i], trunk[j] = trunk[j], trunk[i]
			}
			for i, j := 0, len(branch)-1; i < j; i, j = i+1, j-1 {
				branch[i], branch[j] = branch[j], branch[i]
			}
			return &Fork{b1, trunk, branch}, nil
		}

		trunk = append(trunk, b1)
		branch = append(branch, b2)

		if b1, err = c.getBlockHeader(b1.ParentID); err != nil {
			return nil, err
		}

		if b2, err = c.getBlockHeader(b2.ParentID); err != nil {
			return nil, err
		}
	}
}

func (c *Chain) getRawBlock(id types.Bytes32) (*rawBlock, error) {
	raw, err := c.caches.rawBlocks.GetOrLoad(id)
	if err != nil {
		return nil, err
	}

	return raw.(*rawBlock), nil
}

func (c *Chain) getBlockHeader(id types.Bytes32) (*block.Header, error) {
	raw, err := c.getRawBlock(id)
	if err != nil {
		return nil, err
	}
	return raw.Header()
}

func (c *Chain) getBlockBody(id types.Bytes32) (*block.Body, error) {
	raw, err := c.getRawBlock(id)
	if err != nil {
		return nil, err
	}
	return raw.Body()
}
func (c *Chain) getBlock(id types.Bytes32) (*block.Block, error) {
	raw, err := c.getRawBlock(id)
	if err != nil {
		return nil, err
	}
	return raw.Block()
}

func (c *Chain) hasTransactionMeta(txID []byte) (bool, error) {
	return c.kv.Has(txID[:])
}

func (c *Chain) getTransactionMeta(txID []byte, headBlockID types.Bytes32) (*TxMeta, error) {
	meta, err := loadTxMeta(c.kv, txID)
	if err != nil {
		return nil, err
	}
	if meta == nil {
		return nil, ErrNotFound
	}
	for _, m := range meta {
		ancestorID, err := loadBlockHash(c.kv, block.Number(m.BlockID))
		if err != nil {
			if c.IsNotFound(err) {
				continue
			}
			return nil, err
		}
		if ancestorID == m.BlockID {
			return &m, nil
		}
	}
	return nil, ErrNotFound
}

func (c *Chain) getTransaction(blockID types.Bytes32, index uint64) (cmttypes.Tx, error) {
	body, err := c.getBlockBody(blockID)
	if err != nil {
		return nil, err
	}
	if index >= uint64(len(body.Txs)) {
		return nil, errors.New("tx index out of range")
	}
	return body.Txs[index], nil
}

// IsNotFound returns if an error means not found.
func (c *Chain) IsNotFound(err error) bool {
	return err == ErrNotFound
}

// IsBlockExist returns if the error means block was already in the chain.
func (c *Chain) IsBlockExist(err error) bool {
	return err == ErrBlockExist
}

// NewTicker create a signal Waiter to receive event of head block change.
func (c *Chain) NewTicker() co.Waiter {
	return c.tick.NewWaiter()
}

// Block expanded block.Block to indicate whether it is obsolete
type Block struct {
	*block.Block
	Obsolete bool
}

// BlockReader defines the interface to read Block
type BlockReader interface {
	Read() ([]*Block, error)
}

type readBlock func() ([]*Block, error)

func (r readBlock) Read() ([]*Block, error) {
	return r()
}

// NewBlockReader generate an object that implements the BlockReader interface
func (c *Chain) NewBlockReader(position types.Bytes32) BlockReader {
	return readBlock(func() ([]*Block, error) {
		c.rw.RLock()
		defer c.rw.RUnlock()

		bestID := c.bestBlock.ID()
		if bestID == position {
			return nil, nil
		}

		var blocks []*Block
		for {
			positionBlock, err := c.getBlock(position)
			if err != nil {
				return nil, err
			}

			if block.Number(position) > block.Number(bestID) {
				blocks = append(blocks, &Block{positionBlock, true})
				position = positionBlock.ParentID()
				continue
			}

			ancestor, err := loadBlockHash(c.kv, block.Number(position))
			// ancestor, err := c.ancestorTrie.GetAncestor(bestID, block.Number(position))
			if err != nil {
				return nil, err
			}

			if position == ancestor {
				next, err := c.nextBlock(bestID, block.Number(position))
				if err != nil {
					return nil, err
				}
				position = next.ID()
				return append(blocks, &Block{next, false}), nil
			}

			blocks = append(blocks, &Block{positionBlock, true})
			position = positionBlock.ParentID()
		}
	})
}

func (c *Chain) nextBlock(descendantID types.Bytes32, num uint32) (*block.Block, error) {
	next, err := loadBlockHash(c.kv, num+1)
	if err != nil {
		return nil, err
	}

	return c.getBlock(next)
}

func (c *Chain) FindEpochOnBlock(num uint32) (uint64, error) {
	bestBlock := c.BestBlock()
	curEpoch := bestBlock.QC.EpochID
	curNum := bestBlock.Number()

	if num >= curNum {
		return curEpoch, nil
	}

	b, err := c.GetTrunkBlock(num)
	if err != nil {
		return 0, err
	}
	return b.GetBlockEpoch(), nil
}

func (c *Chain) AddDraft(b *block.DraftBlock) {
	c.drw.Lock()
	defer c.drw.Unlock()
	c.proposalMap.Add(b)
}

func (c *Chain) HasDraft(blkID types.Bytes32) bool {
	c.drw.RLock()
	defer c.drw.RUnlock()
	return c.proposalMap.Has(blkID)
}

func (c *Chain) GetDraft(blkID types.Bytes32) *block.DraftBlock {
	c.drw.RLock()
	defer c.drw.RUnlock()
	return c.proposalMap.Get(blkID)
}

func (c *Chain) GetDraftByNum(num uint32) *block.DraftBlock {
	c.drw.RLock()
	defer c.drw.RUnlock()
	proposals := c.proposalMap.GetDraftByNum(num)
	if len(proposals) > 0 {
		latest := proposals[0]
		for _, prop := range proposals[1:] {
			if prop.Round > latest.Round {
				latest = prop
			}
		}
		return latest
	}
	return nil
}

func (c *Chain) GetDraftByEscortQC(qc *block.QuorumCert) *block.DraftBlock {
	c.drw.RLock()
	defer c.drw.RUnlock()
	return c.proposalMap.GetOneByEscortQC(qc)
}

func (c *Chain) DraftLen() int {
	c.drw.RLock()
	defer c.drw.RUnlock()
	if c.proposalMap != nil {
		return c.proposalMap.Len()
	}
	return 0
}

func (c *Chain) PruneDraftsUpTo(lastCommitted *block.DraftBlock) {
	c.drw.Lock()
	defer c.drw.Unlock()
	c.logger.Debug("start to prune drafts up to", "lastCommitted", lastCommitted.ProposedBlock.Number(), "draftSize", c.proposalMap.Len())
	c.proposalMap.PruneUpTo(lastCommitted)
	c.logger.Debug("ended prune drafts")
}

func (c *Chain) GetDraftsUpTo(commitedBlkID types.Bytes32, qcHigh *block.QuorumCert) []*block.DraftBlock {
	c.drw.RLock()
	defer c.drw.RUnlock()
	return c.proposalMap.GetProposalsUpTo(commitedBlkID, qcHigh)
}

func (c *Chain) RawBlocksCacheLen() int {
	return c.caches.rawBlocks.Len()
}

func (c *Chain) GetBestValidatorSet() *types.ValidatorSet {
	vset, err := loadValidatorSet(c.kv, c.bestBlock.ValidatorHash())
	if err != nil {
		return nil
	}
	return vset
}

func (c *Chain) GetValidatorSet(num uint32) *types.ValidatorSet {
	hash, err := loadBlockHash(c.kv, num)
	if err != nil {
		return nil
	}
	blk, err := c.getBlock(hash)
	if err != nil {
		return nil
	}
	vset, err := loadValidatorSet(c.kv, blk.ValidatorHash())
	if err != nil {
		return nil
	}
	return vset
}

func (c *Chain) SaveValidatorSet(vset *types.ValidatorSet) {
	err := saveValidatorSet(c.kv, vset)
	if err != nil {
		panic(err)
	}
}

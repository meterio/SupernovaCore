package chain

import (
	"log/slog"

	"github.com/meterio/supernova/block"
	"github.com/meterio/supernova/types"
)

type ProposalMap struct {
	proposals map[types.Bytes32]*block.DraftBlock
	chain     *Chain
	logger    slog.Logger
}

func NewProposalMap(c *Chain) *ProposalMap {
	return &ProposalMap{
		proposals: make(map[types.Bytes32]*block.DraftBlock),
		chain:     c,
		logger:    *slog.With("pkg", "pmap"),
	}
}

func (p *ProposalMap) Add(blk *block.DraftBlock) {
	p.proposals[blk.ProposedBlock.ID()] = blk
}

func (p *ProposalMap) GetProposalsUpTo(committedBlkID types.Bytes32, qcHigh *block.QuorumCert) []*block.DraftBlock {
	commited := p.Get(committedBlkID)
	head := p.GetOneByEscortQC(qcHigh)
	result := make([]*block.DraftBlock, 0)
	if commited == nil || head == nil {
		return result
	}

	for i := 0; i < 4; i++ {
		if head == nil || head.Committed {
			break
		}
		if commited.ProposedBlock.ID().Equal(head.ProposedBlock.ID()) {
			return result
		}
		result = append([]*block.DraftBlock{head}, result...)
		head = head.Parent
	}
	return result
}

func (p *ProposalMap) Has(blkID types.Bytes32) bool {
	blk, ok := p.proposals[blkID]
	if ok && blk != nil {
		return true
	}
	return false
}

func (p *ProposalMap) Get(blkID types.Bytes32) *block.DraftBlock {
	blk, ok := p.proposals[blkID]
	if ok {
		return blk
	}

	// load from database
	blkInDB, err := p.chain.GetBlock(blkID)
	if err == nil {
		p.logger.Info("load block from DB", "num", blkInDB.Number(), "id", blkInDB.CompactString())
		return &block.DraftBlock{
			Height:        blkInDB.Number(),
			Round:         blkInDB.QC.Round + 1, // FIXME: might be wrong for the block after kblock
			Parent:        nil,
			Justify:       nil,
			Committed:     true,
			ProposedBlock: blkInDB,
		}
	}
	return nil
}

// qc is for that block?
// blk is derived from DraftBlock message. pass it in if already decoded
func BlockMatchDraftQC(b *block.DraftBlock, escortQC *block.QuorumCert) bool {

	if b == nil {
		// decode block to get qc
		// fmt.Println("can not decode block", err)
		return false
	}

	blk := b.ProposedBlock

	return blk.ID() == escortQC.BlockID
}

func (p *ProposalMap) GetOneByEscortQC(qc *block.QuorumCert) *block.DraftBlock {
	for key := range p.proposals {
		draftBlk := p.proposals[key]
		if draftBlk.ProposedBlock.ID() == qc.BlockID && draftBlk.Round == qc.Round {
			if match := BlockMatchDraftQC(draftBlk, qc); match {
				return draftBlk
			}
		}
	}

	// load from database

	blkInDB, err := p.chain.GetBlock(qc.BlockID)
	if err == nil {
		p.logger.Debug("load block from DB", "num", blkInDB.Number(), "id", blkInDB.CompactString())
		return &block.DraftBlock{
			Height:        qc.Number(),
			Round:         qc.Round,
			Parent:        nil,
			Justify:       nil,
			Committed:     true,
			ProposedBlock: blkInDB,
		}
	}
	return nil
}

func (p *ProposalMap) Len() int {
	return len(p.proposals)
}

func (p *ProposalMap) CleanAll() {
	for key := range p.proposals {
		delete(p.proposals, key)
	}
}

func (p *ProposalMap) PruneUpTo(lastCommitted *block.DraftBlock) {
	for k := range p.proposals {
		draftBlk := p.proposals[k]
		if draftBlk.ProposedBlock.Number() < lastCommitted.Height {
			delete(p.proposals, draftBlk.ProposedBlock.ID())
		}
		if draftBlk.ProposedBlock.Number() == lastCommitted.Height {
			// clean up not-finalized block
			// return tx to txpool
			if !draftBlk.ProposedBlock.ID().Equal(lastCommitted.ProposedBlock.ID()) {

				// only prune state trie if it's not the same as the committed one
				// if !draftBlk.ProposedBlock.StateRoot().Equal(lastCommitted.ProposedBlock.StateRoot()) {
				// draftBlk.Stage.Revert()
				// }

				// delete from proposal map
				delete(p.proposals, draftBlk.ProposedBlock.ID())
			} else {
				draftBlk.Committed = true
			}
		}
	}
}

func (p *ProposalMap) GetDraftsByNum(num uint32) []*block.DraftBlock {
	result := make([]*block.DraftBlock, 0)
	for _, prop := range p.proposals {
		if prop.ProposedBlock.Number() == num {
			result = append(result, prop)
		}
	}
	return result
}

func (p *ProposalMap) GetDraftsByParentID(parentID types.Bytes32) []*block.DraftBlock {
	result := make([]*block.DraftBlock, 0)
	for _, prop := range p.proposals {
		if prop.ProposedBlock.ParentID() == parentID {
			result = append(result, prop)
		}
	}
	return result
}

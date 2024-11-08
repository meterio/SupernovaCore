package consensus

// This is part of pacemaker that in charge of:
// 1. propose blocks
// 2. pack QC and CommitteeInfo into bloks
// 3. collect votes and generate new QC

import (
	"errors"

	"github.com/meterio/supernova/block"
	"github.com/meterio/supernova/types"
)

var (
	ErrParentBlockEmpty     = errors.New("parent block empty")
	ErrPackerEmpty          = errors.New("packer is empty")
	ErrFlowEmpty            = errors.New("flow is empty")
	ErrProposalEmpty        = errors.New("proposal is empty")
	ErrStateCreaterNotReady = errors.New("state creater not ready")
	ErrInvalidRound         = errors.New("invalid round")
)

// Build MBlock
func (p *Pacemaker) buildBlock(timestamp uint64, parent *block.DraftBlock, justify *block.DraftQC, round uint32, nonce uint64, txs types.Transactions) (error, *block.DraftBlock) {
	parentBlock := parent.ProposedBlock
	qc := justify.QC

	// qc.Epoch = p.epochState.epoch

	lastKBlock := uint32(0)
	if parent.ProposedBlock.IsKBlock() {
		lastKBlock = parent.ProposedBlock.Number()
	} else {
		lastKBlock = parent.ProposedBlock.LastKBlock()
	}

	nextValidatorHash := parent.ProposedBlock.NextValidatorsHash()
	num := parent.ProposedBlock.Number() + 1
	newVSet := p.validatorSetRegistry.GetNext(num)
	if newVSet != nil {
		nextValidatorHash = newVSet.Hash()
	}
	builder := new(block.Builder).
		ParentID(parentBlock.ID()).
		Timestamp(timestamp).
		Nonce(nonce).
		ValidatorsHash(parent.ProposedBlock.NextValidatorsHash()).
		NextValidatorsHash(nextValidatorHash).
		ProposerIndex(uint32(p.epochState.CommitteeIndex())).
		LastKBlock(lastKBlock).QC(qc)

	for _, tx := range txs {
		builder.Tx(tx)
	}

	newBlock := builder.Build()

	proposed := &block.DraftBlock{
		Height:        newBlock.Number(),
		Round:         round,
		Parent:        parent,
		Justify:       justify,
		ProposedBlock: newBlock,

		SuccessProcessed: true,
		ProcessError:     nil,
	}

	return nil, proposed
}

// Build MBlock
func (p *Pacemaker) AddTxToCurProposal(newTxID []byte) error {

	// if p.curProposal == nil {
	// 	return ErrProposalEmpty
	// }
	// p.logger.Info("add tx to cur proposal", "tx", newTxID, "proposed", p.curProposal.ProposedBlock.CompactString())
	// parentBlock := p.curProposal.Parent.ProposedBlock
	// //create checkPoint before build block

	// // collect all the txs in cache
	// txsInCache := make(map[string]bool)
	// tmp := p.curProposal.Parent
	// for tmp != nil && !tmp.Committed {
	// 	for _, knownTx := range tmp.ProposedBlock.Transactions() {
	// 		txsInCache[knowntx.Hash().String()] = true
	// 	}
	// 	tmp = p.chain.GetDraft(tmp.ProposedBlock.ParentID())
	// }

	// id := newTxID
	// // prevent to include txs already in previous drafts
	// if _, existed := txsInCache[id.String()]; existed {
	// 	return errors.New("tx already in cache")
	// }
	// txObj := p.reactor.txpool.GetTxObj(id)
	// if txObj == nil {
	// 	p.logger.Error("tx obj is nil", "id", id)
	// 	return errors.New("tx obj is nil")
	// }
	// executable, err := txObj.Executable(p.chain, parentBlock.BlockHeader)
	// if err != nil || !executable {
	// 	p.logger.Warn(fmt.Sprintf("tx %s not executable", id), "err", err)
	// 	return err
	// }
	// tx := txObj.Transaction

	// p.logger.Debug("added tx to cur proposal", "tx", newTxID)
	return nil

	// // FIXME: implement this

}

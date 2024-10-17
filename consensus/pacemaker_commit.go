package consensus

import (
	"fmt"
	"time"

	v1 "github.com/cometbft/cometbft/api/cometbft/abci/v1"
	"github.com/meterio/supernova/block"
	"github.com/meterio/supernova/chain"
	"github.com/meterio/supernova/types"
)

func (p *Pacemaker) FinalizeBlockViaABCI(blk *block.Block) error {
	txs := make([][]byte, 0)
	for _, tx := range blk.Txs {
		txs = append(txs, tx)
	}
	res, err := p.executor.FinalizeBlock(&v1.FinalizeBlockRequest{Txs: txs, Height: int64(blk.Number()), Hash: blk.ID().Bytes()})
	// res.AppHash
	// res.TxResults
	err = p.validatorSetRegistry.Update(blk.Number(), p.epochState.committee, res.ValidatorUpdates)
	if err != nil {
		p.logger.Warn("could not update vset registry", "err", err)
		return err
	}
	p.executor.Commit()
	//stage := blkInfo.Stage

	return nil
}

// finalize the block with its own QC
func (p *Pacemaker) commitBlock(draftBlk *block.DraftBlock, escortQC *block.QuorumCert) error {

	start := time.Now()
	blk := draftBlk.ProposedBlock
	p.logger.Debug("Try to finalize block", "block", blk.Oneliner())

	// fmt.Println("Calling AddBlock from consensus_block.commitBlock, newBlock=", blk.ID())
	if blk.Number() <= p.chain.BestBlock().Number() {
		return errKnownBlock
	}
	fork, err := p.chain.AddBlock(blk, escortQC)
	if err != nil {
		if err == chain.ErrBlockExist {
			p.logger.Info("block already exist", "id", blk.ID(), "num", blk.Number())
		} else {
			p.logger.Warn("add block failed ...", "err", err, "id", blk.ID(), "num", blk.Number())
		}
		return err
	}

	err = p.FinalizeBlockViaABCI(draftBlk.ProposedBlock)
	if err != nil {
		p.logger.Warn("could not finalize via ABCI", "err", err)
		return err
	}

	// unlike processBlock, we do not need to handle fork
	if fork != nil {
		//panic(" chain is in forked state, something wrong")
		//return false
		// process fork????
		if len(fork.Branch) > 0 {
			out := fmt.Sprintf("Fork Happened ... fork(Ancestor=%s, Branch=%s), bestBlock=%s", fork.Ancestor.ID().String(), fork.Branch[0].ID().String(), p.chain.BestBlock().ID().String())
			p.logger.Warn(out)
			panic(out)
		}
	}

	p.logger.Info(fmt.Sprintf("* committed %v", blk.CompactString()), "txs", len(blk.Txs), "epoch", blk.Epoch(), "elapsed", types.PrettyDuration(time.Since(start)))

	// broadcast the new block to all peers
	p.communicator.BroadcastBlock(&block.EscortedBlock{Block: blk, EscortQC: escortQC})
	// successfully added the block, update the current hight of consensus

	if draftBlk.ProposedBlock.IsKBlock() {
		p.logger.Info("committed a KBlock, schedule regulate now", "blk", draftBlk.ProposedBlock.ID().ToBlockShortID())
		p.ScheduleRegulate()
	}
	return nil
}

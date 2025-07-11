package consensus

import (
	"fmt"
	"time"

	"github.com/meterio/supernova/block"
	"github.com/meterio/supernova/chain"
	"github.com/meterio/supernova/types"
)

// finalize the block with its own QC
func (p *Pacemaker) CommitBlock(blk *block.Block, escortQC *block.QuorumCert) error {

	start := time.Now()
	p.logger.Debug("try to finalize block", "block", blk.Oneliner())

	// fmt.Println("Calling AddBlock from consensus_block.commitBlock, newBlock=", blk.ID())
	if blk.Number() <= p.chain.BestBlock().Number() {
		return errKnownBlock
	}

	appHash, nxtVSet, err := p.executor.ApplyBlock(blk, int64(blk.Number())) // TODO: syncingToHeight might need adjustment
	if err != nil {
		return err
	}
	blk.BlockHeader.AppHash = appHash

	if nxtVSet != nil {
		p.addedValidators = CalcAddedValidators(p.epochState.committee, nxtVSet)
		p.nextEpochState, err = NewPendingEpochState(nxtVSet, p.blsMaster.PubKey, p.epochState.epoch)
		if err != nil {
			p.logger.Error("could not calc pending epoch state", "err", err)
			return err
		}
		p.validatorSetRegistry.registerNewValidatorSet(blk.Number(), p.epochState.committee, nxtVSet)
		p.logger.Info("next epoch state", "incommittee", p.nextEpochState.inCommittee, "epoch", p.nextEpochState.epoch)
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

	// unlike processBlock, we do not need to handle fork
	if fork != nil {
		// process fork????
		if len(fork.Branch) > 0 {
			out := fmt.Sprintf("Fork Happened ... fork(Ancestor=%s, Branch=%s), bestBlock=%s", fork.Ancestor.ID().String(), fork.Branch[0].ID().String(), p.chain.BestBlock().ID().String())
			p.logger.Warn(out)
			p.printFork(fork)
			p.scheduleRegulate()
			return ErrForkHappened
		}
	}

	p.logger.Info(fmt.Sprintf("* committed %v", blk.CompactString()), "txs", len(blk.Txs), "epoch", blk.Epoch(), "elapsed", types.PrettyDuration(time.Since(start)))

	p.lastCommitted = blk
	// broadcast the new block to all peers
	// p.communicator.BroadcastBlock(&block.EscortedBlock{Block: blk, EscortQC: escortQC})
	// successfully added the block, update the current hight of consensus

	p.logger.Info("Check kblock")
	if blk.IsKBlock() {
		p.logger.Info("committed a KBlock, schedule regulate now", "blk", blk.ID().ToBlockShortID())
		p.scheduleRegulate()
	}
	p.logger.Info("Prepare to encode block")

	start = time.Now()
	p.communicator.BroadcastBlock(&block.EscortedBlock{Block: blk, EscortQC: escortQC})
	p.logger.Info("broadcast elapsed", "elapsed", time.Since(start))

	return nil
}

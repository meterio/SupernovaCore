package consensus

import (
	"context"
	"fmt"
	"time"

	v1 "github.com/cometbft/cometbft/api/cometbft/abci/v1"
	"github.com/meterio/meter-pov/block"
	"github.com/meterio/meter-pov/chain"
	"github.com/meterio/meter-pov/meter"
)

// finalize the block with its own QC
func (p *Pacemaker) commitBlock(draftBlk *block.DraftBlock, escortQC *block.QuorumCert) error {

	start := time.Now()
	blk := draftBlk.ProposedBlock
	p.logger.Debug("Try to finalize block", "block", blk.Oneliner())

	txs := make([][]byte, 0)
	for _, tx := range blk.Txs {
		txs = append(txs, tx)
	}
	p.reactor.proxyApp.Consensus().FinalizeBlock(context.TODO(), &v1.FinalizeBlockRequest{Txs: txs, Height: int64(draftBlk.Height), Hash: blk.ID().Bytes()})
	p.reactor.proxyApp.Consensus().Commit(context.TODO())
	//stage := blkInfo.Stage

	// fmt.Println("Calling AddBlock from consensus_block.commitBlock, newBlock=", blk.ID())
	if blk.Number() <= p.reactor.chain.BestBlock().Number() {
		return errKnownBlock
	}
	fork, err := p.reactor.chain.AddBlock(blk, escortQC)
	if err != nil {
		if err != chain.ErrBlockExist {
			p.logger.Warn("add block failed ...", "err", err, "id", blk.ID(), "num", blk.Number())
		} else {
			p.logger.Info("block already exist", "id", blk.ID(), "num", blk.Number())
		}
		return err
	}

	// unlike processBlock, we do not need to handle fork
	if fork != nil {
		//panic(" chain is in forked state, something wrong")
		//return false
		// process fork????
		if len(fork.Branch) > 0 {
			out := fmt.Sprintf("Fork Happened ... fork(Ancestor=%s, Branch=%s), bestBlock=%s", fork.Ancestor.ID().String(), fork.Branch[0].ID().String(), p.reactor.chain.BestBlock().ID().String())
			p.logger.Warn(out)
			panic(out)
		}
	}

	p.logger.Info(fmt.Sprintf("* committed %v", blk.ShortID()), "txs", len(blk.Txs), "epoch", blk.GetBlockEpoch(), "elapsed", meter.PrettyDuration(time.Since(start)))

	// broadcast the new block to all peers
	p.reactor.comm.BroadcastBlock(&block.EscortedBlock{Block: blk, EscortQC: escortQC})
	// successfully added the block, update the current hight of consensus
	return nil
}

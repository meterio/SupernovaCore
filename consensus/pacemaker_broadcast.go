package consensus

import (
	"fmt"
	"time"

	"github.com/meterio/supernova/block"
	"github.com/meterio/supernova/types"
)

func (p *Pacemaker) scheduleBroadcast(proposalMsg *block.PMProposalMessage, d time.Duration) {
	scheduleFunc := func() {
	CleanBroadcastCh:
		for {
			select {
			case <-p.broadcastCh:
			default:
				break CleanBroadcastCh
			}
		}
		p.broadcastCh <- proposalMsg
	}

	p.cancelAllPendingBroadcast()
	// p.lastVotingHeight = block.Number(voteMsg.VoteBlockID)
	// p.lastVoteMsg = voteMsg
	blk := proposalMsg.DecodeBlock()
	if d <= 0 || d >= BroadcastTimeLimit {
		p.logger.Info(fmt.Sprintf("schedule broadcast for %s(E:%d) with no delay", blk.CompactString(), proposalMsg.GetEpoch()))
		scheduleFunc()
	} else {
		p.logger.Info(fmt.Sprintf("schedule broadcast for %s(E:%d) after %s", blk.CompactString(), proposalMsg.GetEpoch(), types.PrettyDuration(d)))
		p.broadcastTimer = time.AfterFunc(d, scheduleFunc)
	}
}

func (p *Pacemaker) cancelAllPendingBroadcast() {
	p.logger.Debug("cancel all pending broadcast")
	// stop voteTimer if not nil
	if p.broadcastTimer != nil {
		p.broadcastTimer.Stop()
	}
	// clean msg from voteCh
CleanBroadcastCh:
	for {
		select {
		case <-p.broadcastCh:
		default:
			break CleanBroadcastCh
		}
	}
}

func (p *Pacemaker) OnBroadcastProposal() {
	if p.curProposal == nil {
		p.logger.Warn("proposal is empty, skip broadcasting ...")
		return
	}
	proposalMsg := p.curProposal.Msg.(*block.PMProposalMessage)
	if proposalMsg == nil {
		p.logger.Warn("empty proposal message")
		return
	}
	blk := proposalMsg.DecodeBlock()

	if proposalMsg.Epoch < p.epochState.epoch {
		p.logger.Info(fmt.Sprintf("proposal epoch %d < curEpoch %d , skip broadcast ...", blk.Epoch(), p.epochState.epoch))
		return
	}

	p.Broadcast(proposalMsg)
}

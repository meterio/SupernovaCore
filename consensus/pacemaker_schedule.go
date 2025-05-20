package consensus

import (
	"fmt"
	"time"

	"github.com/meterio/supernova/block"
	"github.com/meterio/supernova/types"
)

func drainChannel[T any](ch chan T) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

func (p *Pacemaker) scheduleBroadcast(proposalMsg *block.PMProposalMessage, d time.Duration) {
	scheduleFunc := func() {
		drainChannel(p.broadcastCh)
		p.broadcastCh <- proposalMsg
	}

	blk := proposalMsg.DecodeBlock()
	if d <= 0 || d >= BroadcastTimeLimit {
		p.logger.Info(fmt.Sprintf("schedule broadcast for %s(E:%d) with no delay", blk.CompactString(), proposalMsg.GetEpoch()))
		scheduleFunc()
	} else {
		p.logger.Info(fmt.Sprintf("schedule broadcast for %s(E:%d) after %s", blk.CompactString(), proposalMsg.GetEpoch(), types.PrettyDuration(d)))
		p.broadcastTimer = time.AfterFunc(d, scheduleFunc)
	}
}

func (p *Pacemaker) broadcastCurProposal() {
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

func (p *Pacemaker) scheduleRegulate() {
	// schedule Regulate
	// make sure this Regulate cmd is the very next cmd
	drainChannel(p.cmdCh)
	p.cmdCh <- PMCmdRegulate
	p.logger.Info("regulate scheduled")
}

func (p *Pacemaker) scheduleOnBeat(epoch uint64, round uint32) {
	// p.enterRound(round, IncRoundOnBeat)
	drainChannel(p.beatCh)
	p.beatCh <- PMBeatInfo{epoch, round}
}

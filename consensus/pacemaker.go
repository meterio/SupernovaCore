// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package consensus

import (
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	v1 "github.com/cometbft/cometbft/api/cometbft/abci/v1"
	cmtproxy "github.com/cometbft/cometbft/proxy"
	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/meterio/supernova/block"
	"github.com/meterio/supernova/chain"
	"github.com/meterio/supernova/libs/comm"
	"github.com/meterio/supernova/txpool"
	"github.com/meterio/supernova/types"
)

const (
	RoundInterval        = 2 * time.Second
	RoundTimeoutInterval = RoundInterval * 4 // round timeout 8 secs.
	ProposeTimeLimit     = 1300 * time.Millisecond
	BroadcastTimeLimit   = 1400 * time.Millisecond
)

type Pacemaker struct {
	reactor      *Reactor //global reactor info
	version      string
	chain        *chain.Chain
	blsMaster    *types.BlsMaster
	logger       *slog.Logger
	executor     *Executor
	txpool       *txpool.TxPool
	communicator *comm.Communicator

	// Current round (current_round - highest_qc_round determines the timeout).
	// Current round is basically max(highest_qc_round, highest_received_tc, highest_local_tc) + 1
	// update_current_round take care of updating current_round and sending new round event if
	// it changes
	epochState     *EpochState
	currentRound   uint32
	roundStartedAt time.Time

	// HotStuff fields
	lastVotingHeight uint32
	lastVoteMsg      *block.PMVoteMessage
	QCHigh           *block.DraftQC
	lastCommitted    *block.DraftBlock

	lastOnBeatRound int32

	// Channels
	roundTimeoutCh chan PMRoundTimeoutInfo
	roundMutex     sync.Mutex
	cmdCh          chan PMCmd
	beatCh         chan PMBeatInfo

	// Timeout
	roundTimer     *time.Timer
	TCHigh         *types.TimeoutCert
	timeoutCounter uint64

	// broadcast timer
	broadcastCh    chan *block.PMProposalMessage
	broadcastTimer *time.Timer

	//
	newTxCh              chan []byte
	curProposal          *block.DraftBlock
	txsAddedAfterPropose int

	validatorSetRegistry *ValidatorSetRegistry
}

func NewPacemaker(version string, c *chain.Chain, txpool *txpool.TxPool, communicator *comm.Communicator, blsMaster *types.BlsMaster, proxyApp cmtproxy.AppConns) *Pacemaker {
	p := &Pacemaker{
		logger:       slog.With("pkg", "pm"),
		chain:        c,
		blsMaster:    blsMaster,
		version:      version,
		txpool:       txpool,
		communicator: communicator,
		executor:     NewExecutor(proxyApp),

		cmdCh:          make(chan PMCmd, 2),
		beatCh:         make(chan PMBeatInfo, 2),
		roundTimeoutCh: make(chan PMRoundTimeoutInfo, 2),
		roundTimer:     nil,
		roundMutex:     sync.Mutex{},
		broadcastCh:    make(chan *block.PMProposalMessage, 4),
		newTxCh:        txpool.GetNewTxFeed(),

		timeoutCounter:       0,
		lastOnBeatRound:      -1,
		validatorSetRegistry: NewValidatorSetRegistry(c),
	}
	return p
}

func (p *Pacemaker) CreateLeaf(parent *block.DraftBlock, justify *block.DraftQC, round uint32) (error, *block.DraftBlock) {
	// timeout := p.TCHigh != nil
	parentBlock := parent.ProposedBlock
	if parentBlock == nil {
		return ErrParentBlockEmpty, nil
	}

	targetTime := time.Unix(int64(parentBlock.Timestamp()+1), 0)
	now := time.Now()
	if now.After(targetTime) {
		targetTime = now
	}

	res, err := p.executor.PrepareProposal(&v1.PrepareProposalRequest{Height: int64(parent.Height) + 1})
	if err != nil {
		return err, nil
	}

	var txs types.Transactions
	for _, txBytes := range res.Txs {
		txs = append(txs, cmttypes.Tx(txBytes))
	}

	if p.epochState.epoch != 0 && round != 0 && round <= justify.QC.Round {
		p.logger.Warn("Invalid round to propose", "round", round, "round", justify.QC.Round)
		return ErrInvalidRound, nil
	}
	if p.epochState.epoch != 0 && round != 0 && round <= parent.Round {
		p.logger.Warn("Invalid round to propose", "round", round, "parentRound", parent.Round)
		return ErrInvalidRound, nil
	}
	err, draftBlock := p.buildBlock(uint64(targetTime.Unix()), parent, justify, round, 0, txs)
	// p.logger.Info(fmt.Sprintf("proposing %v on R:%v with QCHigh(E:%v,R:%v), Parent(%v,R:%v)", draftBlock.ProposedBlock.CompactString(), round, justify.QC.Epoch, justify.QC.Round, parent.ProposedBlock.ID().ToBlockShortID(), parent.Round))
	if time.Now().Before(targetTime) {
		d := time.Until(targetTime)
		p.logger.Info("sleep until", "targetTime", targetTime, "for", types.PrettyDuration(d))
		time.Sleep(time.Until(targetTime))
	}
	return err, draftBlock
}

// b_exec <- b_lock <- b <- b' <- bnew*
func (p *Pacemaker) Update(qc *block.QuorumCert) {

	var b, bPrime *block.DraftBlock
	//now pipeline full, roll this pipeline first
	bPrime = p.chain.GetDraftByEscortQC(qc)
	if bPrime == nil {
		p.logger.Warn("blockPrime is empty, early termination of Update")
		return
	}
	if bPrime.Committed {
		p.logger.Debug("b' is commited", "b'", bPrime.ProposedBlock.CompactString())
		return
	}
	b = bPrime.Justify.QCNode
	if b.Committed {
		p.logger.Debug("b is committed", "b", b.ProposedBlock.CompactString())
	}
	if b == nil {
		//bnew Justify is already higher than current QCHigh
		p.UpdateQCHigh(&block.DraftQC{QC: qc, QCNode: bPrime})
		p.logger.Warn("block is empty, early termination of Update")
		return
	}

	p.logger.Debug(fmt.Sprintf("qc   = %v", qc.String()))
	p.logger.Debug(fmt.Sprintf("b'   = %v", bPrime.ToString()))
	p.logger.Debug(fmt.Sprintf("b    = %v", b.ToString()))

	// pre-commit phase on b"
	p.UpdateQCHigh(&block.DraftQC{QC: qc, QCNode: bPrime})

	/* commit requires direct parent */
	if bPrime.Parent != b {
		return
	}

	commitReady := []commitReadyBlock{}
	for tmp := bPrime; tmp.Parent.Height > p.lastCommitted.Height; tmp = tmp.Parent {
		// Notice: b must be prepended the slice, so we can commit blocks in order
		commitReady = append([]commitReadyBlock{{block: tmp.Parent, escortQC: tmp.ProposedBlock.QC}}, commitReady...)
	}
	p.OnCommit(commitReady)

	p.lastCommitted = b // commit phase on b
}

func (p *Pacemaker) OnCommit(commitReady []commitReadyBlock) {
	for _, b := range commitReady {

		blk := b.block
		escortQC := b.escortQC

		if blk == nil {
			p.logger.Warn("skip commit empty block")
			continue
		}

		// TBD: how to handle this case???
		if !blk.SuccessProcessed {
			p.logger.Error("process this proposal failed, possible my states are wrong", "height", blk.Height, "round", blk.Round, "action", "commit", "err", blk.ProcessError)
			continue
		}
		if blk.ProcessError == errKnownBlock {
			p.logger.Warn("skip commit known block", "height", blk.Height, "round", blk.Round)
			continue
		}

		// commit the approved block
		err := p.commitBlock(blk, escortQC)
		if err != nil {
			if err != chain.ErrBlockExist && err != errKnownBlock {
				if blk != nil {
					p.logger.Warn("commit failed !!!", "err", err, "blk", blk.ProposedBlock.CompactString())
				} else {
					p.logger.Warn("commit failed !!!", "err", err)
				}
			} else {
				if blk != nil && blk.ProposedBlock != nil {
					p.logger.Debug(fmt.Sprintf("block %d already in chain", blk.ProposedBlock.Number()), "id", blk.ProposedBlock.CompactString())
				} else {
					p.logger.Info("block alreday in chain")
				}
			}
		}

		// BUG FIX: normally proposal message are cleaned once it is committed. It is ok because this proposal
		// is not needed any more. Only in one case, if somebody queries the more old message, we can not give.
		// so proposals are kept in this committee and clean all of them at the stopping of pacemaker.
		// remove this DraftBlock from map.
		//delete(p.proposalMap, b.Height)
		p.chain.PruneDraftsUpTo(blk)
	}
}

func (p *Pacemaker) OnReceiveProposal(mi IncomingMsg) {
	msg := mi.Msg.(*block.PMProposalMessage)
	height := msg.DecodeBlock().Number()
	round := msg.Round

	// drop outdated proposal
	if height < p.lastCommitted.Height {
		p.logger.Info("outdated proposal (height <= bLocked.height), dropped ...", "height", height, "bLocked.height", p.lastCommitted.Height)
		return
	}

	blk := msg.DecodeBlock()
	qc := blk.QC
	p.logger.Debug(fmt.Sprintf("Handling %s", msg.GetType()), "blk", blk.ID().ToBlockShortID())

	// load parent
	parent := p.chain.GetDraft(blk.ParentID())
	if parent == nil {
		if blk.Number() > p.QCHigh.QC.Number() {
			// future propsal, throw it back in queue with extended expire
			// if mi.ExpireAt.Add(time.Second * (-2)).After(time.Now()) {
			// 	mi.ExpireAt = mi.ExpireAt.Add(time.Second + 5)
			// }
			if mi.ProcessCount%2 == 0 {
				query, err := p.BuildQueryMessage()
				if err != nil {
					p.logger.Error("could not build query message")
				}
				peers := make([]*ConsensusPeer, 0)
				// query the replica that forwards this msg
				peers = append(peers, NewConsensusPeer(mi.Peer.Name, mi.Peer.IP))

				// query the proposer
				signerPeer := NewConsensusPeer(mi.Signer.Name, mi.Signer.IP)
				peers = append(peers, signerPeer)

				// query the next proposer
				nxtPeer := p.getProposerByRound(round + 1)
				peers = append(peers, nxtPeer)

				distinctPeers := make([]*ConsensusPeer, 0)
				visited := make(map[string]bool)
				for _, peer := range peers {
					if _, exist := visited[peer.IP]; exist {
						continue
					}
					visited[peer.IP] = true
					if !p.isMe(peer) {
						distinctPeers = append(distinctPeers, peer)
					}
				}
				p.Send(query, distinctPeers...)
				p.logger.Info(`query proposals`, "distinctPeers", len(distinctPeers))
			}

			p.logger.Error("cant load parent for future proposal, throw it back in queue", "parent", blk.ParentID().ToBlockShortID())
			inQueue.DelayedAdd(mi)
		} else {
			p.logger.Warn("cant load parent, dropped ...", "parent", blk.ParentID().ToBlockShortID())
		}
		return
	}

	// check QC with parent
	if match := p.ValidateQC(parent.ProposedBlock, qc); !match {
		p.logger.Error("validate QC failed ...", "qc", qc.String(), "parent", parent.ProposedBlock.ID().ToBlockShortID())
		// Theoratically, this should not be worrisome anymore, since the parent is addressed by blockID
		// instead of addressing proposal by height, we already supported the fork in proposal space
		// so if the qc doesn't match parent proposal known to me, cases are:
		// 1. I don't have the correct parent, I will assume that others to commit to the right one and i'll do nothing
		// 2. The current proposal is invalid and I should not vote
		// in both cases, I should wait instead of sending messages to confuse peers

		return
	}

	// check round
	// round 0 must be the first after a ValidatorUpdate enable
	if round == 0 && !parent.ProposedBlock.IsKBlock() {
		p.logger.Error("round(0) must have a direct ValidatorHash update parent")
		return
	}
	// otherwise round must = parent round + 1 without TC
	if round > 0 && parent.Round+1 != round {
		validTC := p.verifyTC(msg.TimeoutCert, msg.Round)
		if !validTC {
			p.logger.Error("round jump without valid TC", "parentRound", parent.Round, "round", round)
			return
		} else if !parent.ProposedBlock.IsKBlock() && parent.Round >= round {
			p.logger.Error("invalid round", "parentRound", parent.Round, "round", round)
			return
		}
	}

	justify := block.NewDraftQC(qc, parent)
	bnew := &block.DraftBlock{
		Msg:           msg,
		Height:        height,
		Round:         round,
		Parent:        parent,
		Justify:       justify,
		ProposedBlock: blk,
	}

	// validate proposal
	if err := p.ValidateProposal(bnew); err != nil {
		p.logger.Error("validate proposal failed", "err", err)
		return
	}

	// place the current proposal in proposal space
	if !p.chain.HasDraft(blk.ID()) {
		p.chain.AddDraft(bnew)
	}

	if bnew.Height >= p.lastVotingHeight && p.ExtendedFromLastCommitted(bnew) {
		voteMsg, err := p.BuildVoteMessage(msg)
		if err != nil {
			p.logger.Error("could not build vote message", "err", err)
			return
		}

		p.Update(bnew.Justify.QC)
		p.sendMsg(voteMsg, false)
		p.lastVoteMsg = voteMsg
		p.lastVotingHeight = block.Number(voteMsg.VoteBlockID)

		if bnew.ProposedBlock.IsKBlock() {
			p.Regulate()
		} else {
			// enter round and reset timer
			p.enterRound(voteMsg.VoteRound+1, RegularRound)
		}
	} else {
		p.logger.Warn("skip voting", "bnew.height", bnew.Height, "lastVoting", p.lastVotingHeight, "extended", p.ExtendedFromLastCommitted(bnew), "bnew", bnew.ProposedBlock.ID().ToBlockShortID(), "lastCommitted", p.lastCommitted.ProposedBlock.ID().ToBlockShortID())
	}

}

func (p *Pacemaker) OnReceiveVote(mi IncomingMsg) {
	msg := mi.Msg.(*block.PMVoteMessage)
	p.logger.Debug(fmt.Sprintf("Handling %s", msg.GetType()), "blk", msg.VoteBlockID.ToBlockShortID())

	round := msg.VoteRound

	// drop outdated vote
	if !(round == p.currentRound && round == 0) && round < p.currentRound-1 {
		p.logger.Info("outdated vote, dropped ...", "currentRound", p.currentRound, "voteRound", round)
		return
	}
	if !p.amIRoundProproser(round + 1) {
		p.logger.Info("invalid vote, I'm not the expected next proposer ...", "round", round)
		return
	}

	b := p.chain.GetDraft(msg.VoteBlockID)
	if b == nil {
		p.logger.Warn("can not get proposed block", "blk", msg.VoteBlockID.ToBlockShortID())
		inQueue.DelayedAdd(mi)
		// return errors.New("can not address block")
		return
	}
	if b.Round != round {
		p.logger.Info("proposal round mismatch", "voteRound", round, "proposalRound", b.Round, "id", b.ProposedBlock.ID().ToBlockShortID())
		return
	}

	qc := p.epochState.AddQCVote(msg.GetSignerIndex(), round, msg.VoteBlockID, msg.VoteSignature)
	if qc == nil {
		p.logger.Debug("no qc formed")
		return
	}
	newDraftQC := &block.DraftQC{QCNode: b, QC: qc}
	changed := p.UpdateQCHigh(newDraftQC)
	if changed {
		// if QC is updated, schedule onbeat now
		p.Update(qc)
		p.scheduleOnBeat(p.epochState.epoch, round+1)
		p.enterRound(round+1, RegularRound)
	}
}

func (p *Pacemaker) OnPropose(qc *block.DraftQC, round uint32) *block.DraftBlock {

	parent := p.chain.GetDraftByEscortQC(qc.QC)
	err, bnew := p.CreateLeaf(parent, qc, round)
	if err != nil {
		p.logger.Error("could not create leaf", "err", err)
		return nil
	}
	// proposedBlk := bnew.ProposedBlockInfo.ProposedBlock

	if bnew.Height <= qc.QC.Number() {
		p.logger.Error("proposed block refers to an invalid qc", "proposedQC", qc.QC.Number(), "proposedHeight", bnew.Height)
		return nil
	}

	msg, err := p.BuildProposalMessage(bnew.Height, bnew.Round, bnew, p.TCHigh)
	if err != nil {
		p.logger.Error("could not build proposal message", "err", err)
		return nil
	}

	bnew.Msg = msg
	p.curProposal = bnew
	return bnew
}

func (p *Pacemaker) UpdateQCHigh(qc *block.DraftQC) bool {
	updated := false
	oqc := p.QCHigh
	// update local qcHigh if
	// newQC.height > qcHigh.height
	// or newQC.height = qcHigh.height && newQC.round > qcHigh.round
	if qc.QCNode != nil && qc.QC.Number() > p.QCHigh.QC.Number() || (qc.QC.Number() == p.QCHigh.QCNode.Height && qc.QC.Round > p.QCHigh.QCNode.Round) {
		p.QCHigh = qc
		updated = true
		p.logger.Info(fmt.Sprintf("QCHigh update to %s", p.QCHigh.ToString()), "from", oqc.ToString())
	}

	return updated
}

func (p *Pacemaker) OnBeat(epoch uint64, round uint32) {
	// avoid leftover onbeat
	if epoch < p.epochState.epoch {
		p.logger.Warn(fmt.Sprintf("outdated onBeat (epoch(%v) < local epoch(%v)), skip ...", epoch, p.epochState.epoch))
		return
	}
	// avoid duplicate onbeat
	if epoch == p.epochState.epoch && int32(round) <= p.lastOnBeatRound {
		p.logger.Warn(fmt.Sprintf("outdated onBeat (round(%v) <= lastOnBeatRound(%v)), skip ...", round, p.lastOnBeatRound))
		return
	}
	if !p.amIRoundProproser(round) {
		p.logger.Info("I'm NOT round proposer, skip OnBeat", "round", round)
		return
	}
	p.lastOnBeatRound = int32(round)
	p.logger.Info(fmt.Sprintf("==> OnBeat Epoch:%v, Round:%v", epoch, round))
	// parent already got QC, pre-commit it

	//b := p.QCHigh.QCNode
	b := p.chain.GetDraftByEscortQC(p.QCHigh.QC)
	if b == nil {
		return
	}

	pmRoleGauge.Set(2) // proposer

	pStart := time.Now()
	bnew := p.OnPropose(p.QCHigh, round)
	if bnew != nil {
		p.logger.Info(fmt.Sprintf("proposed %s", bnew.ProposedBlock.Oneliner()), "elapsed", types.PrettyDuration(time.Since(pStart)))

		// create slot in proposalMap directly, instead of sendmsg to self.
		p.chain.AddDraft(bnew)

		p.TCHigh = nil

		//send proposal to every committee members including myself
		// p.sendMsg(bnew.Msg, true)

		roundElapsed := time.Since(p.roundStartedAt)
		roundWait := BroadcastTimeLimit - roundElapsed
		// send vote message to next proposer
		p.logger.Debug("schedule broadcast with wait", "wait", roundWait)
		p.scheduleBroadcast(bnew.Msg.(*block.PMProposalMessage), roundWait)
	}
}

func (p *Pacemaker) OnReceiveTimeout(mi IncomingMsg) {
	msg := mi.Msg.(*block.PMTimeoutMessage)
	p.logger.Debug(fmt.Sprintf("Handling %s", msg.GetType()), "epoch", msg.Epoch, "wishRound", msg.WishRound, "lastVoteSig", hex.EncodeToString(msg.LastVoteSignature))

	// drop invalid msg
	if !p.amIRoundProproser(msg.WishRound) {
		p.logger.Debug("invalid timeout msg, I'm not the expected proposer", "epoch", msg.Epoch, "wishRound", msg.WishRound)
		return
	}

	// collect vote and see if QC is formed
	newQC := p.epochState.AddQCVote(msg.SignerIndex, msg.LastVoteRound, msg.LastVoteBlockID, msg.LastVoteSignature)
	if newQC != nil {
		escortQCNode := p.chain.GetDraftByEscortQC(newQC)
		p.UpdateQCHigh(&block.DraftQC{QCNode: escortQCNode, QC: newQC})
		p.Update(newQC)
	}

	qc := msg.DecodeQCHigh()
	qcNode := p.chain.GetDraftByEscortQC(qc)
	p.UpdateQCHigh(&block.DraftQC{QCNode: qcNode, QC: qc})

	// collect wish vote to see if TC is formed
	tc := p.epochState.AddTCVote(msg.SignerIndex, msg.WishRound, msg.WishVoteSig, msg.WishVoteHash)
	if tc != nil {
		p.TCHigh = tc
		p.scheduleOnBeat(p.epochState.epoch, p.TCHigh.Round)
	}
}

func (p *Pacemaker) OnReceiveQuery(mi IncomingMsg) {
	msg := mi.Msg.(*block.PMQueryMessage)
	proposals := p.chain.GetDraftsUpTo(msg.LastCommitted, p.QCHigh.QC)
	p.logger.Info(`received query`, "lastCommitted", msg.LastCommitted.ToBlockShortID(), "from", mi.Peer)
	for _, proposal := range proposals {
		p.logger.Info(`forward proposal`, "id", proposal.ProposedBlock.ID().ToBlockShortID(), "to", mi.Peer)
		p.sendMsg(proposal.Msg, false)
		p.Send(proposal.Msg, NewConsensusPeer(mi.Peer.Name, mi.Peer.IP))
	}
}

func (p *Pacemaker) updateEpochState(leaf *block.Block) bool {
	if p.epochState != nil && leaf.Number() != 0 && leaf.Epoch() == p.epochState.epoch {
		return false
	}
	epochState, err := NewEpochState(p.chain, leaf, p.blsMaster.PubKey)
	if err != nil {
		p.logger.Info("could not create epoch state", "err", err)
		return false
	}

	if epochState == nil {
		p.logger.Warn("EPOCH STATE IS EMPTY")
	}

	if epochState.InCommittee() {
		me := epochState.GetValidatorByIndex(epochState.CommitteeIndex())
		myAddr := me.IP
		myName := me.Name

		p.logger.Info("I'm IN committee !!!", "myName", myName, "myIP", myAddr.String())
		inCommitteeGauge.Set(1)
		pmRoleGauge.Set(1) // validator
	} else {
		p.logger.Info("I'm NOT in committee")
		inCommitteeGauge.Set(0)
	}

	p.logger.Info("---------------------------------------------------------")
	p.logger.Info(fmt.Sprintf("Entered epoch %d", epochState.epoch))
	p.logger.Info("---------------------------------------------------------")

	p.epochState = epochState
	return true
}

func (p *Pacemaker) Start() {
	p.Regulate()
	go p.mainLoop()
}

// Committee Leader triggers
func (p *Pacemaker) Regulate() {

	bestQC := p.chain.BestQC()
	if p.QCHigh != nil && p.QCHigh.QC.Number() > bestQC.Number() {
		bestQC = p.QCHigh.QC
	}

	bestNode := p.chain.GetDraftByEscortQC(bestQC)
	if bestNode == nil {
		p.logger.Debug("started with empty qcNode")
	}

	p.updateEpochState(bestNode.ProposedBlock)

	round := bestQC.Round
	actualRound := round + 1
	if bestNode.ProposedBlock.IsKBlock() {
		actualRound = 0
	}

	p.logger.Info(fmt.Sprintf("*** Pacemaker regulate with bestQC %v", bestQC.CompactString()))
	p.lastOnBeatRound = int32(actualRound) - 1

	qcInit := block.NewDraftQC(bestQC, bestNode)

	// now assign b_lock b_exec, b_leaf qc_high
	p.lastCommitted = bestNode
	p.lastVotingHeight = 0
	p.lastVoteMsg = nil
	p.QCHigh = qcInit
	p.chain.AddDraft(bestNode)

	p.currentRound = 0
	p.enterRound(actualRound, RegularRound)
	p.scheduleOnBeat(p.epochState.epoch, actualRound)
}

func (p *Pacemaker) scheduleOnBeat(epoch uint64, round uint32) {
	// p.enterRound(round, IncRoundOnBeat)
CleanBeatCh:
	for {
		select {
		case <-p.beatCh:
		default:
			break CleanBeatCh
		}
	}
	p.beatCh <- PMBeatInfo{epoch, round}
}

func (p *Pacemaker) ScheduleRegulate() {
	// schedule Regulate
	// make sure this Regulate cmd is the very next cmd
CleanCMDCh:
	for {
		select {
		case <-p.cmdCh:
		default:
			break CleanCMDCh
		}
	}

	p.cmdCh <- PMCmdRegulate
	p.logger.Info("regulate scheduled")
}

func (p *Pacemaker) mainLoop() {
	interruptCh := make(chan os.Signal, 1)
	// signal.Notify(interruptCh, syscall.SIGINT, syscall.SIGTERM)

	for {
		bestBlock := p.chain.BestBlock()
		if bestBlock.Number() > p.QCHigh.QC.Number() && p.epochState.InCommittee() {
			p.logger.Info("bestBlock > QCHigh, schedule regulate", "best", bestBlock.Number(), "qcHigh", p.QCHigh.QC.Number())
			p.ScheduleRegulate()
		}
		select {

		case cmd := <-p.cmdCh:
			if cmd == PMCmdRegulate {
				p.Regulate()
			}
		case ti := <-p.roundTimeoutCh:
			if ti.epoch < p.epochState.epoch {
				p.logger.Info("skip timeout handling due to epoch mismatch", "timeoutRound", ti.round, "timeoutEpoch", ti.epoch, "myEpoch", p.epochState.epoch)
				continue
			}
			p.OnRoundTimeout(ti)
		case newTxID := <-p.newTxCh:
			if p.epochState.InCommittee() && p.amIRoundProproser(p.currentRound) && p.curProposal != nil && p.curProposal.ProposedBlock != nil && p.curProposal.ProposedBlock.BlockHeader != nil && p.curProposal.Round == p.currentRound {
				if time.Since(p.roundStartedAt) < ProposeTimeLimit {
					p.AddTxToCurProposal(newTxID)
				}
			}
		case <-p.broadcastCh:
			p.OnBroadcastProposal()
		case b := <-p.beatCh:
			p.OnBeat(b.epoch, b.round)
		case m := <-inQueue.queue:
			// if not in committee, skip rcvd messages
			if !p.epochState.InCommittee() {
				p.logger.Info("skip handling msg bcuz I'm not in committee", "type", m.Msg.GetType())
				continue
			}
			if m.Msg.GetEpoch() != p.epochState.epoch {
				p.logger.Info("rcvd message w/ mismatched epoch ", "epoch", m.Msg.GetEpoch(), "myEpoch", p.epochState.epoch, "type", m.Msg.GetType())
				continue
			}
			if m.Expired() {
				p.logger.Info(fmt.Sprintf("incoming %s msg expired, dropped ...", m.Msg.GetType()))
				continue
			}
			switch m.Msg.(type) {
			case *block.PMProposalMessage:
				p.OnReceiveProposal(m)
			case *block.PMVoteMessage:
				p.OnReceiveVote(m)
			case *block.PMTimeoutMessage:
				p.OnReceiveTimeout(m)
			case *block.PMQueryMessage:
				p.OnReceiveQuery(m)
			default:
				p.logger.Warn("received an message in unknown type")
			}

		case <-interruptCh:
			p.logger.Warn("interrupt by user, exit now")
			return

		}
	}
}

func (p *Pacemaker) OnRoundTimeout(ti PMRoundTimeoutInfo) {
	if ti.epoch < p.epochState.epoch {
		p.logger.Warn(fmt.Sprintf("E:%d,R:%d timeout, but epoch mismatch, ignored ...", ti.epoch, ti.round), "curEpoch", p.epochState.epoch)
	}
	p.logger.Warn(fmt.Sprintf("E:%d,R:%d timeout", ti.epoch, ti.round), "counter", p.timeoutCounter)

	p.enterRound(ti.round+1, TimeoutRound)

	// send new round msg to next round proposer
	msg, err := p.BuildTimeoutMessage(p.QCHigh, &ti, p.lastVoteMsg)
	if err != nil {
		p.logger.Error("could not build timeout message", "err", err)
	} else {
		p.sendMsg(msg, false)
	}
}

func (p *Pacemaker) enterRound(round uint32, rtype roundType) bool {
	if round > 0 && round < p.currentRound {
		p.logger.Warn(fmt.Sprintf("update round skipped %d->%d", p.currentRound, round))
		return false
	}
	if !p.epochState.InCommittee() {
		return false
	}
	var interval time.Duration
	switch rtype {
	case RegularRound:
		fallthrough
	case TimeoutRound:
		interval = p.resetRoundTimer(round, rtype)
	default:
		return false
	}

	restart := (round == p.currentRound)
	oldRound := p.currentRound
	p.currentRound = round
	p.roundStartedAt = time.Now()
	proposer := p.epochState.getRoundProposer(round)

	if restart {
		p.logger.Info(fmt.Sprintf("E:%d, Round:%d restart", p.epochState.epoch, p.currentRound), oldRound, "type", rtype.String(), "proposer", proposer.NameAndIP(), "interval", types.PrettyDuration(interval))
	} else {
		p.logger.Info("---------------------------------------------------------")
		p.logger.Info(fmt.Sprintf("E:%d, Round:%d start", p.epochState.epoch, p.currentRound), "lastRound", oldRound, "type", rtype.String(), "proposer", proposer.NameAndIP(), "interval", types.PrettyDuration(interval))
	}
	pmRoundGauge.Set(float64(p.currentRound))
	return true
}

func (p *Pacemaker) resetRoundTimer(round uint32, rtype roundType) time.Duration {
	p.roundMutex.Lock()
	defer p.roundMutex.Unlock()
	// stop existing round timer
	if p.roundTimer != nil {
		p.logger.Debug(fmt.Sprintf("stop timer for round %d", p.currentRound))
		p.roundTimer.Stop()
		p.roundTimer = nil
	}
	// start round timer
	if p.roundTimer == nil {
		baseInterval := RoundTimeoutInterval
		switch rtype {
		case RegularRound:
			p.timeoutCounter = 0
		case TimeoutRound:
			p.timeoutCounter++
		}
		var power uint64 = 0
		if p.timeoutCounter > 1 {
			power = p.timeoutCounter - 1
		}
		timeoutInterval := baseInterval * (1 << power)
		// p.logger.Debug(fmt.Sprintf("> start round %d timer", round), "interval", int64(timeoutInterval/time.Second), "timeoutCount", p.timeoutCounter)
		epoch := p.epochState.epoch
		p.roundTimer = time.AfterFunc(timeoutInterval, func() {
			p.roundTimeoutCh <- PMRoundTimeoutInfo{epoch: epoch, round: round, counter: p.timeoutCounter}
		})
		return timeoutInterval
	}
	return time.Second
}

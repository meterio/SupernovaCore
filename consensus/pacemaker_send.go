// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package consensus

// This is part of pacemaker that in charge of:
// 1. build outgoing messages
// 2. send messages to peer

import (
	sha256 "crypto/sha256"
	"fmt"
	"net"
	"time"

	"github.com/ethereum/go-ethereum/rlp"
	"github.com/meterio/supernova/block"
	"github.com/meterio/supernova/types"
)

func (p *Pacemaker) sendMsg(msg block.ConsensusMessage, copyMyself bool) bool {
	myNetAddr := p.getMyNetAddr()
	myName := p.getMyName()
	myself := NewConsensusPeer(myName, myNetAddr.IP.String())

	round := msg.GetRound()

	peers := make([]*ConsensusPeer, 0)
	switch msg.(type) {
	case *block.PMProposalMessage:
		peers = p.epochState.GetRelayPeers(round)
	case *block.PMVoteMessage:
		nxtProposer := p.getProposerByRound(round + 1)
		peers = append(peers, nxtProposer)
	case *block.PMTimeoutMessage:
		nxtProposer := p.getProposerByRound(round)
		peers = append(peers, nxtProposer)
	}

	myselfInPeers := myself == nil
	for _, p := range peers {
		if p.IP == myNetAddr.IP.String() {
			myselfInPeers = true
			break
		}
	}
	// send consensus message to myself first (except for PMNewViewMessage)
	if copyMyself && !myselfInPeers {
		p.Send(msg, myself)
	}

	p.Send(msg, peers...)
	return true
}

func (p *Pacemaker) BuildProposalMessage(height, round uint32, bnew *block.DraftBlock, tc *types.TimeoutCert) (*block.PMProposalMessage, error) {
	raw, err := rlp.EncodeToBytes(bnew.ProposedBlock)
	if err != nil {
		return nil, err
	}
	msg := &block.PMProposalMessage{
		Timestamp:   time.Now(),
		Epoch:       p.epochState.epoch,
		SignerIndex: uint32(p.epochState.CommitteeIndex()),

		Round:       round,
		RawBlock:    raw,
		TimeoutCert: tc,
	}

	// sign message
	p.SignMessage(msg)
	p.logger.Debug("Built Proposal Message", "blk", bnew.ProposedBlock.ID().ToBlockShortID(), "msg", msg.String(), "timestamp", msg.Timestamp)

	return msg, nil
}

// BuildVoteMsg build VFP message for proposal
// txRoot, stateRoot is decoded from proposalMsg.ProposedBlock, carry in cos already decoded outside
func (p *Pacemaker) BuildVoteMessage(proposalMsg *block.PMProposalMessage) (*block.PMVoteMessage, error) {

	proposedBlock := proposalMsg.DecodeBlock()
	voteHash := proposedBlock.ID()
	voteSig := p.blsMaster.SignHash(voteHash)
	// p.logger.Debug("Built PMVoteMessage", "signMsg", signMsg)

	msg := &block.PMVoteMessage{
		Timestamp:   time.Now(),
		Epoch:       p.epochState.epoch,
		SignerIndex: uint32(p.epochState.CommitteeIndex()),

		VoteRound:     proposalMsg.Round,
		VoteBlockID:   proposedBlock.ID(),
		VoteSignature: voteSig,
	}

	// sign message
	p.SignMessage(msg)
	p.logger.Debug("Built Vote Message", "msg", msg.String())
	return msg, nil
}

// Timeout Vote Message Hash
func BuildTimeoutVotingHash(epoch uint64, round uint32) [32]byte {
	msg := fmt.Sprintf("Timeout: Epoch:%v Round:%v", epoch, round)
	voteHash := sha256.Sum256([]byte(msg))
	return voteHash
}

// BuildVoteForProposalMsg build VFP message for proposal
func (p *Pacemaker) BuildTimeoutMessage(qcHigh *block.DraftQC, ti *PMRoundTimeoutInfo, lastVoteMsg *block.PMVoteMessage) (*block.PMTimeoutMessage, error) {

	// TODO: changed from nextHeight/nextRound to ti.height/ti.round, not sure if this is correct
	wishVoteHash := BuildTimeoutVotingHash(ti.epoch, ti.round)
	wishVoteSig := p.blsMaster.SignHash(wishVoteHash)

	rawQC, err := rlp.EncodeToBytes(qcHigh.QC)
	if err != nil {
		return nil, err
	}
	msg := &block.PMTimeoutMessage{
		Timestamp:   time.Now(),
		Epoch:       ti.epoch,
		SignerIndex: uint32(p.epochState.CommitteeIndex()),

		WishRound: ti.round,

		QCHigh: rawQC,

		WishVoteHash: wishVoteHash,
		WishVoteSig:  wishVoteSig,
	}

	// attach last vote
	if lastVoteMsg != nil {
		p.logger.Info(fmt.Sprintf("attached last vote on R:%d", lastVoteMsg.VoteRound), "blk", lastVoteMsg.VoteBlockID.ToBlockShortID())
		msg.LastVoteRound = lastVoteMsg.VoteRound
		msg.LastVoteBlockID = lastVoteMsg.VoteBlockID
		msg.LastVoteSignature = lastVoteMsg.VoteSignature
	}

	// if ti != nil {
	// 	msg.TimeoutHeight = nextHeight
	// 	msg.TimeoutRound = ti.round
	// 	msg.TimeoutCounter = ti.counter
	// }
	// sign message
	p.SignMessage(msg)
	p.logger.Debug("Built New View Message", "msg", msg.String())
	return msg, nil
}

// BuildQueryMessage
func (p *Pacemaker) BuildQueryMessage() (*block.PMQueryMessage, error) {
	msg := &block.PMQueryMessage{
		Timestamp:   time.Now(),
		Epoch:       p.epochState.epoch,
		SignerIndex: uint32(p.epochState.CommitteeIndex()),

		LastCommitted: p.lastCommitted.ProposedBlock.ID(),
	}

	// sign message
	p.SignMessage(msg)
	p.logger.Debug("Built Query Message", "msg", msg.String())
	return msg, nil
}

func (p *Pacemaker) getMyNetAddr() *types.NetAddress {
	me := p.epochState.GetMyself()
	if me == nil {
		return &types.NetAddress{IP: net.IP{}, Port: 0}
	}
	return types.NewNetAddressFromNetIP(me.IP, me.Port)
}

func (p *Pacemaker) getMyName() string {
	me := p.epochState.GetMyself()
	if me == nil {
		return "unknown"
	}
	return me.Name
}

func (p *Pacemaker) isMe(peer *ConsensusPeer) bool {
	me := p.epochState.GetMyself()
	return me != nil && me.IP.String() == peer.IP
}

func (p *Pacemaker) Send(msg block.ConsensusMessage, peers ...*ConsensusPeer) {
	rawMsg, err := p.MarshalMsg(msg)
	if err != nil {
		p.logger.Warn("could not marshal msg", "err", err)
		return
	}

	if len(peers) > 0 {
		for _, peer := range peers {
			outQueue.Add(*peer, msg, rawMsg, false)
		}
	}
}

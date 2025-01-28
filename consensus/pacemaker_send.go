// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package consensus

// This is part of pacemaker that in charge of:
// 1. build outgoing messages
// 2. send messages to peer

import (
	"context"
	sha256 "crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/meterio/supernova/block"
	"github.com/meterio/supernova/libs/message"
	snmsg "github.com/meterio/supernova/libs/message"
	"github.com/meterio/supernova/libs/p2p"
	"github.com/meterio/supernova/types"
)

func (p *Pacemaker) BuildProposalMessage(height, round uint32, bnew *block.DraftBlock, tc *types.TimeoutCert) (*block.PMProposalMessage, error) {
	raw, err := rlp.EncodeToBytes(bnew.ProposedBlock)
	if err != nil {
		return nil, err
	}
	msg := &block.PMProposalMessage{
		Timestamp:   time.Now(),
		Epoch:       p.epochState.epoch,
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
	voteSig := p.blsMaster.SignMessage(voteHash[:])
	// p.logger.Debug("Built PMVoteMessage", "signMsg", signMsg)

	msg := &block.PMVoteMessage{
		Timestamp:   time.Now(),
		Epoch:       p.epochState.epoch,
		SignerIndex: uint32(p.epochState.CommitteeIndex()),

		VoteRound:     proposalMsg.Round,
		VoteBlockID:   proposedBlock.ID(),
		VoteSignature: voteSig.Marshal(),
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
	wishVoteSig := p.blsMaster.SignMessage(wishVoteHash[:])

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
		WishVoteSig:  wishVoteSig.Marshal(),
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

		LastCommitted: p.lastCommitted.ID(),
	}

	// sign message
	p.SignMessage(msg)
	p.logger.Debug("Built Query Message", "msg", msg.String())
	return msg, nil
}

func (p *Pacemaker) Broadcast(msg block.ConsensusMessage) {
	rawMsg, err := block.EncodeMsg(msg)
	if err != nil {
		p.logger.Warn("could not marshal msg", "err", err)
		return
	}
	me := p.epochState.GetMyself()
	pbMsg := &snmsg.ConsensusEnvelope{Raw: rawMsg, SenderAddr: me.Address}
	// topic, ok := GossipTypeMapping[reflect.TypeOf(msg)]
	// if !ok {
	// 	p.logger.Warn("could not find topic")
	// 	return
	// }
	// topic := p2p.ConsensusTopic

	// msgBytes, err := proto.Marshal(pbMsg)
	// if err != nil {
	// 	p.logger.Error("proto.Marshal failed", "err", err)
	// 	return
	// }
	// if err := p.p2pSrv.PublishToTopic(p.ctx, topic, msgBytes); err != nil {
	// 	p.logger.Error("PublishToTopic failed", "err", err)
	// 	return
	// }

	p.logger.Debug("broadcast msg", "topic", p2p.ConsensusTopic, "msg", msg.String(), "peersCount", len(p.p2pSrv.Peers().All()))
	sszBytes, err := pbMsg.MarshalSSZ()
	if err != nil {
		p.logger.Error("marshal failed", "err", err)
		return
	}
	topic, err := p.p2pSrv.JoinTopic(p2p.ConsensusTopic)
	// err = p.p2pSrv.PublishToTopic(p.ctx, p2p.ConsensusTopic, sszBytes)
	if err != nil {
		p.logger.Error("Broadcast failed", "err", err)
		return
	}
	topic.Publish(context.TODO(), sszBytes)
	// consensusMsg, err := block.DecodeMsg(pbMsg.Raw)
	// if err != nil {
	// 	p.logger.Error("malformatted msg", "msg", consensusMsg, "err", err)
	// }

	// senderAddr := common.BytesToAddress(pbMsg.SenderAddr)
	// mi := newIncomingMsg(consensusMsg, senderAddr)
	// p.AddIncoming(*mi)
}

func (p *Pacemaker) AddIncoming(mi IncomingMsg) {
	msg, senderAddr := mi.Msg, mi.SenderAddr

	if msg.GetEpoch() < p.epochState.epoch {
		p.logger.Info(fmt.Sprintf("outdated %s, dropped ...", msg.String()), "sender", senderAddr)
		return
	}

	if msg.GetEpoch() == p.epochState.epoch {
		signerIndex := msg.GetSignerIndex()
		if signerIndex >= p.epochState.CommitteeSize() {
			p.logger.Warn("index out of range for signer, dropped ...", "sender", senderAddr, "msg", msg.GetType())
			return
		}
		_, signer := p.epochState.GetValidatorByIndex(int(signerIndex))

		if !msg.VerifyMsgSignature(signer.PubKey) {
			p.logger.Error("invalid signature, dropped ...", "senderAddr", senderAddr, "msg", msg.String(), "signer", signer.Address)
			return
		}
	}

	// sanity check for messages
	switch m := msg.(type) {
	case *block.PMProposalMessage:
		blk := m.DecodeBlock()
		if blk == nil {
			p.logger.Error("Invalid PMProposal: could not decode proposed block")
			return
		}

	case *block.PMTimeoutMessage:
		qcHigh := m.DecodeQCHigh()
		if qcHigh == nil {
			p.logger.Error("Invalid QCHigh: could not decode qcHigh")
		}
	}

	if msg.GetEpoch() == p.epochState.epoch {
		err := inQueue.Add(mi)
		if err != nil {
			return
		}

	} else {
		time.AfterFunc(time.Second, func() {
			p.logger.Info(fmt.Sprintf("future message %s in epoch %d, process after 1s ...", msg.GetType(), msg.GetEpoch()), "curEpoch", p.epochState.epoch)
			p.AddIncoming(mi)
		})
	}
}

func (p *Pacemaker) subscribeToConsensusMessage() {
	p.logger.Debug("subscribe to topic", "topic", p2p.ConsensusTopic)
	sub, err := p.p2pSrv.SubscribeToTopic(p2p.ConsensusTopic)

	if err != nil {
		p.logger.Warn("subscribe to topic failed", "err", err)
		// FIXME: change this
		panic(err)
	}
	for {
		msg, err := sub.Next(p.ctx)
		if err != nil {
			if !errors.Is(err, pubsub.ErrSubscriptionCancelled) { // Only log a warning on unexpected errors.
				p.logger.Error("Subscription next failed", "err", err)
			}
			p.logger.Error("subscription canceled", "err", err)
			sub.Cancel()
			return
		}
		// if msg.ValidatorData == nil {
		// 	p.logger.Warn("Received nil message on pubsub")
		// 	continue
		// }

		p.logger.Debug("received pubsub msg", "id", msg.ID, "from", hex.EncodeToString(msg.From))

		pbMsg := &message.ConsensusEnvelope{}
		err = pbMsg.UnmarshalSSZ(msg.Data)
		if err != nil {
			fmt.Println("Error: ", err, "skip")
			continue
		}
		consensusMsg, err := block.DecodeMsg(pbMsg.Raw)
		if err != nil {
			p.logger.Error("malformatted msg", "msg", consensusMsg, "err", err)
			continue
		}

		senderAddr := common.BytesToAddress(pbMsg.SenderAddr)

		msgInfo := newIncomingMsg(consensusMsg, senderAddr)
		p.AddIncoming(*msgInfo)

	}

}

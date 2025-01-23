// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package consensus

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/meterio/supernova/block"
	"github.com/meterio/supernova/chain"
	cmn "github.com/meterio/supernova/libs/common"
	"github.com/meterio/supernova/types"
	"github.com/prysmaticlabs/prysm/v5/crypto/bls"
)

// This is part of pacemaker that in charge of:
// 1. pending proposal/newView
// 2. timeout cert management

// check a DraftBlock is the extension of b_locked, max 10 hops
func (p *Pacemaker) ExtendedFromLastCommitted(b *block.DraftBlock) bool {

	i := int(0)
	tmp := b
	for i < 10 {
		if tmp.ProposedBlock.ID() == p.lastCommitted.ID() {
			return true
		}
		if tmp = tmp.Parent; tmp == nil {
			break
		}
		i++
	}
	return false
}

func (p *Pacemaker) ValidateProposal(b *block.DraftBlock) error {
	start := time.Now()
	blk := b.ProposedBlock

	defer func() {
		if b != nil && b.ProcessError != nil {
			p.logger.Info(fmt.Sprintf("failed to validate proposal Block R:%v, %v, txs:%d", b.Round, blk.CompactString(), len(b.ProposedBlock.Transactions())), "elapsed", types.PrettyDuration(time.Since(start)), "err", b.ProcessError)

		} else {
			p.logger.Info(fmt.Sprintf("validated proposal Block R:%v, %v, txs:%d", b.Round, blk.CompactString(), len(b.ProposedBlock.Transactions())), "elapsed", types.PrettyDuration(time.Since(start)))
		}
	}()

	// avoid duplicate validation
	if b.SuccessProcessed && b.ProcessError == nil {
		return nil
	}

	parent := b.Parent
	if parent == nil || parent.ProposedBlock == nil {
		return errParentMissing
	}
	parentBlock := parent.ProposedBlock
	if parentBlock == nil {
		return errDecodeParentFailed
	}

	var txsInBlk []cmttypes.Tx
	for _, tx := range blk.Transactions() {
		txsInBlk = append(txsInBlk, tx)
	}

	// make sure tx does not exist in draft cache
	if len(blk.Transactions()) > 0 {
		txsInCache := make(map[string]bool)
		tmp := parent
		for tmp != nil && !tmp.Committed {
			for _, knownTx := range tmp.ProposedBlock.Transactions() {
				txsInCache[hex.EncodeToString(knownTx.Hash())] = true
			}
			tmp = p.chain.GetDraft(tmp.ProposedBlock.ParentID())
		}
		for _, tx := range blk.Transactions() {
			if _, existed := txsInCache[hex.EncodeToString(tx.Hash())]; existed {
				p.logger.Error("tx already existed in cache", "id", tx.Hash(), "containedInBlock", parent.ProposedBlock.ID())
				return errors.New("tx already existed in cache")

			}
		}
	}

	accepted, err := p.executor.ProcessProposal(blk)

	if err != nil {
		return err
	}
	if accepted {
		b.SuccessProcessed = true
		b.ProcessError = nil
		return nil
	} else {
		err = ErrProposalRejected
	}

	if err != nil && err != errKnownBlock {
		p.logger.Error("process proposed failed", "proposed", blk.Oneliner(), "err", err)
		b.SuccessProcessed = false
		b.ProcessError = err
		return err
	}
	b.SuccessProcessed = true
	b.ProcessError = nil

	return nil
}

func (p *Pacemaker) verifyTC(tc *types.TimeoutCert, round uint32) bool {
	if tc != nil {
		voteHash := BuildTimeoutVotingHash(tc.Epoch, tc.Round)
		pubkeys := make([]bls.PublicKey, 0)

		// check epoch and round
		if tc.Epoch != p.epochState.epoch || tc.Round != round {
			return false

		}
		// check hash
		if !bytes.Equal(tc.MsgHash[:], voteHash[:]) {
			return false
		}
		// check vote count
		voteCount := tc.BitArray.Count()
		if !block.MajorityTwoThird(uint32(voteCount), p.epochState.CommitteeSize()) {
			return false
		}

		// check signature
		for index, v := range p.epochState.committee.Validators {
			cmnPubkey, err := cmn.PublicKeyFromBytes(v.PubKey.Bytes())
			if err != nil {
				// FIXME: better handling
				continue
			}

			if tc.BitArray.GetIndex(index) {
				pubkeys = append(pubkeys, cmnPubkey)
			}
		}
		aggrSig, err := bls.SignatureFromBytes(tc.AggSig)
		if err != nil {
			return false
		}
		valid := aggrSig.FastAggregateVerify(pubkeys, tc.MsgHash)
		if !valid {
			p.logger.Warn("Invalid TC", "expected", fmt.Sprintf("E%v.R%v", tc.Epoch, tc.Round), "proposal", fmt.Sprintf("E%v.R%v", p.epochState.epoch, round))
		}
		return valid
	}
	return false
}

func (p *Pacemaker) amIRoundProproser(round uint32) bool {
	proposer := p.epochState.getRoundProposer(round)
	if proposer == nil || proposer.PubKey == nil {
		return false
	}
	return bytes.Equal(proposer.PubKey.Bytes(), p.blsMaster.CmtPubKey.Bytes())
}

func (p *Pacemaker) SignMessage(msg block.ConsensusMessage) {
	msgHash := msg.GetMsgHash()
	sig := p.blsMaster.PrivKey.Sign(msgHash[:])
	msg.SetMsgSignature(sig.Marshal())
}

func (p *Pacemaker) printFork(fork *chain.Fork) {
	if len(fork.Branch) >= 2 {
		trunkLen := len(fork.Trunk)
		branchLen := len(fork.Branch)
		p.logger.Warn(fmt.Sprintf(
			`
⑂⑂⑂⑂⑂⑂⑂⑂ FORK HAPPENED ⑂⑂⑂⑂⑂⑂⑂⑂
ancestor: %v
trunk:    %v  %v
branch:   %v  %v`, fork.Ancestor,
			trunkLen, fork.Trunk[trunkLen-1],
			branchLen, fork.Branch[branchLen-1]))
	}
}

func (p *Pacemaker) EpochStartKBlockNum() uint32 {
	return block.Number(p.epochState.startKBlockID)
}

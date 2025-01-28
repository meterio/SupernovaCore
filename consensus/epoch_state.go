package consensus

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	cmtcrypto "github.com/cometbft/cometbft/crypto"
	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/meterio/supernova/block"
	"github.com/meterio/supernova/chain"
	cmn "github.com/meterio/supernova/libs/common"
	"github.com/meterio/supernova/types"

	"github.com/prysmaticlabs/prysm/v5/crypto/bls"
)

type EpochState struct {
	logger        *slog.Logger
	epoch         uint64
	startKBlockID types.Bytes32
	// committee calculated from last validator set and last nonce
	committee     *cmttypes.ValidatorSet
	inCommittee   bool
	index         int
	qcVoteManager *QCVoteManager
	tcVoteManager *TCVoteManager
	pending       bool
}

func NewEpochState(c *chain.Chain, leaf *block.Block, myPubKey cmtcrypto.PubKey) (*EpochState, error) {
	logger := slog.With("pkg", "es")
	var kblk *block.Block
	if leaf.IsKBlock() {
		kblk = leaf
	} else {
		var err error
		num := leaf.LastKBlock()
		kblk, err = c.GetTrunkBlock(num)
		if err != nil {
			return nil, err
		}
	}

	vset := c.GetNextValidatorSet(kblk.Number())
	if vset == nil {
		slog.Error("Could not get next validator set", "num", kblk.Number())
		return nil, errors.New("could not get next validator set")
	}
	vsetAdapter := cmn.NewValidatorSetAdapter(vset)
	vsetAdapter.SortWithNonce(kblk.Nonce())
	committee := vsetAdapter.ToValidatorSet()
	logger.Info("calc epoch state", "kblk", kblk.Number(), "vsetHash", hex.EncodeToString(vset.Hash()), "committeeSize", committee.Size())

	// it is used for temp calculate committee set by a given nonce in the fly.
	// also return the committee

	inCommittee := false
	index := -1
	if len(committee.Validators) > 0 {
		for i, val := range committee.Validators {
			if bytes.Equal(val.PubKey.Bytes(), myPubKey.Bytes()) {
				index = i
				inCommittee = true
			}
		}
	}

	curEpochGauge.Set(float64(kblk.Epoch()) + 1)
	return &EpochState{
		epoch:  kblk.Epoch() + 1,
		logger: logger,

		startKBlockID: kblk.ID(),
		committee:     committee,
		inCommittee:   inCommittee,
		index:         index,
		qcVoteManager: NewQCVoteManager(uint32(committee.Size())),
		tcVoteManager: NewTCVoteManager(uint32(committee.Size())),
		pending:       false,
	}, nil
}

func NewPendingEpochState(vset *cmttypes.ValidatorSet, myPubKey bls.PublicKey, curEpoch uint64) (*EpochState, error) {
	logger := slog.With("pkg", "es")
	if vset == nil {
		slog.Error("validator set is nil")
		return nil, errors.New("validator set is nil")
	}
	committee := vset
	logger.Info("calc epoch state", "vsetHash", hex.EncodeToString(vset.Hash()), "committeeSize", committee.Size())

	// it is used for temp calculate committee set by a given nonce in the fly.
	// also return the committee

	inCommittee := false
	index := -1
	if len(committee.Validators) > 0 {
		for i, val := range committee.Validators {
			if bytes.Equal(val.PubKey.Bytes(), myPubKey.Marshal()) {
				index = i
				inCommittee = true
			}
		}
	}

	return &EpochState{
		logger: logger,
		epoch:  curEpoch + 1,

		committee:   committee,
		inCommittee: inCommittee,
		index:       index,
		pending:     true,
	}, nil
}

func (es *EpochState) AddQCVote(signerIndex uint32, round uint32, blockID types.Bytes32, sig []byte) *block.QuorumCert {
	v := es.committee.Validators[signerIndex]
	signature, err := bls.SignatureFromBytes(sig)
	if err != nil {
		es.logger.Warn(fmt.Sprintf("invalid signature in QC vote R%v", round), "sig", hex.EncodeToString(sig), "err", err)
		return nil
	}
	cmnPubKey, err := cmn.PublicKeyFromBytes(v.PubKey.Bytes())
	if err != nil {
		es.logger.Warn("Invalid key", "err", err)
		return nil
	}
	verified := signature.Verify(cmnPubKey, blockID[:])
	if !verified {
		es.logger.Warn("invalid vote", "sig", hex.EncodeToString(sig), "signerIndex", signerIndex)
		return nil
	}

	return es.qcVoteManager.AddVote(signerIndex, es.epoch, round, blockID, sig)
}

func (es *EpochState) AddTCVote(signerIndex uint32, round uint32, sig []byte, hash [32]byte) *types.TimeoutCert {
	v := es.committee.Validators[signerIndex]
	signature, err := bls.SignatureFromBytes(sig)
	if err != nil {
		es.logger.Warn(fmt.Sprintf("invalid signature in TC vote R%v", round), "sig", hex.EncodeToString(sig), "err", err)
		return nil
	}
	cmnPubKey, err := cmn.PublicKeyFromBytes(v.PubKey.Bytes())
	if err != nil {
		es.logger.Warn("Invalid key", "err", err)
		return nil
	}
	verified := signature.Verify(cmnPubKey, hash[:])
	if !verified {
		es.logger.Warn("invalid vote", "sig", hex.EncodeToString(sig), "signerIndex", signerIndex)
		return nil
	}
	return es.tcVoteManager.AddVote(signerIndex, es.epoch, round, sig, hash)
}

func (es *EpochState) CommitteeSize() uint32 {
	return uint32(es.committee.Size())
}

func (es *EpochState) InCommittee() bool {
	return es.inCommittee
}

func (es *EpochState) CommitteeIndex() int {
	return es.index
}

func (es *EpochState) GetValidatorByIndex(index int) ([]byte, *cmttypes.Validator) {
	return es.committee.GetByIndex(int32(index))
}

func (es *EpochState) GetMyself() *cmttypes.Validator {
	if es.inCommittee {
		_, v := es.committee.GetByIndex(int32(es.index))
		return v
	}
	return nil
}

func (es *EpochState) PrintCommittee() {
	fmt.Printf("* Current Committee (%d):\n%s\n\n", es.committee.Size(), peekCommittee(es.committee))
}

func peekCommittee(committee *cmttypes.ValidatorSet) string {
	s := make([]string, 0)
	if committee.Size() > 6 {
		for index, val := range committee.Validators[:3] {
			s = append(s, fmt.Sprintf("#%-4v %v", index, val.String()))
		}
		s = append(s, "...")
		for index, val := range committee.Validators[committee.Size()-3:] {
			s = append(s, fmt.Sprintf("#%-4v %v", index+committee.Size()-3, val.String()))
		}
	} else {
		for index, val := range committee.Validators {
			s = append(s, fmt.Sprintf("#%-2v %v", index, val.String()))
		}
	}
	return strings.Join(s, "\n")
}

// get the specific round proposer
func (es *EpochState) getRoundProposer(round uint32) *cmttypes.Validator {
	size := int(es.CommitteeSize())
	if size == 0 {
		es.logger.Info("can't get round proposer", "size", size, "round", round)
		return &cmttypes.Validator{}
	}
	_, v := es.committee.GetByIndex(int32(int(round) % size))
	return v
}

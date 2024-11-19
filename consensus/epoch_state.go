package consensus

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"

	"github.com/meterio/supernova/block"
	"github.com/meterio/supernova/chain"
	"github.com/meterio/supernova/types"
	"github.com/prysmaticlabs/prysm/v5/crypto/bls"
)

type EpochState struct {
	logger        *slog.Logger
	epoch         uint64
	startKBlockID types.Bytes32
	// committee calculated from last validator set and last nonce
	committee     *types.ValidatorSet
	inCommittee   bool
	index         int
	ipToName      map[string]string
	qcVoteManager *QCVoteManager
	tcVoteManager *TCVoteManager
	pending       bool
}

func NewEpochState(c *chain.Chain, leaf *block.Block, myPubKey bls.PublicKey) (*EpochState, error) {
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
	slog.Info("vset size", "size", vset.Size())
	committee := vset.SortWithNonce(kblk.Nonce())
	logger.Info("calc epoch state", "kblk", kblk.Number(), "vset", vset.Hex(), "committeeSize", committee.Size())

	// it is used for temp calculate committee set by a given nonce in the fly.
	// also return the committee

	inCommittee := false
	index := -1
	ipToName := make(map[string]string)
	if len(committee.Validators) > 0 {
		for i, val := range committee.Validators {
			if bytes.Equal(val.PubKey.Marshal(), myPubKey.Marshal()) {
				index = i
				inCommittee = true
			}
			ipToName[val.IP.String()] = val.Name
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
		ipToName:      ipToName,
		qcVoteManager: NewQCVoteManager(uint32(committee.Size())),
		tcVoteManager: NewTCVoteManager(uint32(committee.Size())),
		pending:       false,
	}, nil
}

func NewPendingEpochState(vset *types.ValidatorSet, myPubKey bls.PublicKey, curEpoch uint64) (*EpochState, error) {

	logger := slog.With("pkg", "es")
	if vset == nil {
		slog.Error("validator set is nil")
		return nil, errors.New("validator set is nil")
	}
	committee := vset
	logger.Info("calc epoch state", "vset", vset.Hex(), "committeeSize", committee.Size())

	// it is used for temp calculate committee set by a given nonce in the fly.
	// also return the committee

	inCommittee := false
	index := -1
	ipToName := make(map[string]string)
	if len(committee.Validators) > 0 {
		for i, val := range committee.Validators {
			if bytes.Equal(val.PubKey.Marshal(), myPubKey.Marshal()) {
				index = i
				inCommittee = true
			}
			ipToName[val.IP.String()] = val.Name
		}
	}

	return &EpochState{
		logger: logger,
		epoch:  curEpoch + 1,

		committee:   committee,
		inCommittee: inCommittee,
		index:       index,
		ipToName:    ipToName,
		pending:     true,
	}, nil
}

func (es *EpochState) AddQCVote(signerIndex uint32, round uint32, blockID types.Bytes32, sig []byte) *block.QuorumCert {
	v := es.committee.Validators[signerIndex]
	signature, err := bls.SignatureFromBytes(sig)
	if err != nil {
		es.logger.Warn("invalid signature", "sig", hex.EncodeToString(sig), "err", err)
		return nil
	}
	verified := signature.Verify(v.PubKey, blockID[:])
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
		es.logger.Warn("invalid signature", "sig", hex.EncodeToString(sig), "err", err)
		return nil
	}
	verified := signature.Verify(v.PubKey, hash[:])
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

func (es *EpochState) GetValidatorByIndex(index int) *types.Validator {
	return es.committee.GetByIndex(index)
}

func (es *EpochState) GetMyself() *types.Validator {
	if es.inCommittee {
		return es.committee.GetByIndex(es.index)
	}
	return nil
}

func (es *EpochState) PrintCommittee() {
	fmt.Printf("* Current Committee (%d):\n%s\n\n", es.committee.Size(), peekCommittee(es.committee))
}

func peekCommittee(committee *types.ValidatorSet) string {
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

// Assumptions to use this:
// myIndex is always 0 for proposer (or leader in consensus)
// indexes starts from 0
// 1st layer: 				0  (proposer)
// 2nd layer: 				[1, 2], [3, 4], [5, 6], [7, 8]
// 3rd layer (32 groups):   [9..] ...
func CalcRelayPeers(myIndex, size int) (peers []int) {
	peers = []int{}
	if myIndex > size {
		fmt.Println("Input wrong!!! myIndex > size")
		return
	}
	replicas := 8
	if size <= replicas {
		replicas = size
		for i := 1; i <= replicas; i++ {
			peers = append(peers, (myIndex+i)%size)
		}
	} else {
		replica1 := 8
		replica2 := 4
		replica3 := 2
		limit1 := int(math.Ceil(float64(size)/float64(replica1))) * 2
		limit2 := limit1 + int(math.Ceil(float64(size)/float64(replica2)))*4

		if myIndex < limit1 {
			base := myIndex * replica1
			for i := 1; i <= replica1; i++ {
				peers = append(peers, (base+i)%size)
			}
		} else if myIndex >= limit1 && myIndex < limit2 {
			base := replica1*limit1 + (myIndex-limit1)*replica2
			for i := 1; i <= replica2; i++ {
				peers = append(peers, (base+i)%size)
			}
		} else if myIndex >= limit2 {
			base := replica1*limit1 + (limit2-limit1)*replica2 + (myIndex-limit2)*replica3
			for i := 1; i <= replica3; i++ {
				peers = append(peers, (base+i)%size)
			}
		}
	}
	return

}

func (es *EpochState) GetRelayPeers(round uint32) []*ConsensusPeer {
	peers := make([]*ConsensusPeer, 0)
	size := es.committee.Size()
	myIndex := int(es.index)
	if size == 0 {
		return make([]*ConsensusPeer, 0)
	}
	rr := int(round % uint32(size))
	if myIndex >= rr {
		myIndex = myIndex - rr
	} else {
		myIndex = myIndex + size - rr
	}

	indexes := CalcRelayPeers(myIndex, size)
	for _, i := range indexes {
		index := i + rr
		if index >= size {
			index = index % size
		}
		member := es.committee.GetByIndex(index)
		name := member.Name
		peers = append(peers, NewConsensusPeer(name, member.IP.String()))
	}
	es.logger.Debug("get relay peers result", "myIndex", myIndex, "committeeSize", size, "round", round, "indexes", indexes)
	return peers
}

// get the specific round proposer
func (es *EpochState) getRoundProposer(round uint32) *types.Validator {
	size := int(es.CommitteeSize())
	if size == 0 {
		return &types.Validator{}
	}
	return es.committee.GetByIndex(int(round) % size)
}

func (es *EpochState) GetNameByIP(ip string) string {
	if name, ok := es.ipToName[ip]; ok {
		return name
	}
	return "unknown"
}

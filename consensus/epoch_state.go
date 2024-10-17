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
	chain *chain.Chain

	// committee calculated from last validator set and last nonce
	epoch         uint64
	committee     *types.ValidatorSet
	lastCommittee *types.ValidatorSet
	inCommittee   bool
	index         uint32
	ipToName      map[string]string

	qcVoteManager *QCVoteManager
	tcVoteManager *TCVoteManager

	logger *slog.Logger
}

func NewEpochState(c *chain.Chain, myPubKey bls.PublicKey) (*EpochState, error) {
	kblk, err := c.BestKBlock()
	if err != nil {
		return nil, err
	}
	if !kblk.IsKBlock() {
		return nil, errors.New("not kblock")
	}
	vset := c.GetNextValidatorSet(kblk.Number())
	if vset == nil {
		fmt.Println("COUL DNOT GET NXT VALIDATOR SET", kblk.Number(), kblk.NextValidatorHash())
		return nil, err
	}
	committee := vset.SortWithNonce(kblk.Nonce())

	lastCommittee := &types.ValidatorSet{Validators: make([]*types.Validator, 0)}
	if kblk.Number() > 0 {
		lastKBlk, err := c.GetTrunkBlock(kblk.LastKBlock())
		if err != nil {
			return nil, err
		}
		if !lastKBlk.IsKBlock() {
			return nil, errors.New("not kblock")
		}

		lastVSet := c.GetValidatorSet(kblk.LastKBlock())
		if lastVSet == nil {
			fmt.Println("COUL DNOT GET NXT VALIDATOR SET", kblk.Number())
			return nil, err
		}
		lastCommittee = vset.SortWithNonce(lastKBlk.Nonce())
	}

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
		epoch:         kblk.Epoch() + 1,
		committee:     committee,
		lastCommittee: lastCommittee,
		inCommittee:   inCommittee,
		index:         uint32(index),
		ipToName:      ipToName,

		qcVoteManager: NewQCVoteManager(uint32(committee.Size())),
		tcVoteManager: NewTCVoteManager(uint32(committee.Size())),

		logger: slog.With("pkg", "es"),
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

func (es *EpochState) CommitteeIndex() uint32 {
	return es.index
}

func (es *EpochState) GetValidatorByIndex(index uint32) *types.Validator {
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

	fmt.Printf("Last Committee (%d):\n%s\n\n", es.lastCommittee.Size(), peekCommittee(es.lastCommittee))
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
		member := es.committee.GetByIndex(uint32(index))
		name := member.Name
		peers = append(peers, NewConsensusPeer(name, member.IP.String()))
	}
	es.logger.Debug("get relay peers result", "myIndex", myIndex, "committeeSize", size, "round", round, "indexes", indexes)
	return peers
}

// get the specific round proposer
func (es *EpochState) getRoundProposer(round uint32) *types.Validator {
	size := es.CommitteeSize()
	if size == 0 {
		return &types.Validator{}
	}
	return es.committee.GetByIndex(round % size)
}

func (es *EpochState) GetNameByIP(ip string) string {
	if name, ok := es.ipToName[ip]; ok {
		return name
	}
	return "unknown"
}

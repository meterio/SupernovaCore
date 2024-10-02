package consensus

import (
	"fmt"
	"log/slog"

	"github.com/meterio/supernova/block"
	cmn "github.com/meterio/supernova/libs/common"
	"github.com/meterio/supernova/types"
	"github.com/prysmaticlabs/prysm/v5/crypto/bls"
)

type timeoutVoteKey struct {
	Epoch uint64
	Round uint32
}

type TCVoteManager struct {
	votes         map[timeoutVoteKey]map[uint32]*vote
	sealed        map[timeoutVoteKey]bool
	committeeSize uint32
	logger        *slog.Logger
}

func NewTCVoteManager(committeeSize uint32) *TCVoteManager {
	return &TCVoteManager{
		votes:         make(map[timeoutVoteKey]map[uint32]*vote),
		sealed:        make(map[timeoutVoteKey]bool), // sealed indicator
		committeeSize: committeeSize,
		logger:        slog.With("pkg", "tcman"),
	}
}

func (m *TCVoteManager) Size() uint32 {
	return m.committeeSize
}

func (m *TCVoteManager) AddVote(index uint32, epoch uint64, round uint32, sig []byte, hash [32]byte) *types.TimeoutCert {
	key := timeoutVoteKey{Epoch: epoch, Round: round}
	if _, existed := m.votes[key]; !existed {
		m.votes[key] = make(map[uint32]*vote)
	}

	if _, sealed := m.sealed[key]; sealed {
		return nil
	}

	blsSig, err := bls.SignatureFromBytes(sig)
	if err != nil {
		m.logger.Error("load signature failed", "err", err)
		return nil
	}
	m.votes[key][index] = &vote{Signature: blsSig, Hash: hash}

	voteCount := uint32(len(m.votes[key]))
	if block.MajorityTwoThird(voteCount, m.committeeSize) {
		m.seal(epoch, round)
		tc := m.Aggregate(epoch, round)
		m.logger.Info(
			fmt.Sprintf("%d/%d voted on E:%d, R:%d, TC formed.", voteCount, m.committeeSize, epoch, round))

		return tc
	} else {
		m.logger.Info(fmt.Sprintf("%d/%d voted on E:%d, R:%d ", voteCount, m.committeeSize, key.Epoch, key.Round))
	}
	return nil
}

func (m *TCVoteManager) Count(epoch uint64, round uint32) uint32 {
	key := timeoutVoteKey{Epoch: epoch, Round: round}
	return uint32(len(m.votes[key]))
}

func (m *TCVoteManager) seal(epoch uint64, round uint32) {
	key := timeoutVoteKey{Epoch: epoch, Round: round}
	m.sealed[key] = true
}

func (m *TCVoteManager) Aggregate(epoch uint64, round uint32) *types.TimeoutCert {
	m.seal(epoch, round)
	sigs := make([]bls.Signature, 0)
	key := timeoutVoteKey{Epoch: epoch, Round: round}

	bitArray := cmn.NewBitArray(int(m.committeeSize))
	var msgHash [32]byte
	for index, v := range m.votes[key] {
		sigs = append(sigs, v.Signature)
		bitArray.SetIndex(int(index), true)
		msgHash = v.Hash
	}
	aggrSig := bls.AggregateSignatures(sigs)

	return &types.TimeoutCert{
		Epoch:    epoch,
		Round:    round,
		BitArray: bitArray,
		MsgHash:  msgHash,
		AggSig:   aggrSig.Marshal(),
	}
}

func (m *TCVoteManager) CleanUpTo(epoch uint64) {
	m.logger.Info(fmt.Sprintf("clean tc votes up to epoch %v", epoch), "len", len(m.votes))
	for key, voteMap := range m.votes {
		if key.Epoch < epoch {
			for index, _ := range voteMap {
				delete(voteMap, index)
			}
			delete(m.votes, key)
			if _, exist := m.sealed[key]; exist {
				delete(m.sealed, key)
			}
		}
	}
	m.logger.Debug(fmt.Sprintf("after clean tc votes up to epoch %v", epoch), "len", len(m.votes))
}

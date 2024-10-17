package consensus

import (
	"fmt"
	"log/slog"

	"github.com/meterio/supernova/block"
	cmn "github.com/meterio/supernova/libs/common"
	"github.com/meterio/supernova/types"
	"github.com/prysmaticlabs/prysm/v5/crypto/bls"
)

type vote struct {
	Signature bls.Signature
	Hash      [32]byte
}

type voteKey struct {
	Round   uint32
	BlockID types.Bytes32
}

type QCVoteManager struct {
	votes         map[voteKey]map[uint32]*vote
	sealed        map[voteKey]bool
	committeeSize uint32
	logger        *slog.Logger
}

func NewQCVoteManager(committeeSize uint32) *QCVoteManager {
	return &QCVoteManager{
		votes:         make(map[voteKey]map[uint32]*vote),
		sealed:        make(map[voteKey]bool), // sealed indicator
		committeeSize: committeeSize,
		logger:        slog.With("pkg", "qcman"),
	}
}

func (m *QCVoteManager) Size() uint32 {
	return m.committeeSize
}

func (m *QCVoteManager) AddVote(index uint32, epoch uint64, round uint32, blockID types.Bytes32, sig []byte) *block.QuorumCert {
	key := voteKey{Round: round, BlockID: blockID}
	if _, existed := m.votes[key]; !existed {
		m.votes[key] = make(map[uint32]*vote)
	}

	if _, sealed := m.sealed[key]; sealed {
		return nil
	}

	if len(sig) <= 0 {
		return nil
	}
	blsSig, err := bls.SignatureFromBytes(sig)
	if err != nil {
		m.logger.Error("load qc signature failed", "err", err)
		return nil
	}
	m.votes[key][index] = &vote{Signature: blsSig, Hash: blockID}

	voteCount := uint32(len(m.votes[key]))
	if block.MajorityTwoThird(voteCount, m.committeeSize) {
		m.seal(round, blockID)
		qc := m.Aggregate(round, blockID, epoch)
		m.logger.Info(
			fmt.Sprintf("%d/%d voted on %s, R:%d, QC formed.", voteCount, m.committeeSize, blockID.ToBlockShortID(), round))
		return qc

	}
	m.logger.Info(fmt.Sprintf("%d/%d voted on %s, R:%d", voteCount, m.committeeSize, key.BlockID.ToBlockShortID(), key.Round))
	return nil
}

func (m *QCVoteManager) Count(round uint32, blockID types.Bytes32) uint32 {
	key := voteKey{Round: round, BlockID: blockID}
	return uint32(len(m.votes[key]))
}

func (m *QCVoteManager) seal(round uint32, blockID types.Bytes32) {
	key := voteKey{Round: round, BlockID: blockID}
	m.sealed[key] = true
}

func (m *QCVoteManager) Aggregate(round uint32, blockID types.Bytes32, epoch uint64) *block.QuorumCert {
	m.seal(round, blockID)
	sigs := make([]bls.Signature, 0)
	key := voteKey{Round: round, BlockID: blockID}

	bitArray := cmn.NewBitArray(int(m.committeeSize))
	for index, v := range m.votes[key] {
		sigs = append(sigs, v.Signature)
		bitArray.SetIndex(int(index), true)
	}
	aggrSig := bls.AggregateSignatures(sigs)

	return &block.QuorumCert{
		Epoch:    epoch,
		Round:    round,
		BlockID:  blockID,
		BitArray: bitArray,
		AggSig:   aggrSig.Marshal(),
	}
}

func (m *QCVoteManager) CleanUpTo(blockNum uint32) {
	m.logger.Info(fmt.Sprintf("clean qc votes up to block %v", blockNum), "len", len(m.votes))
	for key, voteMap := range m.votes {
		num := block.Number(key.BlockID)
		if num < blockNum {
			for index, _ := range voteMap {
				delete(voteMap, index)
			}
			delete(m.votes, key)
			if _, exist := m.sealed[key]; exist {
				delete(m.sealed, key)
			}
		}
	}
	m.logger.Debug(fmt.Sprintf("after clean qc votes up to block %v", blockNum), "len", len(m.votes))
}

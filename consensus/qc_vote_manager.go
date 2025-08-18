package consensus

import (
	"fmt"
	"log/slog"

	"github.com/OffchainLabs/prysm/v6/crypto/bls"
	v2 "github.com/cometbft/cometbft/api/cometbft/abci/v2"
	cmttypesv2 "github.com/cometbft/cometbft/api/cometbft/types/v2"
	cmttypes "github.com/cometbft/cometbft/v2/types"
	"github.com/meterio/supernova/block"
	cmn "github.com/meterio/supernova/libs/common"
	"github.com/meterio/supernova/types"
)

type voteValue struct {
	Signature bls.Signature
	VoteInfo  v2.ExtendedVoteInfo
}

type voteKey struct {
	Round   uint32
	BlockID types.Bytes32
}

type QCVoteManager struct {
	votes         map[voteKey]map[uint32]*voteValue
	sealed        map[voteKey]bool
	committeeSize uint32
	logger        *slog.Logger
}

func NewQCVoteManager(committeeSize uint32) *QCVoteManager {
	return &QCVoteManager{
		votes:         make(map[voteKey]map[uint32]*voteValue),
		sealed:        make(map[voteKey]bool), // sealed indicator
		committeeSize: committeeSize,
		logger:        slog.With("pkg", "qcman"),
	}
}

func (m *QCVoteManager) Size() uint32 {
	return m.committeeSize
}

func (m *QCVoteManager) AddVerifiedVote(index uint32, validator *cmttypes.Validator, epoch uint64, round uint32, blockID types.Bytes32, blsSig bls.Signature, voteExtension, extensionSignature, nonRpVoteExtension, nonRpExtensionSignature []byte) (*block.QuorumCert, *v2.ExtendedCommitInfo) {
	key := voteKey{Round: round, BlockID: blockID}
	if _, existed := m.votes[key]; !existed {
		m.votes[key] = make(map[uint32]*voteValue)
	}

	if _, sealed := m.sealed[key]; sealed {
		return nil, nil
	}

	m.votes[key][index] = &voteValue{
		Signature: blsSig,
		VoteInfo: v2.ExtendedVoteInfo{
			Validator:               cmttypes.TM2PB.Validator(validator),
			BlockIdFlag:             cmttypesv2.BlockIDFlagCommit,
			VoteExtension:           voteExtension,
			ExtensionSignature:      extensionSignature,
			NonRpVoteExtension:      nonRpVoteExtension,
			NonRpExtensionSignature: nonRpExtensionSignature,
		},
	}

	voteCount := uint32(len(m.votes[key]))
	if block.MajorityTwoThird(voteCount, m.committeeSize) {
		m.seal(round, blockID)
		qc, commitInfo := m.Aggregate(round, blockID, epoch)
		m.logger.Info(
			fmt.Sprintf("%d/%d voted on %s, R:%d, QC formed.", voteCount, m.committeeSize, blockID.ToBlockShortID(), round))
		return qc, commitInfo

	}
	m.logger.Info(fmt.Sprintf("%d/%d voted on %s, R:%d", voteCount, m.committeeSize, key.BlockID.ToBlockShortID(), key.Round))
	return nil, nil
}

func (m *QCVoteManager) Count(round uint32, blockID types.Bytes32) uint32 {
	key := voteKey{Round: round, BlockID: blockID}
	return uint32(len(m.votes[key]))
}

func (m *QCVoteManager) seal(round uint32, blockID types.Bytes32) {
	key := voteKey{Round: round, BlockID: blockID}
	m.sealed[key] = true
}

func (m *QCVoteManager) Aggregate(round uint32, blockID types.Bytes32, epoch uint64) (*block.QuorumCert, *v2.ExtendedCommitInfo) {
	m.seal(round, blockID)
	sigs := make([]bls.Signature, 0)
	key := voteKey{Round: round, BlockID: blockID}

	bitArray := cmn.NewBitArray(int(m.committeeSize))
	votes := make([]v2.ExtendedVoteInfo, 0)
	for index, v := range m.votes[key] {
		sigs = append(sigs, v.Signature)
		bitArray.SetIndex(int(index), true)
		votes = append(votes, v.VoteInfo)
	}
	aggrSig := bls.AggregateSignatures(sigs)

	qc := &block.QuorumCert{
		Epoch:    epoch,
		Round:    round,
		BlockID:  blockID,
		BitArray: bitArray,
		AggSig:   aggrSig.Marshal(),
	}

	return qc, &v2.ExtendedCommitInfo{Round: int32(round), Votes: votes}
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

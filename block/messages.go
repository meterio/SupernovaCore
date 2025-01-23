// Copyright (c) 2020 The Meter.io developers
// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying

// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package block

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	cmtcrypto "github.com/cometbft/cometbft/crypto"
	"github.com/ethereum/go-ethereum/crypto/blake2b"
	"github.com/ethereum/go-ethereum/rlp"
	cmn "github.com/meterio/supernova/libs/common"
	"github.com/meterio/supernova/types"
	"github.com/prysmaticlabs/prysm/v5/crypto/bls"
	amino "github.com/tendermint/go-amino"
)

const (
	//maxMsgSize = 1048576 // 1MB;
	// set as 1184 * 1024
	maxMsgSize = 1300000 // gasLimit 20000000 generate, 1024+1024 (1048576) + sizeof(QC) + sizeof(committee)...

)

// ConsensusMessage is a message that can be sent and received on the Reactor
type ConsensusMessage interface {
	GetSignerIndex() uint32
	GetEpoch() uint64
	GetType() string
	GetRound() uint32

	String() string
	GetMsgHash() types.Bytes32
	SetMsgSignature(signature []byte)
	VerifyMsgSignature(cmtPubKey cmtcrypto.PubKey) bool
}

var _ ConsensusMessage = &PMProposalMessage{}
var _ ConsensusMessage = &PMVoteMessage{}
var _ ConsensusMessage = &PMTimeoutMessage{}
var _ ConsensusMessage = &PMQueryMessage{}

func RegisterConsensusMessages(cdc *amino.Codec) {
	cdc.RegisterInterface((*ConsensusMessage)(nil), nil)

	cdc.RegisterConcrete(&PMProposalMessage{}, "meterio/PMProposal", nil)
	cdc.RegisterConcrete(&PMVoteMessage{}, "meterio/PMVote", nil)
	cdc.RegisterConcrete(&PMTimeoutMessage{}, "meterio/PMTimeout", nil)
	cdc.RegisterConcrete(&PMQueryMessage{}, "meterio/PMQuery", nil)
}

var (
	cdc = amino.NewCodec()
)

func init() {
	RegisterConsensusMessages(cdc)
}

func DecodeMsg(raw []byte) (ConsensusMessage, error) {
	var msg ConsensusMessage
	if len(raw) > maxMsgSize {
		return msg, fmt.Errorf("msg exceeds max size (%d > %d)", len(raw), maxMsgSize)
	}
	err := cdc.UnmarshalBinaryBare(raw, &msg)
	return msg, err
}

func EncodeMsg(msg ConsensusMessage) ([]byte, error) {
	raw := cdc.MustMarshalBinaryBare(msg)
	if len(raw) > maxMsgSize {
		slog.Error("consensus msg exceeds max size", "raw", len(raw), "maxSize", maxMsgSize)
		return make([]byte, 0), errors.New("msg exceeds max size")
	}
	return raw, nil
}

func verifyMsgSignature(cmtPubkey cmtcrypto.PubKey, msg []byte, signature []byte) bool {
	blsPubKey, err := cmn.BlsPublicKeyFromCmtPubKey(cmtPubkey)
	if err != nil {
		return false
	}
	sig, err := bls.SignatureFromBytes(signature)
	if err != nil {
		return false
	}
	return sig.Verify(blsPubKey, msg)
}

// PMProposalMessage is sent when a new block is proposed
type PMProposalMessage struct {
	Timestamp   time.Time
	Epoch       uint64
	Round       uint32
	RawBlock    []byte
	TimeoutCert *types.TimeoutCert

	MsgSignature []byte

	// cached
	decodedBlock *Block
}

func (m *PMProposalMessage) GetEpoch() uint64 {
	return m.Epoch
}

func (m *PMProposalMessage) GetSignerIndex() uint32 {
	return m.DecodeBlock().ProposerIndex()
}

func (m *PMProposalMessage) GetType() string {
	return "PMProposal"
}

func (m *PMProposalMessage) GetRound() uint32 {
	return m.Round
}

// GetMsgHash computes hash of all header fields excluding signature.
func (m *PMProposalMessage) GetMsgHash() types.Bytes32 {
	data := []interface{}{m.Timestamp, m.Epoch, m.Round, m.RawBlock}
	if m.TimeoutCert != nil {
		data = append(data, m.TimeoutCert)
	}
	bs, err := rlp.EncodeToBytes(data)
	if err != nil {
		slog.Error("RLP Encode Error", "err", err)
	}
	return blake2b.Sum256(bs)
}

func (m *PMProposalMessage) DecodeBlock() *Block {
	if m.decodedBlock != nil {
		return m.decodedBlock
	}
	blk, err := BlockDecodeFromBytes(m.RawBlock)
	if err != nil {
		fmt.Println("decode error: ", err)
		m.decodedBlock = nil
		return nil
	}
	m.decodedBlock = blk
	return blk
}

// String returns a string representation.
func (m *PMProposalMessage) String() string {
	blk := m.DecodeBlock()
	tcStr := ""
	if m.TimeoutCert != nil {
		tcStr = "TC:" + m.TimeoutCert.String()
	}
	blkStr := blk.Oneliner()
	return fmt.Sprintf("Proposal(E%v.R%v) %v %v", m.Epoch, m.Round, blkStr, tcStr)
}

func (m *PMProposalMessage) SetMsgSignature(msgSignature []byte) {
	m.MsgSignature = msgSignature
}

func (m *PMProposalMessage) VerifyMsgSignature(cmtPubKey cmtcrypto.PubKey) bool {
	return verifyMsgSignature(cmtPubKey, m.GetMsgHash().Bytes(), m.MsgSignature)
}

// PMVoteMessage is sent when voting for a proposal (or lack thereof).
type PMVoteMessage struct {
	Timestamp     time.Time
	Epoch         uint64
	SignerIndex   uint32
	VoteRound     uint32
	VoteBlockID   types.Bytes32
	VoteSignature []byte //bls.Signature

	MsgSignature []byte
}

func (m *PMVoteMessage) GetSignerIndex() uint32 {
	return m.SignerIndex
}

func (m *PMVoteMessage) GetEpoch() uint64 {
	return m.Epoch
}

func (m *PMVoteMessage) GetType() string {
	return "PMVote"
}

func (m *PMVoteMessage) GetRound() uint32 {
	return m.VoteRound
}

// GetMsgHash computes hash of all header fields excluding signature.
func (m *PMVoteMessage) GetMsgHash() types.Bytes32 {
	bs, err := rlp.EncodeToBytes([]interface{}{
		m.Timestamp,
		m.Epoch,
		m.SignerIndex,
		m.VoteRound,
		m.VoteBlockID,
		m.VoteSignature,
	})
	if err != nil {
		slog.Error("RLP Encode Error", "err", err)
	}
	return blake2b.Sum256(bs)
}

// String returns a string representation.
func (m *PMVoteMessage) String() string {
	return fmt.Sprintf("Vote(R:%d) %v",
		m.VoteRound, m.VoteBlockID.ToBlockShortID())
}

func (m *PMVoteMessage) SetMsgSignature(msgSignature []byte) {
	m.MsgSignature = msgSignature
}

func (m *PMVoteMessage) VerifyMsgSignature(cmtPubKey cmtcrypto.PubKey) bool {
	return verifyMsgSignature(cmtPubKey, m.GetMsgHash().Bytes(), m.MsgSignature)
}

// PMTimeoutMessage is sent to the next leader in these two senarios
type PMTimeoutMessage struct {
	Timestamp   time.Time
	Epoch       uint64
	SignerIndex uint32
	WishRound   uint32

	// local QCHigh
	QCHigh []byte

	// timeout vote
	WishVoteHash [32]byte
	WishVoteSig  []byte // signature

	// last vote for proposal
	LastVoteRound     uint32
	LastVoteBlockID   types.Bytes32
	LastVoteSignature []byte

	MsgSignature []byte

	// cached
	decodedQCHigh *QuorumCert
}

func (m *PMTimeoutMessage) GetSignerIndex() uint32 {
	return m.SignerIndex
}

func (m *PMTimeoutMessage) GetEpoch() uint64 {
	return m.Epoch
}

func (m *PMTimeoutMessage) GetType() string {
	return "PMTimeout"
}

func (m *PMTimeoutMessage) GetRound() uint32 {
	return m.WishRound
}

// GetMsgHash computes hash of all header fields excluding signature.
func (m *PMTimeoutMessage) GetMsgHash() types.Bytes32 {
	bs, err := rlp.EncodeToBytes([]interface{}{
		m.Timestamp,
		m.Epoch,
		m.SignerIndex,
		m.WishRound,
		m.QCHigh,
		m.WishVoteHash,
		m.WishVoteSig,
		m.LastVoteRound,
		m.LastVoteBlockID,
		m.LastVoteSignature,
	})
	if err != nil {
		slog.Error("RLP Encode Error", "err", err)
	}
	return blake2b.Sum256(bs)
}

func (m *PMTimeoutMessage) DecodeQCHigh() *QuorumCert {
	if m.decodedQCHigh != nil {
		return m.decodedQCHigh
	}
	qcHigh, err := QCDecodeFromBytes(m.QCHigh)
	if err != nil {
		m.decodedQCHigh = nil
		return nil
	}
	m.decodedQCHigh = qcHigh
	return qcHigh
}

// String returns a string representation.
func (m *PMTimeoutMessage) String() string {
	qcHigh := m.DecodeQCHigh()
	s := fmt.Sprintf("Timeout(E%v.WR%d)", m.Epoch, m.WishRound)
	if qcHigh != nil {
		s = s + " " + fmt.Sprintf("QCHigh(E%d.R%d)", qcHigh.Epoch, qcHigh.Round)
	}
	if len(m.LastVoteSignature) > 0 {
		s = s + " " + fmt.Sprintf("LastVote(R:%d, %v)", m.LastVoteRound, m.LastVoteBlockID.ToBlockShortID())
	}
	return s
}

func (m *PMTimeoutMessage) SetMsgSignature(msgSignature []byte) {
	m.MsgSignature = msgSignature
}

func (m *PMTimeoutMessage) VerifyMsgSignature(cmtPubKey cmtcrypto.PubKey) bool {
	return verifyMsgSignature(cmtPubKey, m.GetMsgHash().Bytes(), m.MsgSignature)
}

type PMQueryMessage struct {
	Timestamp   time.Time
	Epoch       uint64
	SignerIndex uint32

	LastCommitted types.Bytes32

	MsgSignature []byte
}

func (m *PMQueryMessage) GetSignerIndex() uint32 {
	return m.SignerIndex
}

func (m *PMQueryMessage) GetEpoch() uint64 {
	return m.Epoch
}

func (m *PMQueryMessage) GetType() string {
	return "PMQuery"
}

func (m *PMQueryMessage) GetRound() uint32 {
	return uint32(0)
}

// GetMsgHash computes hash of all header fields excluding signature.
func (m *PMQueryMessage) GetMsgHash() types.Bytes32 {
	bs, err := rlp.EncodeToBytes([]interface{}{
		m.Timestamp,
		m.Epoch,
		m.SignerIndex,
		m.LastCommitted,
	})
	if err != nil {
		slog.Error("RLP Encode Error", "err", err)
	}
	return blake2b.Sum256(bs)
}

// String returns a string representation.
func (m *PMQueryMessage) String() string {
	return fmt.Sprintf("Query LastCommitted:%v", m.LastCommitted.ToBlockShortID())
}

func (m *PMQueryMessage) SetMsgSignature(msgSignature []byte) {
	m.MsgSignature = msgSignature
}

func (m *PMQueryMessage) VerifyMsgSignature(cmtPubKey cmtcrypto.PubKey) bool {
	return verifyMsgSignature(cmtPubKey, m.GetMsgHash().Bytes(), m.MsgSignature)
}

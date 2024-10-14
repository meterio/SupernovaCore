// Copyright (c) 2020 The Meter.io developers
// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying

// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package block

import (
	"crypto/sha256"
	"fmt"
	"io"
	"log/slog"

	"github.com/ethereum/go-ethereum/rlp"
	cmn "github.com/meterio/supernova/libs/common"
)

type QuorumCert struct {
	Height uint32
	Round  uint32
	Epoch  uint64

	MsgHash  [32]byte // [][32]byte
	AggSig   []byte
	BitArray *cmn.BitArray
}

func (qc *QuorumCert) String() string {
	if qc != nil {
		// bitArray := strings.ReplaceAll(qc.VoterBitArrayStr, "\"", "")
		voted := qc.BitArray.CountYes()
		unvoted := qc.BitArray.CountNo()
		return fmt.Sprintf("QC(#%v, R:%v, E:%v, BitArray:(%v/%v), AggSig:len(%v))",
			qc.Height, qc.Round, qc.Epoch, voted, (voted + unvoted), len(qc.AggSig))
	}
	return "QC(nil)"
}

func (qc *QuorumCert) CompactString() string {
	if qc != nil {
		return fmt.Sprintf("QC(#%v,R:%v,E:%v)",
			qc.Height, qc.Round, qc.Epoch)
	}
	return "QC(nil)"
}

func (qc *QuorumCert) ToBytes() []byte {
	bytes, err := rlp.EncodeToBytes(qc)
	if err != nil {
		slog.Error("qc to bytes error", "err", err)
	}
	return bytes
}

// EncodeRLP implements rlp.Encoder.
func (qc *QuorumCert) EncodeRLP(w io.Writer) error {
	if qc == nil {
		w.Write([]byte{})
		return nil
	}
	return rlp.Encode(w, []interface{}{
		qc.Height,
		qc.Round,
		qc.Epoch,
		qc.MsgHash,
		qc.AggSig,
		qc.BitArray,
	})
}

func (qc *QuorumCert) Hash() []byte {
	b, _ := rlp.EncodeToBytes(qc)
	hash := sha256.Sum256(b)
	return hash[:]
}

// DecodeRLP implements rlp.Decoder.
func (qc *QuorumCert) DecodeRLP(s *rlp.Stream) error {
	payload := struct {
		Height   uint32
		Round    uint32
		Epoch    uint64
		MsgHash  [32]byte
		AggSig   []byte
		BitArray *cmn.BitArray
	}{}

	if err := s.Decode(&payload); err != nil {
		return err
	}

	*qc = QuorumCert{
		Height:   payload.Height,
		Round:    payload.Round,
		Epoch:    payload.Epoch,
		MsgHash:  payload.MsgHash,
		AggSig:   payload.AggSig,
		BitArray: payload.BitArray,
	}
	return nil
}

func GenesisEscortQC(b *Block) *QuorumCert {
	return &QuorumCert{Height: 0, Round: 0, Epoch: 0, MsgHash: b.VotingHash(), BitArray: cmn.NewBitArray(1)}
}

// --------------
func QCEncodeBytes(qc *QuorumCert) []byte {
	blockBytes, _ := rlp.EncodeToBytes(qc)
	return blockBytes
}

func QCDecodeFromBytes(bytes []byte) (*QuorumCert, error) {
	qc := QuorumCert{}
	err := rlp.DecodeBytes(bytes, &qc)
	return &qc, err
}

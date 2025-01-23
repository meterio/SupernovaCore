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
	"github.com/meterio/supernova/types"
)

type QuorumCert struct {
	Epoch   uint64
	Round   uint32
	BlockID types.Bytes32

	AggSig   []byte
	BitArray *cmn.BitArray
}

func (qc *QuorumCert) String() string {
	if qc != nil {
		voted := qc.BitArray.CountYes()
		unvoted := qc.BitArray.CountNo()
		return fmt.Sprintf("QC(%v, E%v.R%v, BitArray:(%v/%v), AggSig:len(%v))",
			qc.BlockID.ToBlockShortID(), qc.Epoch, qc.Round, voted, (voted + unvoted), len(qc.AggSig))
	}
	return "QC(nil)"
}

func (qc *QuorumCert) CompactString() string {
	if qc != nil {
		return fmt.Sprintf("QC(%v, E%v.R%v, voted:%v/%v)",
			qc.BlockID.ToBlockShortID(), qc.Epoch, qc.Round, qc.BitArray.CountYes(), qc.BitArray.Count())
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
		qc.Epoch,
		qc.Round,
		qc.BlockID,
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
		Epoch    uint64
		Round    uint32
		BlockID  [32]byte
		AggSig   []byte
		BitArray *cmn.BitArray
	}{}

	if err := s.Decode(&payload); err != nil {
		return err
	}

	*qc = QuorumCert{
		Epoch:    payload.Epoch,
		Round:    payload.Round,
		BlockID:  payload.BlockID,
		AggSig:   payload.AggSig,
		BitArray: payload.BitArray,
	}
	return nil
}

func (qc *QuorumCert) Number() uint32 {
	return Number(qc.BlockID)
}

func GenesisEscortQC(b *Block, vsetCount int) *QuorumCert {
	return &QuorumCert{Epoch: 0, Round: 0, BlockID: b.ID(), BitArray: cmn.NewBitArray(vsetCount)}
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

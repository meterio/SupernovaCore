// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package block

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"strings"
	"sync/atomic"
	"time"

	cmtbytes "github.com/cometbft/cometbft/libs/bytes"
	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/ethereum/go-ethereum/rlp"
	cmn "github.com/meterio/supernova/libs/common"
	"github.com/meterio/supernova/types"
	"github.com/prysmaticlabs/prysm/v5/crypto/bls"
)

const (
	DoubleSign = int(1)
)

// Block is an immutable block type.
type Block struct {
	BlockHeader *Header
	Txs         types.Transactions
	QC          *QuorumCert
	cache       struct {
		size atomic.Uint64
	}
}

// Body defines body of a block.
type Body struct {
	Txs types.Transactions
}

// Create new committee Info
// Compose compose a block with all needed components
// Note: This method is usually to recover a block by its portions, and the TxsRoot is not verified.
// To build up a block, use a Builder.
func Compose(header *Header, txs types.Transactions) *Block {
	return &Block{
		BlockHeader: header,
		Txs:         append(types.Transactions(nil), txs...),
	}
}

func MajorityTwoThird(voterNum, committeeSize uint32) bool {
	if committeeSize < 1 {
		return false
	}
	// Examples
	// committeeSize= 1 twoThirds= 1
	// committeeSize= 2 twoThirds= 2
	// committeeSize= 3 twoThirds= 2
	// committeeSize= 4 twoThirds= 3
	// committeeSize= 5 twoThirds= 4
	// committeeSize= 6 twoThirds= 4
	twoThirds := math.Ceil(float64(committeeSize) * 2 / 3)
	return float64(voterNum) >= twoThirds
}

func (b *Block) VerifyQC(escortQC *QuorumCert, blsMaster *types.BlsMaster, committee *cmttypes.ValidatorSet) (bool, error) {
	committeeSize := uint32(committee.Size())
	if b == nil {
		// decode block to get qc
		// slog.Error("can not decode block", err)
		return false, errors.New("block empty")
	}

	// genesis/first block does not have qc
	if strings.EqualFold(b.ID().String(), escortQC.BlockID.String()) && (b.Number() == 0 || b.Number() == 1) {
		return true, nil
	}

	// check vote count
	voteCount := escortQC.BitArray.Count()
	if !MajorityTwoThird(uint32(voteCount), committeeSize) {
		return false, fmt.Errorf("not enough votes (%d/%d)", voteCount, committeeSize)
	}

	pubkeys := make([]bls.PublicKey, 0)
	for index, v := range committee.Validators {
		if v.PubKey.Type() == "bls12_381" {
			if escortQC.BitArray.GetIndex(index) {
				cmnPubkey, err := cmn.PublicKeyFromBytes(v.PubKey.Bytes())
				if err != nil {
					// FIXME: implement this
					panic("unsupported pubkey type")
				}
				pubkeys = append(pubkeys, cmnPubkey)
			}
		}
	}
	sig, err := bls.SignatureFromBytes(escortQC.AggSig)
	if err != nil {
		return false, errors.New("invalid aggregate signature:" + err.Error())
	}
	start := time.Now()
	valid := sig.FastAggregateVerify(pubkeys, escortQC.BlockID)
	slog.Debug("verified QC", "elapsed", types.PrettyDuration(time.Since(start)))

	return valid, err
}

// Header returns the block header.
func (b *Block) Header() *Header {
	return b.BlockHeader
}

func (b *Block) ID() types.Bytes32 {
	return b.BlockHeader.ID()
}

func (b *Block) CompactString() string {
	if b != nil {
		prefix := "b"
		if b.IsKBlock() {
			prefix = "k"
		}
		return fmt.Sprintf("%v#%v..%x", prefix, b.Number(), b.ID().Bytes()[28:])
	}
	return ""
}

// ParentID returns id of parent block.
func (b *Block) ParentID() types.Bytes32 {
	return b.BlockHeader.ParentID
}

// LastBlocID returns id of parent block.
func (b *Block) LastKBlock() uint32 {
	return b.BlockHeader.LastKBlock
}

// Number returns sequential number of this block.
func (b *Block) Number() uint32 {
	// inferred from parent id
	return b.BlockHeader.Number()
}

func (b *Block) ValidatorsHash() cmtbytes.HexBytes {
	return b.BlockHeader.ValidatorsHash
}

func (b *Block) NextValidatorsHash() cmtbytes.HexBytes {
	return b.BlockHeader.NextValidatorsHash
}

func (b *Block) IsKBlock() bool {
	return !bytes.Equal(b.ValidatorsHash(), b.NextValidatorsHash())
}

// Timestamp returns timestamp of this block.
func (b *Block) Timestamp() uint64 {
	return b.BlockHeader.Timestamp
}

func (b *Block) AppHash() cmtbytes.HexBytes {
	return b.BlockHeader.AppHash
}

// TxsRoot returns merkle root of txs contained in this block.
func (b *Block) TxsRoot() cmtbytes.HexBytes {
	return b.BlockHeader.TxsRoot
}

// Transactions returns a copy of transactions.
func (b *Block) Transactions() types.Transactions {
	return append(types.Transactions(nil), b.Txs...)
}

// Body returns body of a block.
func (b *Block) Body() *Body {
	return &Body{append(make([]cmttypes.Tx, 0), b.Txs...)}
}

// EncodeRLP implements rlp.Encoder.
func (b *Block) EncodeRLP(w io.Writer) error {
	if b == nil {
		w.Write([]byte{})
		return nil
	}
	return rlp.Encode(w, []interface{}{
		b.BlockHeader,
		b.Txs,
		b.QC,
	})
}

// DecodeRLP implements rlp.Decoder.
func (b *Block) DecodeRLP(s *rlp.Stream) error {
	_, size, err := s.Kind()
	if err != nil {
		slog.Error("decode rlp error", "err", err)
	}

	payload := struct {
		Header Header
		Txs    types.Transactions
		QC     *QuorumCert
	}{}

	if err := s.Decode(&payload); err != nil {
		return err
	}

	*b = Block{
		BlockHeader: &payload.Header,
		Txs:         payload.Txs,
		QC:          payload.QC,
	}
	b.cache.size.Store(rlp.ListSize(size))
	return nil
}

type writeCounter uint64

func (c *writeCounter) Write(b []byte) (int, error) {
	*c += writeCounter(len(b))
	return len(b), nil
}

// Size returns the true RLP encoded storage size of the block, either by encoding
// and returning it, or returning a previously cached value.
func (b *Block) Size() uint64 {
	if size := b.cache.size.Load(); size > 0 {
		return size
	}
	c := writeCounter(0)
	rlp.Encode(&c, b)
	b.cache.size.Store(uint64(c))
	return uint64(c)
}

func (b *Block) String() string {
	s := fmt.Sprintf(`%v(%v) %v {
  BlockHeader: %v
  QuorumCert:  %v
  Txs:         %v`, "Block", b.BlockHeader.Number(), b.ID(), b.BlockHeader, b.QC, b.Txs)

	s += "\n}"
	return s
}

func (b *Block) Oneliner() string {
	txs := ""
	if len(b.Transactions()) > 0 {
		txs = fmt.Sprintf(", txs:%v", len(b.Transactions()))
	}
	return fmt.Sprintf("%v[ %v%v ]",
		b.CompactString(), b.QC.CompactString(), txs)
}

// -----------------
func (b *Block) SetQC(qc *QuorumCert) *Block {
	b.QC = qc
	return b
}
func (b *Block) GetQC() *QuorumCert {
	return b.QC
}

// if the block is the first mblock, get epoch from committee
// otherwise get epoch from QC
func (b *Block) Epoch() uint64 {
	return b.QC.Epoch
}

func (b *Block) ToBytes() []byte {
	bytes, err := rlp.EncodeToBytes(b)
	if err != nil {
		slog.Error("tobytes error", "err", err)
	}

	return bytes
}

func (b *Block) Nonce() uint64 {
	return b.BlockHeader.Nonce
}

func (b *Block) ProposerIndex() uint32 {
	return b.BlockHeader.ProposerIndex
}

// --------------

func BlockDecodeFromBytes(bytes []byte) (*Block, error) {
	blk := Block{}
	err := rlp.DecodeBytes(bytes, &blk)
	//slog.Error("decode failed", err)
	return &blk, err
}

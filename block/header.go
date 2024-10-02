// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package block

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"log/slog"

	// "io"
	"sync/atomic"

	cmtbytes "github.com/cometbft/cometbft/libs/bytes"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/meterio/supernova/meter"
)

type BlockType uint32

const (
	KBlockType BlockType = 1
	MBlockType BlockType = 2
	SBlockType BlockType = 255 //special message to stop pacemake, not a block
)

// Header contains almost all information about a block, except block body.
// It's immutable.
type Header struct {
	ParentID         meter.Bytes32
	Timestamp        uint64
	BlockType        BlockType
	Proposer         common.Address
	TxsRoot          cmtbytes.HexBytes
	EvidenceDataRoot meter.Bytes32 // deprecated, saved just for compatibility

	LastKBlockHeight uint32
	Nonce            uint64 // the last of the pow block

	QCHash            cmtbytes.HexBytes // hash of QC
	ValidatorHash     cmtbytes.HexBytes // hash of validator set
	NextValidatorHash cmtbytes.HexBytes // hash of next validator set

	Signature []byte

	cache struct {
		signingHash atomic.Value
		signer      atomic.Value
		id          atomic.Value
	}
}

// Number returns sequential number of this block.
func (h *Header) Number() uint32 {
	// inferred from parent id
	if bytes.Equal(h.ParentID.Bytes(), meter.Bytes32{}.Bytes()) {
		return 0
	}
	return Number(h.ParentID) + 1
}

// ID computes id of block.
// The block ID is defined as: blockNumber + hash(signingHash, signer)[4:].
func (h *Header) ID() (id meter.Bytes32) {
	if cached := h.cache.id.Load(); cached != nil {
		return cached.(meter.Bytes32)
	}
	defer func() {
		// overwrite first 4 bytes of block hash to block number.
		binary.BigEndian.PutUint32(id[:], h.Number())
		h.cache.id.Store(id)
	}()

	signer, err := h.Signer()
	if err != nil {
		return
	}

	hw := meter.NewBlake2b()
	hw.Write(h.SigningHash().Bytes())
	hw.Write(signer.Bytes())
	hw.Sum(id[:0])

	return
}

// SigningHash computes hash of all header fields excluding signature.
func (h *Header) SigningHash() (hash meter.Bytes32) {
	if cached := h.cache.signingHash.Load(); cached != nil {
		return cached.(meter.Bytes32)
	}
	defer func() { h.cache.signingHash.Store(hash) }()

	hw := meter.NewBlake2b()
	err := rlp.Encode(hw, []interface{}{
		h.ParentID,
		h.Timestamp,
		h.BlockType,

		h.TxsRoot,
		h.EvidenceDataRoot,

		h.LastKBlockHeight,
		h.Nonce,

		h.QCHash,
		h.ValidatorHash,
		h.NextValidatorHash,
	})
	if err != nil {
		slog.Error("could not calculate signing hash", "err", err)
	}
	hw.Sum(hash[:0])
	return
}

// Signer extract signer of the block from signature.
func (h *Header) Signer() (signer common.Address, err error) {
	if h.Number() == 0 {
		// special case for genesis block
		return common.Address{}, nil
	}

	if cached := h.cache.signer.Load(); cached != nil {
		return cached.(common.Address), nil
	}
	defer func() {
		if err == nil {
			h.cache.signer.Store(signer)
		}
	}()

	pub, err := crypto.SigToPub(h.SigningHash().Bytes(), h.Signature)
	if err != nil {
		return common.Address{}, err
	}

	signer = common.Address(crypto.PubkeyToAddress(*pub))
	return
}

func (h *Header) WithSignature(sig []byte) *Header {
	h.Signature = sig
	return h
}

func (h *Header) String() string {
	var signerStr string
	if signer, err := h.Signer(); err != nil {
		signerStr = "N/A"
	} else {
		signerStr = signer.String()
	}

	return fmt.Sprintf(`
    ParentID:     %v
    Timestamp:    %v
    LastKBlock:   %v
    TxsRoot:      %v
    Signer:       %v
    Nonce:        %v
    Signature:    0x%x`, h.ParentID, h.Timestamp, h.LastKBlockHeight, h.TxsRoot, signerStr, h.Nonce,
		h.Signature)
}

// Number extract block number from block id.
func Number(blockID meter.Bytes32) uint32 {
	// first 4 bytes are over written by block number (big endian).
	return binary.BigEndian.Uint32(blockID[:])
}

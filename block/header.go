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
	"github.com/ethereum/go-ethereum/crypto/blake2b"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/meterio/supernova/types"
)

// Header contains almost all information about a block, except block body.
// It's immutable.
type Header struct {
	ParentID      types.Bytes32
	Timestamp     uint64
	ProposerIndex uint32
	TxsRoot       cmtbytes.HexBytes
	LastKBlock    uint32
	Nonce         uint64 //

	QCHash             cmtbytes.HexBytes // hash of QC
	ValidatorsHash     cmtbytes.HexBytes // hash of validator set
	NextValidatorsHash cmtbytes.HexBytes // hash of next validator set
	AppHash            cmtbytes.HexBytes //

	cache struct {
		signingHash atomic.Value
		signer      atomic.Value
		id          atomic.Value
	}
}

// Number returns sequential number of this block.
func (h *Header) Number() uint32 {
	// inferred from parent id
	if bytes.Equal(h.ParentID.Bytes(), types.Bytes32{}.Bytes()) {
		return 0
	}
	return Number(h.ParentID) + 1
}

// ID computes id of block.
// The block ID is defined as: blockNumber + hash(signingHash, proposerIndex)[4:].
func (h *Header) ID() (id types.Bytes32) {
	if cached := h.cache.id.Load(); cached != nil {
		return cached.(types.Bytes32)
	}
	defer func() {
		// overwrite first 4 bytes of block hash to block number.
		binary.BigEndian.PutUint32(id[:], h.Number())
		h.cache.id.Store(id)
	}()
	var proposerIndex [4]byte
	binary.BigEndian.PutUint32(proposerIndex[:], h.ProposerIndex)

	id = blake2b.Sum256(append(h.SigningHash().Bytes(), proposerIndex[:]...))
	return
}

// SigningHash computes hash of all header fields excluding signature.
func (h *Header) SigningHash() (hash types.Bytes32) {
	if cached := h.cache.signingHash.Load(); cached != nil {
		return cached.(types.Bytes32)
	}
	defer func() { h.cache.signingHash.Store(hash) }()

	bs, err := rlp.EncodeToBytes([]interface{}{
		h.ParentID,
		h.Timestamp,

		h.TxsRoot,

		h.LastKBlock,
		h.Nonce,

		h.QCHash,
		h.ValidatorsHash,
		h.NextValidatorsHash,
	})
	if err != nil {
		slog.Error("could not calculate signing hash", "err", err)
	}
	return blake2b.Sum256(bs)
}

func (h *Header) String() string {
	return fmt.Sprintf(`
    ParentID:                 %v
    Timestamp:                %v
    TxsRoot:                  %v
    ValidatorsHash:           %v
    NextValidatorsHash:       %v
    LastKBlock:               %v
    Nonce:                    %v`, h.ParentID, h.Timestamp, h.TxsRoot, h.ValidatorsHash.String(), h.NextValidatorsHash.String(), h.LastKBlock, h.Nonce)
}

// Number extract block number from block id.
func Number(blockID types.Bytes32) uint32 {
	// first 4 bytes are over written by block number (big endian).
	return binary.BigEndian.Uint32(blockID[:])
}

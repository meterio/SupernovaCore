// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package block

import (
	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/meterio/supernova/types"
)

// Builder only build header and txs. committee info and kblock data built by app.
// Builder to make it easy to build a block object.
type Builder struct {
	header Header
	txs    types.Transactions
	//	committeeInfo CommitteeInfo
	//	kBlockData    kBlockData
	qc    *QuorumCert
	magic [4]byte
}

// ParentID set parent id.
func (b *Builder) ParentID(id types.Bytes32) *Builder {
	b.header.ParentID = id
	return b
}

func (b *Builder) LastKBlock(height uint32) *Builder {
	b.header.LastKBlock = height
	return b
}

func (b *Builder) Tx(tx cmttypes.Tx) *Builder {
	b.txs = append(b.txs, tx)
	return b
}

// Timestamp set timestamp.
func (b *Builder) Timestamp(ts uint64) *Builder {
	b.header.Timestamp = ts
	return b
}

// Transaction add a transaction.
func (b *Builder) Transaction(tx []byte) *Builder {
	b.txs = append(b.txs, tx)
	return b
}

func (b *Builder) Nonce(nonce uint64) *Builder {
	b.header.Nonce = nonce
	return b
}

func (b *Builder) QC(qc *QuorumCert) *Builder {
	b.qc = qc
	b.header.QCHash = qc.Hash()
	return b
}

func (b *Builder) Magic(magic [4]byte) *Builder {
	b.magic = magic
	return b
}

func (b *Builder) ValidatorHash(hash []byte) *Builder {
	b.header.ValidatorHash = hash
	return b
}

func (b *Builder) NextValidatorHash(hash []byte) *Builder {
	b.header.NextValidatorHash = hash
	return b
}

func (b *Builder) ProposerIndex(index uint32) *Builder {
	b.header.ProposerIndex = index
	return b
}

// Build build a block object.
func (b *Builder) Build() *Block {
	header := b.header
	header.TxsRoot = b.txs.RootHash()

	return &Block{
		BlockHeader: &header,
		Txs:         b.txs,
		QC:          b.qc,
		Magic:       b.magic,
	}
}

// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package types

import (
	"github.com/cometbft/cometbft/crypto/merkle"
	cmtbytes "github.com/cometbft/cometbft/libs/bytes"
	cmttypes "github.com/cometbft/cometbft/types"
)

// Transactions a slice of transactions.
type Transactions []cmttypes.Tx

// RootHash computes merkle root hash of transactions.
func (txs Transactions) RootHash() cmtbytes.HexBytes {
	slice := make([][]byte, 0)
	for _, tx := range txs {
		slice = append(slice, tx)
	}
	return merkle.HashFromByteSlices(slice)
}

func (txs Transactions) Convert() [][]byte {
	txbytes := make([][]byte, 0)
	for _, tx := range txs {
		txbytes = append(txbytes, tx)
	}
	return txbytes
}

// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package node

import (
	"container/list"
	"log/slog"

	cmtdb "github.com/cometbft/cometbft-db"
	cmttypes "github.com/cometbft/cometbft/v2/types"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/meterio/supernova/types"
)

// to stash non-executable txs.
// it uses a FIFO queue to limit the size of stash.
type txStash struct {
	db      cmtdb.DB
	fifo    *list.List
	maxSize int
}

func newTxStash(db cmtdb.DB, maxSize int) *txStash {
	return &txStash{db, list.New(), maxSize}
}

func (ts *txStash) Save(tx cmttypes.Tx) error {
	has, err := ts.db.Has(tx.Hash())
	if err != nil {
		return err
	}
	if has {
		return nil
	}

	data, err := rlp.EncodeToBytes(tx)
	if err != nil {
		return err
	}

	if err := ts.db.Set(tx.Hash(), data); err != nil {
		return err
	}
	ts.fifo.PushBack(tx.Hash())
	for ts.fifo.Len() > ts.maxSize {
		keyToDelete := ts.fifo.Remove(ts.fifo.Front()).(types.Bytes32).Bytes()
		if err := ts.db.Delete(keyToDelete); err != nil {
			return err
		}
	}
	return nil
}

func (ts *txStash) LoadAll() types.Transactions {
	var txs types.Transactions
	iter, _ := ts.db.Iterator([]byte{0x0}, []byte{0xff})
	for iter.Valid() {
		var tx cmttypes.Tx
		if err := rlp.DecodeBytes(iter.Value(), &tx); err != nil {
			slog.Warn("decode stashed tx", "err", err)
			if err := ts.db.Delete(iter.Key()); err != nil {
				slog.Warn("delete corrupted stashed tx", "err", err)
			}
		} else {
			txs = append(txs, tx)
			ts.fifo.PushBack(tx.Hash())
		}
		iter.Next()
	}
	return txs
}

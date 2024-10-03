// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package node

import (
	"container/list"
	"log/slog"

	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/meterio/supernova/libs/kv"
	"github.com/meterio/supernova/types"
)

// to stash non-executable txs.
// it uses a FIFO queue to limit the size of stash.
type txStash struct {
	kv      kv.GetPutter
	fifo    *list.List
	maxSize int
}

func newTxStash(kv kv.GetPutter, maxSize int) *txStash {
	return &txStash{kv, list.New(), maxSize}
}

func (ts *txStash) Save(tx cmttypes.Tx) error {
	has, err := ts.kv.Has(tx.Hash())
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

	if err := ts.kv.Put(tx.Hash(), data); err != nil {
		return err
	}
	ts.fifo.PushBack(tx.Hash())
	for ts.fifo.Len() > ts.maxSize {
		keyToDelete := ts.fifo.Remove(ts.fifo.Front()).(types.Bytes32).Bytes()
		if err := ts.kv.Delete(keyToDelete); err != nil {
			return err
		}
	}
	return nil
}

func (ts *txStash) LoadAll() types.Transactions {
	var txs types.Transactions
	iter := ts.kv.NewIterator(*kv.NewRangeWithBytesPrefix(nil))
	for iter.Next() {
		var tx cmttypes.Tx
		if err := rlp.DecodeBytes(iter.Value(), &tx); err != nil {
			slog.Warn("decode stashed tx", "err", err)
			if err := ts.kv.Delete(iter.Key()); err != nil {
				slog.Warn("delete corrupted stashed tx", "err", err)
			}
		} else {
			txs = append(txs, tx)
			ts.fifo.PushBack(tx.Hash())
		}
	}
	return txs
}

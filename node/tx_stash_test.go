// Copyright (c) 2020 The Meter.io developers
// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying

// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package node

import (
	"bytes"
	"sort"
	"testing"

	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/meterio/supernova/libs/lvldb"
	"github.com/meterio/supernova/types"
	"github.com/stretchr/testify/assert"
)

func newTx() cmttypes.Tx {
	var tx cmttypes.Tx
	return tx
}

func TestTxStash(t *testing.T) {
	db, _ := lvldb.NewMem()
	defer db.Close()

	stash := newTxStash(db, 10)

	var saved types.Transactions
	for i := 0; i < 11; i++ {
		tx := newTx()
		assert.Nil(t, stash.Save(tx))
		saved = append(saved, tx)
	}

	loaded := newTxStash(db, 10).LoadAll()

	saved = saved[1:]
	sort.Slice(saved, func(i, j int) bool {
		return bytes.Compare(saved[i].Hash(), saved[j].Hash()) < 0
	})

	sort.Slice(loaded, func(i, j int) bool {
		return bytes.Compare(loaded[i].Hash(), loaded[j].Hash()) < 0
	})

	assert.Equal(t, saved.RootHash(), loaded.RootHash())
}

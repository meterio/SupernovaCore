// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package txpool

import (
	"errors"
	"testing"

	"github.com/meterio/supernova/genesis"
	"github.com/meterio/supernova/types"
	"github.com/stretchr/testify/assert"
)

func TestTxObjMap(t *testing.T) {

	tx1 := newTx(genesis.DevAccounts()[0])
	tx2 := newTx(genesis.DevAccounts()[0])
	tx3 := newTx(genesis.DevAccounts()[1])

	txObj1, _ := resolveTx(tx1)
	txObj2, _ := resolveTx(tx2)
	txObj3, _ := resolveTx(tx3)

	m := newTxObjectMap()
	assert.Zero(t, m.Len())

	assert.Nil(t, m.Add(txObj1, 1))
	assert.Nil(t, m.Add(txObj1, 1), "should no error if exists")
	assert.Equal(t, 1, m.Len())

	assert.Equal(t, errors.New("account quota exceeded"), m.Add(txObj2, 1))
	assert.Equal(t, 1, m.Len())

	assert.Nil(t, m.Add(txObj3, 1))
	assert.Equal(t, 2, m.Len())

	assert.True(t, m.Contains(tx1.Hash()))
	assert.False(t, m.Contains(tx2.Hash()))
	assert.True(t, m.Contains(tx3.Hash()))

	assert.True(t, m.Remove(tx1.Hash()))
	assert.False(t, m.Contains(tx1.Hash()))
	assert.False(t, m.Remove(tx2.Hash()))

	assert.Equal(t, []*txObject{txObj3}, m.ToTxObjects())
	assert.Equal(t, types.Transactions{tx3}, m.ToTxs())

}

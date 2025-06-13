// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package txpool

import (
	"testing"
	"time"

	cmtdb "github.com/cometbft/cometbft-db"
	cmttypes "github.com/cometbft/cometbft/v2/types"
	"github.com/meterio/supernova/block"
	"github.com/meterio/supernova/types"
	"github.com/stretchr/testify/assert"
)

func init() {
}

func newPool() *TxPool {
	db := cmtdb.NewMemDB()
	chain := newChain(db)
	return New(chain, Options{
		Limit:           10,
		LimitPerAccount: 2,
		MaxLifetime:     time.Hour,
	})
}
func TestNewClose(t *testing.T) {
	pool := newPool()
	defer pool.Close()
}

func TestSubscribeNewTx(t *testing.T) {
	pool := newPool()
	defer pool.Close()

	b1 := new(block.Builder).
		ParentID(pool.chain.GenesisBlock().ID()).
		NanoTimestamp(uint64(time.Now().UnixNano())).Build()
	qc := block.QuorumCert{Epoch: 0, Round: 1}
	b1.SetQC(&qc)
	pool.chain.AddBlock(b1, nil)

	txCh := make(chan *TxEvent)

	pool.SubscribeTxEvent(txCh)

	tx := newTx()
	assert.Nil(t, pool.Add(tx))

	v := true
	assert.Equal(t, &TxEvent{tx, &v}, <-txCh)
}

func TestWashTxs(t *testing.T) {
	pool := newPool()
	defer pool.Close()
	txs, _, err := pool.wash(pool.chain.BestBlock().Header(), time.Second*10)
	assert.Nil(t, err)
	assert.Zero(t, len(txs))
	assert.Zero(t, len(pool.Executables()))

	tx := newTx()
	assert.Nil(t, pool.Add(tx))

	txs, _, err = pool.wash(pool.chain.BestBlock().Header(), time.Second*10)
	assert.Nil(t, err)
	assert.Equal(t, types.Transactions{tx}, txs)

	b1 := new(block.Builder).
		ParentID(pool.chain.GenesisBlock().ID()).
		NanoTimestamp(uint64(time.Now().UnixNano())).
		Build()
	qc := block.QuorumCert{Epoch: 0, Round: 1}
	b1.SetQC(&qc)
	pool.chain.AddBlock(b1, nil)

	txs, _, err = pool.wash(pool.chain.BestBlock().Header(), time.Second*10)
	assert.Nil(t, err)
	assert.Equal(t, types.Transactions{tx}, txs)
}

func TestAdd(t *testing.T) {
	pool := newPool()
	defer pool.Close()
	b1 := new(block.Builder).
		ParentID(pool.chain.GenesisBlock().ID()).
		NanoTimestamp(uint64(time.Now().UnixNano())).
		Build()
	qc := block.QuorumCert{Epoch: 0, Round: 1}
	b1.SetQC(&qc)
	pool.chain.AddBlock(b1, nil)

	dupTx := newTx()

	tests := []struct {
		tx     cmttypes.Tx
		errStr string
	}{
		{newTx(), "bad tx: chain tag mismatch"},
		{dupTx, ""},
		{dupTx, ""},
	}

	for _, tt := range tests {
		err := pool.Add(tt.tx)
		if tt.errStr == "" {
			assert.Nil(t, err)
		} else {
			assert.Equal(t, tt.errStr, err.Error())
		}
	}

	tests = []struct {
		tx     cmttypes.Tx
		errStr string
	}{
		{newTx(), "tx rejected: tx is not executable"},
		{newTx(), "tx rejected: tx is not executable"},
	}

	for _, tt := range tests {
		err := pool.StrictlyAdd(tt.tx)
		if tt.errStr == "" {
			assert.Nil(t, err)
		} else {
			assert.Equal(t, tt.errStr, err.Error())
		}
	}
}

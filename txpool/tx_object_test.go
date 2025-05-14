// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package txpool

import (
	"math/big"
	"testing"

	cmtdb "github.com/cometbft/cometbft-db"
	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/meterio/supernova/block"
	"github.com/meterio/supernova/chain"
	"github.com/stretchr/testify/assert"
)

func newChain(db cmtdb.DB) *chain.Chain {
	chain, _ := chain.New(db, false)
	return chain
}

func newTx() cmttypes.Tx {
	return cmttypes.Tx{}
}

func TestSort(t *testing.T) {
	objs := []*txObject{
		{overallGasPrice: big.NewInt(10)},
		{overallGasPrice: big.NewInt(20)},
		{overallGasPrice: big.NewInt(30)},
	}
	sortTxObjsByOverallGasPriceDesc(objs)

	assert.Equal(t, big.NewInt(30), objs[0].overallGasPrice)
	assert.Equal(t, big.NewInt(20), objs[1].overallGasPrice)
	assert.Equal(t, big.NewInt(10), objs[2].overallGasPrice)
}

func TestResolve(t *testing.T) {
	tx := newTx()

	txObj, err := resolveTx(tx)
	assert.Nil(t, err)
	assert.Equal(t, tx, txObj.Tx)

}

func TestExecutable(t *testing.T) {

	db := cmtdb.NewMemDB()
	chain := newChain(db)
	b0 := chain.GenesisBlock()
	b1 := new(block.Builder).ParentID(b0.Header().ID()).Build()
	qc1 := block.QuorumCert{Epoch: 0, Round: 1}
	b1.SetQC(&qc1)
	escortQC := &block.QuorumCert{Round: b1.QC.Round + 1, Epoch: b1.QC.Epoch}
	chain.AddBlock(b1, escortQC)

	tests := []struct {
		tx          cmttypes.Tx
		expected    bool
		expectedErr string
	}{
		{newTx(), true, ""},
		{newTx(), false, "gas too large"},
		{newTx(), true, "block ref out of schedule"},
		{newTx(), true, "head block expired"},
		{newTx(), false, ""},
	}

	for _, tt := range tests {
		txObj, err := resolveTx(tt.tx)
		assert.Nil(t, err)

		exe, err := txObj.Executable(chain, b1.Header())
		if tt.expectedErr != "" {
			assert.Equal(t, tt.expectedErr, err.Error())
		} else {
			assert.Nil(t, err)
			assert.Equal(t, tt.expected, exe)
		}
	}
}

// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package txpool

import (
	"math/big"
	"testing"

	cmttypes "github.com/cometbft/cometbft/types"
	db "github.com/cosmos/cosmos-db"
	"github.com/meterio/supernova/block"
	"github.com/meterio/supernova/chain"
	"github.com/meterio/supernova/genesis"
	"github.com/meterio/supernova/libs/lvldb"
	"github.com/stretchr/testify/assert"
)

func newChain(kv db.DB) *chain.Chain {
	gene := genesis.NewGenesis()
	b0, _ := gene.Build()
	chain, _ := chain.New(kv, b0, gene.ValidatorSet(), false)
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
	acc := genesis.DevAccounts()[0]
	tx := newTx(acc)

	txObj, err := resolveTx(tx)
	assert.Nil(t, err)
	assert.Equal(t, tx, txObj.Tx)

}

func TestExecutable(t *testing.T) {
	acc := genesis.DevAccounts()[0]

	kv, _ := lvldb.NewMem()
	chain := newChain(kv)
	b0 := chain.GenesisBlock()
	b1 := new(block.Builder).ParentID(b0.Header().ID()).Build()
	qc1 := block.QuorumCert{Height: 1, Round: 1, Epoch: 0}
	b1.SetQC(&qc1)
	escortQC := &block.QuorumCert{Height: b1.Number(), Round: b1.QC.Round + 1, Epoch: b1.QC.Epoch, VoterMsgHash: b1.VotingHash()}
	chain.AddBlock(b1, escortQC)

	tests := []struct {
		tx          cmttypes.Tx
		expected    bool
		expectedErr string
	}{
		{newTx(acc), true, ""},
		{newTx(acc), false, "gas too large"},
		{newTx(acc), true, "block ref out of schedule"},
		{newTx(acc), true, "head block expired"},
		{newTx(acc), false, ""},
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

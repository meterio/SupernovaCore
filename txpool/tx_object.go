// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package txpool

import (
	"log/slog"
	"math/big"
	"sort"
	"time"

	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/meterio/supernova/block"
	"github.com/meterio/supernova/chain"
	"github.com/pkg/errors"
)

type txObject struct {
	cmttypes.Tx

	timeAdded       int64
	executable      bool
	overallGasPrice *big.Int // don't touch this value, it's only be used in pool's housekeeping
}

func resolveTx(tx cmttypes.Tx) (*txObject, error) {
	return &txObject{
		Tx:        tx,
		timeAdded: time.Now().UnixNano(),
	}, nil
}

func (o *txObject) Executable(chain *chain.Chain, headBlock *block.Header) (bool, error) {
	if o == nil {
		slog.Error("tx object is nil")
		return false, errors.New("txobject is null")
	}

	if has, err := chain.HasTransactionMeta(o.Hash()); err != nil {
		return false, err
	} else if has {
		return false, errors.New("known tx")
	}

	return true, nil
}

func sortTxObjsByOverallGasPriceDesc(txObjs []*txObject) {
	sort.Slice(txObjs, func(i, j int) bool {
		gp1, gp2 := txObjs[i].overallGasPrice, txObjs[j].overallGasPrice
		return gp1.Cmp(gp2) >= 0
	})
}

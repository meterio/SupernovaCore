// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package txpool

import (
	"encoding/hex"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/cometbft/cometbft/v2/libs/bytes"
	cmttypes "github.com/cometbft/cometbft/v2/types"
	"github.com/ethereum/go-ethereum/event"
	"github.com/meterio/supernova/chain"
	"github.com/meterio/supernova/libs/co"
	"github.com/meterio/supernova/types"
)

var (
	errTxExisted = errors.New("tx existed")
)

var DefaultTxPoolOptions = Options{
	Limit:           200000,
	LimitPerAccount: 1024, /*16,*/ //XXX: increase to 1024 from 16 during the testing
	MaxLifetime:     20 * time.Minute,
}

// Options options for tx pool.
type Options struct {
	Limit           int
	LimitPerAccount int
	MaxLifetime     time.Duration
}

// TxEvent will be posted when tx is added or status changed.
type TxEvent struct {
	Tx         cmttypes.Tx
	Executable *bool
}

// TxPool maintains unprocessed transactions.
type TxPool struct {
	options Options
	chain   *chain.Chain

	executables sync.Map // key: txId, value: txObject

	done   chan struct{}
	txFeed event.Feed
	scope  event.SubscriptionScope
	goes   co.Goes

	logger *slog.Logger
}

// New create a new TxPool instance.
// Shutdown is required to be called at end.
func New(chain *chain.Chain, options Options) *TxPool {
	pool := &TxPool{
		options:     options,
		executables: sync.Map{},
		chain:       chain,
		done:        make(chan struct{}),

		logger: slog.With("pkg", "txpool"),
	}
	return pool
}

// Close cleanup inner go routines.
func (p *TxPool) Close() {
	close(p.done)
	p.scope.Close()
	p.goes.Wait()
	p.logger.Debug("closed")
}

// SubscribeTxEvent receivers will receive a tx
func (p *TxPool) SubscribeTxEvent(ch chan *TxEvent) event.Subscription {
	return p.scope.Track(p.txFeed.Subscribe(ch))
}

func (p *TxPool) Add(newTx cmttypes.Tx) error {
	txObj, err := resolveTx(newTx)
	if err != nil {
		return badTxError{err.Error()}
	}

	// Check if key exists
	if _, ok := p.executables.Load(newTx.Hash().String()); ok {
		// key exist
		return errTxExisted
	} else {
		// key doesn not exist
		p.executables.Store(newTx.Hash().String(), txObj)
	}

	p.logger.Info("tx added", "id", newTx.Hash())
	p.goes.Go(func() {
		v := true
		p.txFeed.Send(&TxEvent{newTx, &v})
	})

	return nil
}

func (p *TxPool) Get(id []byte) cmttypes.Tx {
	strId := bytes.HexBytes(id).String()
	if txObj, ok := p.executables.Load(strId); ok {
		return txObj.(*txObject).Tx
	}
	return nil
}

// Remove removes tx from pool by its ID.
func (p *TxPool) Remove(id []byte) bool {
	strId := bytes.HexBytes(id).String()
	if _, ok := p.executables.Load(strId); ok {
		p.executables.Delete(strId)
		hash := hex.EncodeToString(id)
		p.logger.Info("tx removed", "id", hash)
		return true
	}
	return false
}

// FIXME: should sort tx
// Executables returns executable txs.
func (p *TxPool) Executables() types.Transactions {
	txList := make([]cmttypes.Tx, 0)
	p.executables.Range(func(key any, value any) bool {
		txList = append(txList, value.(*txObject).Tx)
		return true
	})
	return txList
}

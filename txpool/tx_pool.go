// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package txpool

import (
	"log/slog"
	"sync/atomic"
	"time"

	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/ethereum/go-ethereum/common/mclock"
	"github.com/ethereum/go-ethereum/event"
	"github.com/meterio/supernova/block"
	"github.com/meterio/supernova/chain"
	"github.com/meterio/supernova/libs/co"
	"github.com/meterio/supernova/types"
	"github.com/pkg/errors"
)

const (
	// max size of tx allowed
	maxTxSize = 64 * 1024 * 2
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

	executables    atomic.Value
	all            *txObjectMap
	addedAfterWash uint32

	done   chan struct{}
	txFeed event.Feed
	scope  event.SubscriptionScope
	goes   co.Goes

	newTxFeed chan []byte
	logger    *slog.Logger
}

// New create a new TxPool instance.
// Shutdown is required to be called at end.
func New(chain *chain.Chain, options Options) *TxPool {
	pool := &TxPool{
		options: options,
		chain:   chain,
		all:     newTxObjectMap(),
		done:    make(chan struct{}),

		newTxFeed: make(chan []byte, options.Limit),
		logger:    slog.With("pkg", "txpool"),
	}
	pool.goes.Go(pool.housekeeping)
	return pool
}

func (p *TxPool) housekeeping() {
	p.logger.Debug("enter housekeeping")
	defer p.logger.Debug("leave housekeeping")

	washInterval := time.Second
	ticker := time.NewTicker(washInterval)
	defer ticker.Stop()

	// Hotstuff: Should change to after seem new proposal and do wash txs.
	headBlock := p.chain.BestBlock().Header()

	for {
		select {
		case <-p.done:
			return
		case <-ticker.C:
			var headBlockChanged bool
			if newHeadBlock := p.chain.BestBlock().Header(); newHeadBlock.ID() != headBlock.ID() {
				headBlock = newHeadBlock
				headBlockChanged = true
			}
			if !isChainSynced(uint64(time.Now().Unix()), headBlock.Timestamp) {
				// skip washing txs if not synced
				continue
			}
			poolLen := p.all.Len()
			p.logger.Debug("wash start", "poolLen", poolLen)
			// do wash on
			// 1. head block changed
			// 2. pool size exceeds limit
			// 3. new tx added while pool size is small
			if headBlockChanged ||
				poolLen > p.options.Limit ||
				(poolLen < 200 && atomic.LoadUint32(&p.addedAfterWash) > 0) {

				atomic.StoreUint32(&p.addedAfterWash, 0)

				startTime := mclock.Now()
				executables, removed, err := p.wash(headBlock, washInterval)
				elapsed := mclock.Now() - startTime

				ctx := []interface{}{
					"len", poolLen,
					"removed", removed,
					"elapsed", types.PrettyDuration(elapsed),
				}
				if err != nil {
					ctx = append(ctx, "err", err)
				} else {
					p.executables.Store(executables)
				}

				p.logger.Debug("wash done", ctx...)
			}
		}
	}
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

func (p *TxPool) add(newTx cmttypes.Tx, rejectNonexecutable bool) error {
	if p.all.Contains(newTx.Hash()) {
		// tx already in the pool
		return nil
	}

	txObj, err := resolveTx(newTx)
	if err != nil {
		return badTxError{err.Error()}
	}

	headBlock := p.chain.BestBlock().Header()
	if isChainSynced(uint64(time.Now().Unix()), headBlock.Timestamp) {
		executable, err := txObj.Executable(p.chain, headBlock)
		if err != nil {
			return txRejectedError{err.Error()}
		}

		if rejectNonexecutable && !executable {
			return txRejectedError{"tx is not executable"}
		}

		if err := p.all.Add(txObj, p.options.LimitPerAccount); err != nil {
			return txRejectedError{err.Error()}
		}
		p.logger.Debug("tx added, chain is synced", "id", newTx.Hash(), "pool size", p.all.Len())
		txObj.executable = executable
		p.goes.Go(func() {
			p.txFeed.Send(&TxEvent{newTx, &executable})
		})
	} else {
		// we skip steps that rely on head block when chain is not synced,
		// but check the pool's limit
		if p.all.Len() >= p.options.Limit {
			return txRejectedError{"pool is full"}
		}

		if err := p.all.Add(txObj, p.options.LimitPerAccount); err != nil {
			return txRejectedError{err.Error()}
		}
		p.logger.Debug("tx added, chain is not synced", "id", newTx.Hash(), "pool size", p.all.Len())
		p.txFeed.Send(&TxEvent{newTx, nil})
	}
	p.logger.Debug("tx added to pool", "id", newTx.Hash())

	if len(p.newTxFeed) < cap(p.newTxFeed) {
		p.newTxFeed <- newTx.Hash()
		p.logger.Debug("new tx feed: ", "id", newTx.Hash())
	} else {
		select {
		case <-p.newTxFeed:
		default:
			break
		}
		p.newTxFeed <- newTx.Hash()
		p.logger.Debug("new tx feed: ", "id", newTx.Hash())
	}
	atomic.AddUint32(&p.addedAfterWash, 1)
	return nil
}

func (p *TxPool) GetNewTxFeed() chan []byte {
	return p.newTxFeed
}

// Add add new tx into pool.
// It's not assumed as an error if the tx to be added is already in the pool,
func (p *TxPool) Add(newTx cmttypes.Tx) error {
	return p.add(newTx, false)
}

func (p *TxPool) Get(id []byte) cmttypes.Tx {
	if txObj := p.all.GetByID(id); txObj != nil {
		return txObj.Tx
	}
	return nil
}

func (p *TxPool) GetTxObj(id []byte) *txObject {
	if txObj := p.all.GetByID(id); txObj != nil {
		return txObj
	}
	return nil
}

// StrictlyAdd add new tx into pool. A rejection error will be returned, if tx is not executable at this time.
func (p *TxPool) StrictlyAdd(newTx cmttypes.Tx) error {
	return p.add(newTx, true)
}

// Remove removes tx from pool by its ID.
func (p *TxPool) Remove(id []byte) bool {
	if p.all.Remove(id) {
		p.logger.Debug("tx removed", "id", id)
		return true
	}
	return false
}

// Executables returns executable txs.
func (p *TxPool) Executables() types.Transactions {
	if sorted := p.executables.Load(); sorted != nil {
		return sorted.(types.Transactions)
	}
	return nil
}

// Fill fills txs into pool.
func (p *TxPool) Fill(txs types.Transactions, executed func(txID []byte) bool) {
	txObjs := make([]*txObject, 0, len(txs))
	for _, tx := range txs {
		// here we ignore errors
		if txObj, err := resolveTx(tx); err == nil {
			// skip executed
			if executed(tx.Hash()) {
				p.logger.Debug("tx skipped", "id", txObj.Hash(), "err", "executed")
				continue
			}

			txObjs = append(txObjs, txObj)
		}
	}
	p.all.Fill(txObjs)
}

// Dump dumps all txs in the pool.
func (p *TxPool) Dump() types.Transactions {
	return p.all.ToTxs()
}

// wash to evict txs that are over limit, out of lifetime, out of energy, settled, expired or dep broken.
// this method should only be called in housekeeping go routine
func (p *TxPool) wash(headBlock *block.Header, timeLimit time.Duration) (executables types.Transactions, removed int, err error) {
	all := p.all.ToTxObjects()
	var toRemove [][]byte
	defer func() {
		if err != nil {
			// in case of error, simply cut pool size to limit
			for i, txObj := range all {
				if len(all)-i <= p.options.Limit {
					break
				}
				removed++
				p.all.Remove(txObj.Hash())
			}
		} else {
			for _, id := range toRemove {
				p.all.Remove(id)
			}
			removed = len(toRemove)
		}
	}()
	start := time.Now()
	var (
		seeker            = p.chain.NewSeeker(headBlock.ID())
		executableObjs    = make([]*txObject, 0, len(all))
		nonExecutableObjs = make([]*txObject, 0, len(all))
		now               = time.Now().UnixNano()
	)
	for _, txObj := range all {
		// out of lifetime
		if now > txObj.timeAdded+int64(p.options.MaxLifetime) {
			toRemove = append(toRemove, txObj.Hash())
			p.logger.Debug("tx washed out", "id", txObj.Hash(), "err", "out of lifetime")
			continue
		}
		// settled, out of energy or dep broken
		executable, err := txObj.Executable(p.chain, headBlock)
		if err != nil {
			toRemove = append(toRemove, txObj.Hash())
			p.logger.Debug("tx washed out", "id", txObj.Hash(), "err", err)
			continue
		}

		if executable {
			executableObjs = append(executableObjs, txObj)
		} else {
			nonExecutableObjs = append(nonExecutableObjs, txObj)
		}
		if time.Since(start) > timeLimit {
			p.logger.Info("tx wash ended early due to time limit", "elapsed", types.PrettyDuration(time.Since(start)), "execs", len(executableObjs))
			break
		}
	}

	if err := seeker.Err(); err != nil {
		return nil, 0, errors.WithMessage(err, "seeker")
	}

	// sort objs by price from high to low
	// sortTxObjsByOverallGasPriceDesc(executableObjs)

	limit := p.options.Limit

	// remove over limit txs, from non-executables to low priced
	if len(executableObjs) > limit {
		for _, txObj := range nonExecutableObjs {
			toRemove = append(toRemove, txObj.Hash())
			p.logger.Debug("non-executable tx washed out due to pool limit", "id", txObj.Hash())
		}
		for _, txObj := range executableObjs[limit:] {
			toRemove = append(toRemove, txObj.Hash())
			p.logger.Debug("executable tx washed out due to pool limit", "id", txObj.Hash())
		}
		executableObjs = executableObjs[:limit]
	} else if len(executableObjs)+len(nonExecutableObjs) > limit {
		// executableObjs + nonExecutableObjs over pool limit
		for _, txObj := range nonExecutableObjs[limit-len(executableObjs):] {
			toRemove = append(toRemove, txObj.Hash())
			p.logger.Debug("non-executable tx washed out due to pool limit", "id", txObj.Hash())
		}
	}

	executables = make(types.Transactions, 0, len(executableObjs))
	var toBroadcast types.Transactions

	for _, obj := range executableObjs {
		executables = append(executables, obj.Tx)
		if !obj.executable {
			obj.executable = true
			toBroadcast = append(toBroadcast, obj.Tx)
		}
	}

	p.goes.Go(func() {
		for _, tx := range toBroadcast {
			executable := true
			p.txFeed.Send(&TxEvent{tx, &executable})
		}
	})
	p.logger.Debug("in wash", "executables size", len(executables), "non-executables size", len(nonExecutableObjs))
	return executables, 0, nil
}

func isChainSynced(nowTimestamp, blockTimestamp uint64) bool {
	timeDiff := nowTimestamp - blockTimestamp
	if blockTimestamp > nowTimestamp {
		timeDiff = blockTimestamp - nowTimestamp
	}
	return timeDiff < types.BlockInterval*6
}

func (p *TxPool) All() []*txObject {
	return p.all.ToTxObjects()
}

func (p *TxPool) Len() int {
	return p.all.Len()
}

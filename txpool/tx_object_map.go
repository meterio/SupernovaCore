// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package txpool

import (
	"encoding/hex"
	"fmt"
	"log/slog"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/meterio/supernova/types"
)

// txObjectMap to maintain mapping of ID to tx object, and account quota.
type txObjectMap struct {
	lock     sync.RWMutex
	txObjMap map[string]*txObject
	quota    map[common.Address]int
}

func newTxObjectMap() *txObjectMap {
	return &txObjectMap{
		txObjMap: make(map[string]*txObject),
		quota:    make(map[common.Address]int),
	}
}

func (m *txObjectMap) Contains(txId []byte) bool {
	m.lock.RLock()
	defer m.lock.RUnlock()
	_, found := m.txObjMap[hex.EncodeToString(txId)]
	return found
}

func (m *txObjectMap) Add(txObj *txObject, limitPerAccount int) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	if _, found := m.txObjMap[hex.EncodeToString(txObj.Hash())]; found {
		return nil
	}

	m.txObjMap[hex.EncodeToString(txObj.Hash())] = txObj
	slog.Debug(fmt.Sprintf("added tx %s", hex.EncodeToString(txObj.Hash())), "poolSize", len(m.txObjMap))
	return nil
}

func (m *txObjectMap) GetByID(id []byte) *txObject {
	m.lock.Lock()
	defer m.lock.Unlock()
	return m.txObjMap[hex.EncodeToString(id)]
}

func (m *txObjectMap) Remove(txId []byte) bool {
	m.lock.Lock()
	defer m.lock.Unlock()

	if _, ok := m.txObjMap[hex.EncodeToString(txId)]; ok {
		delete(m.txObjMap, hex.EncodeToString(txId))
		slog.Debug("removed tx", "id", txId, "mapSize", len(m.txObjMap))
		return true
	}
	return false
}

func (m *txObjectMap) ToTxObjects() []*txObject {
	m.lock.RLock()
	defer m.lock.RUnlock()

	txObjs := make([]*txObject, 0, len(m.txObjMap))
	for _, txObj := range m.txObjMap {
		txObjs = append(txObjs, txObj)
	}
	return txObjs
}

func (m *txObjectMap) ToTxs() types.Transactions {
	m.lock.RLock()
	defer m.lock.RUnlock()

	txs := make(types.Transactions, 0, len(m.txObjMap))
	for _, txObj := range m.txObjMap {
		txs = append(txs, txObj.Tx)
	}
	return txs
}

func (m *txObjectMap) Fill(txObjs []*txObject) {
	m.lock.Lock()
	defer m.lock.Unlock()
	for _, txObj := range txObjs {
		if _, found := m.txObjMap[hex.EncodeToString(txObj.Hash())]; found {
			continue
		}
		// skip account limit check

		m.txObjMap[hex.EncodeToString(txObj.Hash())] = txObj
	}
}

func (m *txObjectMap) Len() int {
	m.lock.RLock()
	defer m.lock.RUnlock()
	return len(m.txObjMap)
}

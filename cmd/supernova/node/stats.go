// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package node

import (
	"github.com/meterio/supernova/block"
)

type blockStats struct {
	txs       int
	processed int
}

func (s *blockStats) UpdateProcessed(n int, txs int) {
	s.processed += n
	s.txs += txs
}

func (s *blockStats) LogContext(last *block.Header, pending int) []interface{} {
	return []interface{}{
		"txs", s.txs,
		"pending", pending,
		"processed", float64(s.processed),
	}
}

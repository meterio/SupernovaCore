// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package comm

import "github.com/meterio/supernova/types"

// type Traffic struct {
// 	Bytes    uint64
// 	Requests uint64
// 	Errors   uint64
// }

// PeerStats records stats of a peer.
type PeerStats struct {
	Name         string
	BestBlockID  types.Bytes32
	BestBlockNum uint32
	PeerID       string
	NetAddr      string
	Inbound      bool
	Duration     uint64 // in seconds
}

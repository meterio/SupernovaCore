// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package rpc

import (
	"context"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/meterio/supernova/block"
	"github.com/meterio/supernova/types"
)

// NewBlockEvent event emitted when received block announcement.
type NewBlockEvent struct {
	PeerID   peer.ID
	NewBlock *block.EscortedBlock
}

type NewBlockIDEvent struct {
	PeerID     peer.ID
	NewBlockID types.Bytes32
}

// HandleBlockStream to handle the stream of downloaded blocks in sync process.
type HandleBlockStream func(ctx context.Context, stream <-chan *block.EscortedBlock) error

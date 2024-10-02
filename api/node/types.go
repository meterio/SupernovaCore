// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package node

import (
	"github.com/meterio/supernova/comm"
	"github.com/meterio/supernova/consensus"
	"github.com/meterio/supernova/meter"
)

type Network interface {
	PeersStats() []*comm.PeerStats
}

type PeerStats struct {
	Name         string        `json:"name"`
	BestBlockID  meter.Bytes32 `json:"bestBlockID"`
	BestBlockNum uint32        `json:"bestBlockNum"`
	PeerID       string        `json:"peerID"`
	NetAddr      string        `json:"netAddr"`
	Inbound      bool          `json:"inbound"`
	Duration     uint64        `json:"duration"`
}

func ConvertPeersStats(ss []*comm.PeerStats) []*PeerStats {
	if len(ss) == 0 {
		return nil
	}
	peersStats := make([]*PeerStats, len(ss))
	for i, peerStats := range ss {
		peersStats[i] = &PeerStats{
			Name:         peerStats.Name,
			BestBlockID:  peerStats.BestBlockID,
			BestBlockNum: peerStats.BestBlockNum,
			PeerID:       peerStats.PeerID,
			NetAddr:      peerStats.NetAddr,
			Inbound:      peerStats.Inbound,
			Duration:     peerStats.Duration,
		}
	}
	return peersStats
}

type Consensus interface {
	Committee() []*consensus.ApiCommitteeMember
}

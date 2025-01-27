package rpc

import (
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/meterio/supernova/types"
)

type announcement struct {
	newBlockID types.Bytes32
	peerID     peer.ID
}

func (s *RPCServer) announcementLoop() {
	const maxFetches = 3 // per block ID

	fetchingPeers := map[peer.ID]bool{}
	fetchingBlockIDs := map[types.Bytes32]int{}

	fetchDone := make(chan *announcement)

	for {
		select {
		case <-s.ctx.Done():
			return
		case ann := <-fetchDone:
			delete(fetchingPeers, ann.peerID)
			if n := fetchingBlockIDs[ann.newBlockID] - 1; n > 0 {
				fetchingBlockIDs[ann.newBlockID] = n
			} else {
				delete(fetchingBlockIDs, ann.newBlockID)
			}
		case ann := <-s.announcementCh:
			if f, n := fetchingPeers[ann.peerID], fetchingBlockIDs[ann.newBlockID]; !f && n < maxFetches {
				fetchingPeers[ann.peerID] = true
				fetchingBlockIDs[ann.newBlockID] = n + 1

				s.goes.Go(func() {
					defer func() {
						select {
						case fetchDone <- ann:
						case <-s.ctx.Done():
						}
					}()
					s.fetchBlockByID(ann.peerID, ann.newBlockID)
				})
			} else {
				s.logger.Debug("skip new block ID announcement")
			}
		}
	}
}

func (s *RPCServer) fetchBlockByID(peer peer.ID, newBlockID types.Bytes32) {
	if _, err := s.chain.GetBlockHeader(newBlockID); err != nil {
		if !s.chain.IsNotFound(err) {
			s.logger.Error("failed to get block header", "err", err)
		}
	} else {
		// already in chain
		return
	}

	escortedBlk, err := s.GetBlockByID(peer, newBlockID)
	if err != nil {
		s.logger.Debug("failed to get block by id", "err", err)
		return
	}
	if escortedBlk == nil {
		s.logger.Debug("get nil block by id")
		return
	}

	s.newBlockFeed.Send(&NewBlockEvent{
		EscortedBlock: escortedBlk,
	})
}

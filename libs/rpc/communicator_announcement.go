package rpc

import (
	"context"

	"github.com/ethereum/go-ethereum/rlp"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/meterio/supernova/block"
	"github.com/meterio/supernova/libs/pb"
	"github.com/meterio/supernova/types"
)

type announcement struct {
	newBlockID types.Bytes32
	peerID     peer.ID
}

func (c *Communicator) announcementLoop() {
	const maxFetches = 3 // per block ID

	fetchingPeers := map[peer.ID]bool{}
	fetchingBlockIDs := map[types.Bytes32]int{}

	fetchDone := make(chan *announcement)

	for {
		select {
		case <-c.ctx.Done():
			return
		case ann := <-fetchDone:
			delete(fetchingPeers, ann.peerID)
			if n := fetchingBlockIDs[ann.newBlockID] - 1; n > 0 {
				fetchingBlockIDs[ann.newBlockID] = n
			} else {
				delete(fetchingBlockIDs, ann.newBlockID)
			}
		case ann := <-c.announcementCh:
			if f, n := fetchingPeers[ann.peerID], fetchingBlockIDs[ann.newBlockID]; !f && n < maxFetches {
				fetchingPeers[ann.peerID] = true
				fetchingBlockIDs[ann.newBlockID] = n + 1

				c.goes.Go(func() {
					defer func() {
						select {
						case fetchDone <- ann:
						case <-c.ctx.Done():
						}
					}()
					c.fetchBlockByID(ann.peerID, ann.newBlockID)
				})
			} else {
				c.logger.Debug("skip new block ID announcement")
			}
		}
	}
}

func (c *Communicator) fetchBlockByID(peerID peer.ID, newBlockID types.Bytes32) {
	if _, err := c.chain.GetBlockHeader(newBlockID); err != nil {
		if !c.chain.IsNotFound(err) {
			c.logger.Error("failed to get block header", "err", err)
		}
	} else {
		// already in chain
		return
	}

	client := c.GetRPCClient(peerID)
	res, err := client.GetBlockByID(context.Background(), &pb.GetBlockByIDRequest{BlockIdBytes: newBlockID.Bytes()})
	if err != nil {
		c.logger.Debug("failed to get block by id", "err", err)
		return
	}
	if res == nil {
		c.logger.Debug("get nil block by id")
		return
	}

	escortedBlock := block.EscortedBlock{}
	rlp.DecodeBytes(res.BlockBytes, &escortedBlock)

	c.newBlockFeed.Send(&NewBlockEvent{
		EscortedBlock: &escortedBlock,
	})
}

package rpc

import (
	"context"

	"github.com/ethereum/go-ethereum/rlp"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/meterio/supernova/block"
	"github.com/meterio/supernova/libs/pb"
	"github.com/meterio/supernova/types"
)

func (c *Communicator) announcementLoop() {
	const maxFetches = 3 // per block ID

	fetchingPeers := map[peer.ID]bool{}
	fetchingBlockIDs := map[types.Bytes32]int{}

	fetchDone := make(chan *NewBlockIDEvent)

	for {
		select {
		case <-c.ctx.Done():
			return
		case ann := <-fetchDone:
			delete(fetchingPeers, ann.PeerID)
			if n := fetchingBlockIDs[ann.NewBlockID] - 1; n > 0 {
				fetchingBlockIDs[ann.NewBlockID] = n
			} else {
				delete(fetchingBlockIDs, ann.NewBlockID)
			}
		case ann := <-c.newBlockIdCh:
			if f, n := fetchingPeers[ann.PeerID], fetchingBlockIDs[ann.NewBlockID]; !f && n < maxFetches {
				fetchingPeers[ann.PeerID] = true
				fetchingBlockIDs[ann.NewBlockID] = n + 1

				c.goes.Go(func() {
					defer func() {
						select {
						case fetchDone <- ann:
						case <-c.ctx.Done():
						}
					}()
					c.fetchBlockByID(ann.PeerID, ann.NewBlockID)
				})
			} else {
				c.logger.Warn("skip new block ID announcement", "id", ann.NewBlockID)
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

	c.rpcServer.newBlockFeed.Send(&NewBlockEvent{
		PeerID:   peerID,
		NewBlock: &escortedBlock,
	})
}

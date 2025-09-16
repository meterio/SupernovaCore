package rpc

import (
	"context"
	"encoding/hex"
	errors "errors"
	"log/slog"
	sync "sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/libp2p/go-libp2p/core/network"
	libp2ppeer "github.com/libp2p/go-libp2p/core/peer"
	"github.com/meterio/supernova/block"
	"github.com/meterio/supernova/chain"
	"github.com/meterio/supernova/libs/co"
	"github.com/meterio/supernova/libs/p2p"
	"github.com/meterio/supernova/libs/pb"
	"github.com/meterio/supernova/txpool"
	"github.com/meterio/supernova/types"
	"storj.io/drpc/drpcmux"
	"storj.io/drpc/drpcserver"
)

type RPCServer struct {
	ctx    context.Context
	chain  *chain.Chain
	logger *slog.Logger
	p2pSrv p2p.P2P // libp2p host
	txPool *txpool.TxPool
	goes   co.Goes

	// announcement
	newBlockFeed   event.Feed // feed of block
	newBlockIdFeed event.Feed // feed of announcement
	feedScope      event.SubscriptionScope

	// sync
	syncedCh   chan struct{}
	onceSynced sync.Once
}

// NewRPCServer creates a new RPC server
func NewRPCServer(p2pSrv p2p.P2P, c *chain.Chain, txPool *txpool.TxPool) *RPCServer {
	return &RPCServer{
		p2pSrv:   p2pSrv,
		chain:    c,
		txPool:   txPool,
		logger:   slog.With("pkg", "rpcserver"),
		syncedCh: make(chan struct{}),
	}
}

// Start sets up the stream handler and starts listening
func (s *RPCServer) Start(ctx context.Context) {
	s.ctx = ctx

	m := drpcmux.New()
	pb.DRPCRegisterSync(m, s)
	server := drpcserver.New(m)

	s.p2pSrv.Host().SetStreamHandler("sync", func(stream network.Stream) {
		server.ServeOne(context.Background(), stream)
		s.logger.Info("handling sync stream", "fromPeer", stream.Conn().ID())
		stream.Close()
	})
	id := s.p2pSrv.Host().ID()
	s.logger.Info("RPC server started", "self", id)
}

func (s *RPCServer) Stop() {
	s.p2pSrv.Host().RemoveStreamHandler("sync")
	s.logger.Info("RPC server stopped")
}

func (s *RPCServer) GetStatus(ctx context.Context, req *pb.GetStatusRequest) (*pb.GetStatusResponse, error) {
	best := s.chain.BestBlock()
	genesis := s.chain.GenesisBlock()
	resp := &pb.GetStatusResponse{
		GenesisBlockId:   genesis.ID().Bytes(),
		BestBlockNum:     uint64(best.Number()),
		BestBlockId:      best.ID().Bytes(),
		SysNanoTimestamp: uint64(time.Now().Nanosecond()),
	}
	return resp, nil
}

func (s *RPCServer) SubscribeBlock(ch chan *NewBlockEvent) event.Subscription {
	return s.feedScope.Track(s.newBlockFeed.Subscribe(ch))
}

func (s *RPCServer) SubscribeBlockID(ch chan *NewBlockIDEvent) event.Subscription {
	return s.feedScope.Track(s.newBlockIdFeed.Subscribe(ch))
}

func (s *RPCServer) NotifyBlock(ctx context.Context, req *pb.NotifyBlockRequest) (*pb.NotifyBlockResponse, error) {
	escortedBlk := &block.EscortedBlock{}
	err := rlp.DecodeBytes(req.BlockBytes, escortedBlk)
	if err != nil {
		return &pb.NotifyBlockResponse{}, nil
	}

	peerID, err := libp2ppeer.Decode(req.PeerId)
	if err != nil {
		return &pb.NotifyBlockResponse{}, nil
	}
	if err == nil {
		s.newBlockFeed.Send(&NewBlockEvent{PeerID: peerID, NewBlock: escortedBlk})
	}
	return &pb.NotifyBlockResponse{}, nil
}

func (s *RPCServer) NotifyBlockID(ctx context.Context, req *pb.NotifyBlockIDRequest) (*pb.NotifyBlockIDResponse, error) {
	newBlockID := types.BytesToBytes32(req.BlockIdBytes)
	s.logger.Debug("Handling NotifyBlockID", "blockID", newBlockID)
	peerID, err := libp2ppeer.Decode(req.PeerId)
	if err != nil {
		return &pb.NotifyBlockIDResponse{}, nil
	}
	s.logger.Debug("putting newBlockID into newBlockIdFeed", "id", newBlockID)
	s.newBlockIdFeed.Send(&NewBlockIDEvent{PeerID: peerID, NewBlockID: newBlockID})
	return &pb.NotifyBlockIDResponse{}, nil
}

func (s *RPCServer) NotifyTx(ctx context.Context, req *pb.NotifyTxRequest) (*pb.NotifyTxResponse, error) {
	newTx := req.TxBytes
	s.logger.Info("Handling NotifyTx ", "tx", hex.EncodeToString(newTx))
	// TODO: handle new tx
	return &pb.NotifyTxResponse{}, nil
}

func (s *RPCServer) GetBlockByID(ctx context.Context, req *pb.GetBlockByIDRequest) (*pb.GetBlockByIDResponse, error) {
	blockID := types.BytesToBytes32(req.BlockIdBytes)
	s.logger.Debug("Handling GetBlockByID", "blockID", blockID)

	resp := &pb.GetBlockByIDResponse{}

	blk, err := s.chain.GetBlock(blockID)
	if err != nil {
		if !s.chain.IsNotFound(err) {
			s.logger.Error("failed to get block", "err", err)
		}
	} else {
		num := blk.Number()
		var escortQC *block.QuorumCert
		if num == s.chain.BestBlock().Number() {
			escortQC = s.chain.BestQC()
		} else {
			child, err := s.chain.GetTrunkBlock(num + 1)
			if err != nil {
				s.logger.Error("failed to get block id by number", "err", err)
			} else {
				escortQC = child.QC
			}
		}
		if escortQC != nil && blk != nil {
			escortedBlk := &block.EscortedBlock{Block: blk, EscortQC: escortQC}
			resp.BlockBytes, _ = rlp.EncodeToBytes(escortedBlk)
		} else {
			err = errors.New("no matching QC found")
			return nil, err
		}
	}

	return resp, nil
}

func (s *RPCServer) GetBlockIDByNumber(ctx context.Context, req *pb.GetBlockIDByNumberRequest) (*pb.GetBlockIDByNumberResponse, error) {
	s.logger.Info("Handling GetIDByNumber", "num", req.BlockNum)
	bestBlock := s.chain.BestBlock()
	blockID, err := s.chain.GetAncestorBlockID(bestBlock.ID(), uint32(req.BlockNum))
	resp := &pb.GetBlockIDByNumberResponse{}
	if err == nil {
		resp.BlockIdBytes = blockID.Bytes()
	}
	return resp, nil
}

func (s *RPCServer) GetBlocksFromNumber(ctx context.Context, req *pb.GetBlocksFromNumberRequest) (*pb.GetBlocksFromNumberResponse, error) {
	resp := &pb.GetBlocksFromNumberResponse{}
	var num uint32 = uint32(req.BlockNum)

	s.logger.Info("Handling GetBlocksFromNumber", "num", req.BlockNum)

	const maxBlocks = 1024
	const maxSize = 512 * 1024
	result := make([]*block.EscortedBlock, 0)
	var size common.StorageSize
	for size < maxSize && len(result) < maxBlocks {
		if num > s.chain.BestBlock().Number() {
			break
		}
		blk, err := s.chain.GetTrunkBlock(num)
		if err != nil {
			if !s.chain.IsNotFound(err) {
				s.logger.Debug("failed to get block raw by number", "err", err)
			}
			break
		}
		var nxtBlk *block.Block
		var escortQC *block.QuorumCert
		if s.chain.BestBlock().Number() == num {
			escortQC = s.chain.BestQC()
		} else {
			nxtBlk, err = s.chain.GetTrunkBlock(num + 1)
			if err != nil {
				s.logger.Warn("could not get next block", "num", num+1)
				break
			}
			escortQC = nxtBlk.QC
		}
		result = append(result, &block.EscortedBlock{Block: blk, EscortQC: escortQC})
		num++

		// FIXME: use actual QC size
		size += common.StorageSize(blk.Size() + 10000)
	}
	resp.BlockBytesList = make([][]byte, 0)
	for _, escortedBlk := range result {
		ebytes, _ := rlp.EncodeToBytes(escortedBlk)
		resp.BlockBytesList = append(resp.BlockBytesList, ebytes)
	}
	return resp, nil
}

func (s *RPCServer) GetTxs(ctx context.Context, req *pb.GetTxsRequest) (*pb.GetTxsResponse, error) {
	return &pb.GetTxsResponse{}, nil
}

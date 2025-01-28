package rpc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/meterio/supernova/block"
	"github.com/meterio/supernova/chain"
	"github.com/meterio/supernova/libs/co"
	"github.com/meterio/supernova/libs/message"
	"github.com/meterio/supernova/libs/p2p"
	"github.com/meterio/supernova/txpool"
	"github.com/meterio/supernova/types"
)

const protocolID = "/rpc/1.0.0"

const (
	GET_STATUS          = uint32(0)
	NEW_BLOCK           = uint32(1)
	NEW_BLOCK_ID        = uint32(2)
	NEW_TX              = uint32(3)
	GET_BLOCK_BY_ID     = uint32(4)
	GET_BLOCKS_FROM_NUM = uint32(5)
	GET_TXS             = uint32(6)
)

// RPCServer represents the RPC server
type RPCServer struct {
	ctx    context.Context
	p2pSrv p2p.P2P // libp2p host
	chain  *chain.Chain
	txPool *txpool.TxPool
	logger *slog.Logger
	goes   co.Goes

	// announcement
	newBlockFeed   event.Feed
	announcementCh chan *announcement
	feedScope      event.SubscriptionScope

	// sync
	syncedCh   chan struct{}
	onceSynced sync.Once
}

// NewRPCServer creates a new RPC server
func NewRPCServer(p2pSrv p2p.P2P, c *chain.Chain, txPool *txpool.TxPool) *RPCServer {
	return &RPCServer{
		p2pSrv:         p2pSrv,
		chain:          c,
		txPool:         txPool,
		logger:         slog.With("pkg", "rpc"),
		syncedCh:       make(chan struct{}),
		announcementCh: make(chan *announcement),
	}
}

// Start sets up the stream handler and starts listening
func (s *RPCServer) Start(ctx context.Context) {
	s.ctx = ctx
	// Set a stream handler for the RPC protocol
	s.p2pSrv.SetStreamHandler(p2p.RPCProtocolPrefix+"/ssz_snappy", s.handleRPC)
	s.goes.Go(s.announcementLoop)
}

func (s *RPCServer) Stop() {
	fmt.Println("rpc server stop")
}

func (s *RPCServer) Reply(stream network.Stream, msgType uint32, content interface{}, isResponse bool) error {
	raw, err := rlp.EncodeToBytes(content)
	if err != nil {
		return err
	}
	env := &message.RPCEnvelope{MsgType: msgType, Raw: raw, IsResponse: isResponse}
	pkt, err := env.MarshalSSZ()
	if err != nil {
		return err
	}
	_, err = stream.Write(pkt)
	return err
}
func MsgName(msgType uint32) string {
	switch msgType {
	case GET_STATUS:
		return "GetStatus"
	case NEW_BLOCK:
		return "NewBlock"
	case NEW_BLOCK_ID:
		return "NewBlockID"
	case NEW_TX:
		return "NewTx"
	case GET_BLOCK_BY_ID:
		return "GetBlockByID"
	case GET_BLOCKS_FROM_NUM:
		return "GetBlocksFromNum"
	case GET_TXS:
		return "GetTxs"
	default:
		return "Unknown"
	}
}

// handleStream handles incoming streams for the RPC protocol
func (s *RPCServer) handleRPC(stream network.Stream) {
	defer stream.Close()

	bs, err := io.ReadAll(stream)
	if err != nil {
		log.Printf("Error reading request: %s", err)
		return
	}

	env := &message.RPCEnvelope{}
	err = env.UnmarshalSSZ(bs)
	if err != nil {
		fmt.Println("Unmarshal error")
		return
	}

	msgName := MsgName(env.MsgType)
	remotePeer := stream.Conn().RemotePeer()
	s.logger.Debug("Recved rpc call", "msgType", msgName, "from", remotePeer)

	switch env.MsgType {
	case GET_STATUS:
		best := s.chain.BestBlock().Header()
		err = s.Reply(
			stream,
			GET_STATUS,
			&Status{
				GenesisBlockID: s.chain.GenesisBlock().ID(),
				SysTimestamp:   uint64(time.Now().Unix()),
				BestBlockID:    best.ID(),
				BestBlockNum:   best.Number(),
			}, true)
	case NEW_BLOCK:
		escortedBlk := &block.EscortedBlock{}
		err = rlp.DecodeBytes(env.Raw, escortedBlk)
		if err != nil {
			break
		}

		s.newBlockFeed.Send(&NewBlockEvent{
			EscortedBlock: escortedBlk,
		})

	case NEW_BLOCK_ID:
		newBlockID := types.BytesToBytes32(env.Raw[:32])
		select {
		case <-s.ctx.Done():
		case s.announcementCh <- &announcement{newBlockID, remotePeer}:
		}

	case NEW_TX:
		newTx := env.Raw
		s.txPool.StrictlyAdd(newTx)

	case GET_BLOCK_BY_ID:
		blockID := types.BytesToBytes32(env.Raw[:32])

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
				err = s.Reply(stream, GET_BLOCK_BY_ID, &block.EscortedBlock{Block: blk, EscortQC: escortQC}, true)
			} else {
				err = errors.New("no matching QC found")
			}

		}

	case GET_BLOCKS_FROM_NUM:
		var num uint32
		err = rlp.DecodeBytes(env.Raw, &num)
		if err != nil {
			break
		}

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
		err = s.Reply(stream, GET_BLOCKS_FROM_NUM, result, true)
	case GET_TXS:
		// FIXME: impl this

	}
	if err != nil {
		s.logger.Warn(fmt.Sprintf("failed to write %v response", msgName), "err", err)
		stream.Close()
	} else {
		s.logger.Debug(fmt.Sprintf(`replied rpc call %v`, msgName))
	}

}

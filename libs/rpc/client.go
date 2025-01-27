// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package rpc

import (
	"context"
	"io"

	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/meterio/supernova/block"
	"github.com/meterio/supernova/libs/message"
	"github.com/meterio/supernova/libs/p2p"
	"github.com/meterio/supernova/types"
)

type (
	// Status result of MsgGetStatus.
	Status struct {
		GenesisBlockID types.Bytes32
		SysTimestamp   uint64
		BestBlockID    types.Bytes32
		BestBlockNum   uint32
	}
)

// RPC defines RPC interface.
type RPC interface {
	Notify(ctx context.Context, msgCode uint64, arg interface{}) error
	Call(ctx context.Context, msgCode uint64, arg interface{}, result interface{}) error
	String() string
	Info(msg string, ctx ...interface{})
	Debug(msg string, ctx ...interface{})
	Warn(msg string, ctx ...interface{})
}

// GetStatus get status of remote peer.
func (s *RPCServer) GetStatus(pid peer.ID) (*Status, error) {
	env := &message.RPCEnvelope{MsgType: GET_STATUS, Raw: make([]byte, 0), IsResponse: false}
	stream, err := s.p2pSrv.Send(context.Background(), env, p2p.RPCProtocolPrefix, pid)
	if err != nil {
		return nil, err
	}

	status := &Status{}
	err = s.decode(stream, status)
	if err != nil {
		return nil, err
	}

	return status, nil
}

func (s *RPCServer) decode(stream network.Stream, decoded interface{}) error {
	bs, err := io.ReadAll(stream)
	if err != nil {
		return err
	}
	err = rlp.DecodeBytes(bs, decoded)
	if err != nil {
		return err
	}
	return nil
}

// NotifyNewBlockID notify new block ID to remote peer.
func (s *RPCServer) NotifyNewBlockID(pid peer.ID, id types.Bytes32) error {
	env := &message.RPCEnvelope{MsgType: NEW_BLOCK_ID, Raw: id[:], IsResponse: false}
	_, err := s.p2pSrv.Send(context.Background(), env, p2p.RPCProtocolPrefix, pid)
	return err
}

// NotifyNewBlock notify new block to remote peer.
func (s *RPCServer) NotifyNewBlock(pid peer.ID, escortedBlk *block.EscortedBlock) error {
	raw, err := rlp.EncodeToBytes(escortedBlk)
	if err != nil {
		return err
	}
	env := &message.RPCEnvelope{MsgType: NEW_BLOCK, Raw: raw, IsResponse: false}
	_, err = s.p2pSrv.Send(context.Background(), env, p2p.RPCProtocolPrefix, pid)
	return err
}

// NotifyNewTx notify new tx to remote peer.
func (s *RPCServer) NotifyNewTx(pid peer.ID, tx cmttypes.Tx) error {
	env := &message.RPCEnvelope{MsgType: NEW_TX, Raw: tx, IsResponse: false}
	_, err := s.p2pSrv.Send(context.Background(), env, p2p.RPCProtocolPrefix, pid)
	return err
}

// GetBlockByID query block from remote peer by given block ID.
// It may return nil block even no error.
func (s *RPCServer) GetBlockByID(pid peer.ID, id types.Bytes32) (*block.EscortedBlock, error) {
	env := &message.RPCEnvelope{MsgType: GET_BLOCK_BY_ID, Raw: id[:], IsResponse: false}
	stream, err := s.p2pSrv.Send(context.Background(), env, p2p.RPCProtocolPrefix, pid)
	if err != nil {
		return nil, err
	}
	escortedBlock := &block.EscortedBlock{}
	err = s.decode(stream, escortedBlock)
	if err != nil {
		return nil, err
	}
	s.logger.Debug("GetBlockByID success", "id", id)
	return escortedBlock, err
}

// GetBlocksFromNumber get a batch of blocks starts with num from remote peer.
func (s *RPCServer) GetBlocksFromNumber(pid peer.ID, num uint32) ([]*block.EscortedBlock, error) {
	raw, err := rlp.EncodeToBytes(num)
	if err != nil {
		return make([]*block.EscortedBlock, 0), err
	}
	env := &message.RPCEnvelope{MsgType: GET_BLOCKS_FROM_NUM, Raw: raw, IsResponse: false}
	stream, err := s.p2pSrv.Send(context.Background(), env, p2p.RPCProtocolPrefix, pid)
	if err != nil {
		return nil, err
	}
	escortedBlocks := make([]*block.EscortedBlock, 0)
	err = s.decode(stream, escortedBlocks)
	if err != nil {
		return nil, err
	}

	s.logger.Debug("GetBlocksFromNumber success", "num", num, "len", len(escortedBlocks))
	return escortedBlocks, nil
}

// GetTxs get txs from remote peer.
func (s *RPCServer) GetTxs(pid peer.ID) (types.Transactions, error) {
	env := &message.RPCEnvelope{MsgType: GET_TXS, Raw: make([]byte, 0), IsResponse: false}
	stream, err := s.p2pSrv.Send(context.Background(), env, p2p.RPCProtocolPrefix, pid)
	if err != nil {
		return nil, err
	}
	var txs types.Transactions
	err = s.decode(stream, txs)
	if err != nil {
		return nil, err
	}

	return txs, nil

}

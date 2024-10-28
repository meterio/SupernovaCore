// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package proto

import (
	"context"

	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/meterio/supernova/block"
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
func GetStatus(ctx context.Context, rpc RPC) (*Status, error) {
	var status Status
	if err := rpc.Call(ctx, MsgGetStatus, &struct{}{}, &status); err != nil {
		return nil, err
	}
	return &status, nil
}

// NotifyNewBlockID notify new block ID to remote peer.
func NotifyNewBlockID(ctx context.Context, rpc RPC, id types.Bytes32) error {
	return rpc.Notify(ctx, MsgNewBlockID, &id)
}

// NotifyNewBlock notify new block to remote peer.
func NotifyNewBlock(ctx context.Context, rpc RPC, block *block.EscortedBlock) error {
	return rpc.Notify(ctx, MsgNewBlock, block)
}

// NotifyNewTx notify new tx to remote peer.
func NotifyNewTx(ctx context.Context, rpc RPC, tx cmttypes.Tx) error {
	return rpc.Notify(ctx, MsgNewTx, tx)
}

// GetBlockByID query block from remote peer by given block ID.
// It may return nil block even no error.
func GetBlockByID(ctx context.Context, rpc RPC, id types.Bytes32) (rlp.RawValue, error) {
	var result []rlp.RawValue
	if err := rpc.Call(ctx, MsgGetBlockByID, id, &result); err != nil {

		rpc.Debug("GetBlockByID failed", "id", id, "err", err)
		return nil, err
	}
	if len(result) == 0 {
		rpc.Debug("GetBlockByID empty", "id", id)
		return nil, nil
	}
	rpc.Debug("GetBlockByID success", "id", id)
	return result[0], nil
}

// GetBlockIDByNumber query block ID from remote peer by given number.
func GetBlockIDByNumber(ctx context.Context, rpc RPC, num uint32) (types.Bytes32, error) {
	var id types.Bytes32
	if err := rpc.Call(ctx, MsgGetBlockIDByNumber, num, &id); err != nil {
		rpc.Debug("GetBlockIDByNumber failed", "err", err)
		return types.Bytes32{}, err
	}
	rpc.Debug("GetBlockIDByNumber success", "id", id)
	return id, nil
}

// GetBlocksFromNumber get a batch of blocks starts with num from remote peer.
func GetBlocksFromNumber(ctx context.Context, rpc RPC, num uint32) ([]rlp.RawValue, error) {
	var blocks []rlp.RawValue
	if err := rpc.Call(ctx, MsgGetBlocksFromNumber, num, &blocks); err != nil {
		rpc.Warn("GetBlocksFromNumber failed", "num", num, "err", err)
		return nil, err
	}
	rpc.Debug("GetBlocksFromNumber success", "num", num, "len", len(blocks))
	return blocks, nil
}

// GetTxs get txs from remote peer.
func GetTxs(ctx context.Context, rpc RPC) (types.Transactions, error) {
	var txs types.Transactions
	if err := rpc.Call(ctx, MsgGetTxs, &struct{}{}, &txs); err != nil {
		return nil, err
	}
	return txs, nil
}

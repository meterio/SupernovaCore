// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package jsonrpc

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"strconv"

	cmtabci "github.com/cometbft/cometbft/v2/abci/types"
	cmtproxy "github.com/cometbft/cometbft/v2/proxy"
	ctypes "github.com/cometbft/cometbft/v2/rpc/core/types"
	"github.com/meterio/supernova/chain"
	"github.com/meterio/supernova/txpool"
)

// CheckTx result.
type ResultBroadcastTx struct {
	Code      uint32 `json:"code"`
	Data      []byte `json:"data"`
	Log       string `json:"log"`
	Codespace string `json:"codespace"`

	Hash []byte `json:"hash"`
}

type Result struct {
	JSONRPC string            `json:"jsonrpc"`
	Id      uint32            `json:"id"`
	Result  ResultBroadcastTx `json:"result"`
	Error   string            `json:"error"`
}

const MaxUint32 = 1<<32 - 1

type JSONRPCAPI struct {
	chain         *chain.Chain
	txPool        *txpool.TxPool
	proxyAppQuery cmtproxy.AppConnQuery
	logger        *slog.Logger
}

func New(chain *chain.Chain, txPool *txpool.TxPool, proxyAppQuery cmtproxy.AppConnQuery) *JSONRPCAPI {
	return &JSONRPCAPI{
		chain,
		txPool,
		proxyAppQuery,
		slog.With("api", "query"),
	}
}

func (a *JSONRPCAPI) HandleBroadcastTx(params json.RawMessage) (output json.RawMessage, err error) {
	var m map[string]interface{}
	if err := json.Unmarshal(params, &m); err != nil {
		panic(err)
	}

	txBase64 := m["tx"].(string)
	decodedTx, err := base64.StdEncoding.DecodeString(txBase64)
	if err != nil {
		panic(err)
	}
	a.txPool.Add(decodedTx)
	return json.RawMessage{}, nil
}

func (a *JSONRPCAPI) HandleABCIQuery(params json.RawMessage) (output json.RawMessage, err error) {
	var m map[string]interface{}
	if err := json.Unmarshal(params, &m); err != nil {
		panic(err)
	}

	a.logger.Info("handle ABCI query", "path", m["path"], "data", m["data"], "height", m["height"], "prove", m["prove"])
	height, _ := strconv.ParseInt(m["height"].(string), 10, 64)
	databytes, _ := hex.DecodeString(m["data"].(string))
	resQuery, err := a.proxyAppQuery.Query(context.TODO(), &cmtabci.QueryRequest{
		Path:   m["path"].(string),
		Data:   databytes,
		Height: height,
		Prove:  m["prove"].(bool),
	})
	if err != nil {
		a.logger.Error("proxy app failed to process query", "err", err)
		return nil, err
	}

	// fmt.Println("query result: ")
	// fmt.Println("code: ", resQuery.Code)
	// fmt.Println("codespace: ", resQuery.Codespace)
	// fmt.Println("height: ", resQuery.Height)
	// fmt.Println("index: ", resQuery.Index)
	// fmt.Println("key: ", resQuery.Key)
	// fmt.Println("log: ", resQuery.Log)
	// fmt.Println("info: ", resQuery.Info)
	// fmt.Println("proofOps: ", resQuery.ProofOps)
	// fmt.Println("value: ", resQuery.Value)

	result := ctypes.ResultABCIQuery{Response: *resQuery}

	a.logger.Info("query result", "code", result.Response.Code, "log", result.Response.Log, "info", result.Response.Info)

	resultBytes, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	return resultBytes, nil
}

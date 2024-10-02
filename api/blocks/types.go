// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package blocks

import (
	"encoding/hex"
	"math/big"

	cmtbytes "github.com/cometbft/cometbft/libs/bytes"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/meterio/supernova/block"
	"github.com/meterio/supernova/tx"
	"github.com/meterio/supernova/types"
)

type JSONBlockSummary struct {
	Number           uint32            `json:"number"`
	ID               types.Bytes32     `json:"id"`
	Size             uint32            `json:"size"`
	ParentID         types.Bytes32     `json:"parentID"`
	Timestamp        uint64            `json:"timestamp"`
	GasUsed          uint64            `json:"gasUsed"`
	TotalScore       uint64            `json:"totalScore"`
	TxsRoot          cmtbytes.HexBytes `json:"txsRoot"`
	TxsFeatures      uint32            `json:"txsFeatures"`
	Signer           common.Address    `json:"signer"`
	IsTrunk          bool              `json:"isTrunk"`
	BlockType        string            `json:"blockType"`
	LastKBlockHeight uint32            `json:"lastKBlockHeight"`
	QC               *QC               `json:"qc"`
	Nonce            uint64            `json:"nonce"`
	Epoch            uint64            `json:"epoch"`
}

type JSONCollapsedBlock struct {
	*JSONBlockSummary
	Transactions []string `json:"transactions"`
}

type JSONClause struct {
	To    *common.Address      `json:"to"`
	Value math.HexOrDecimal256 `json:"value"`
	Token uint32               `json:"token"`
	Data  string               `json:"data"`
}

type JSONTransfer struct {
	Sender    common.Address        `json:"sender"`
	Recipient common.Address        `json:"recipient"`
	Amount    *math.HexOrDecimal256 `json:"amount"`
	Token     uint32                `json:"token"`
}

type JSONEvent struct {
	Address common.Address  `json:"address"`
	Topics  []types.Bytes32 `json:"topics"`
	Data    string          `json:"data"`
}

type JSONOutput struct {
	ContractAddress *common.Address `json:"contractAddress"`
	Events          []*JSONEvent    `json:"events"`
	Transfers       []*JSONTransfer `json:"transfers"`
}

type JSONEmbeddedTx struct {
	Hash string `json:"hash"`
	Raw  string `json:"chainTag"`
}

type JSONEpoch struct {
	EpochID uint64 `json:"epochID"`
	Number  uint32 `json:"number"`
	Nonce   uint64 `json:"nonce"`
}

func buildJSONEpoch(blk *block.Block) *JSONEpoch {
	return &JSONEpoch{
		Nonce:   blk.Nonce(),
		EpochID: blk.GetBlockEpoch(),
		Number:  blk.Number(),
	}
}

type JSONExpandedBlock struct {
	*JSONBlockSummary
	Transactions []*JSONEmbeddedTx `json:"transactions"`
}

func buildJSONBlockSummary(blk *block.Block, isTrunk bool, baseFeePerGas *big.Int) *JSONBlockSummary {
	header := blk.Header()
	signer, _ := header.Signer()

	var epoch uint64
	blockType := ""
	if blk.IsKBlock() {
		blockType = "KBlock"
	} else if blk.IsMBlock() {
		blockType = "MBlock"
	} else if blk.IsSBlock() {
		blockType = "SBlock"
	}
	isKBlock := header.BlockType == block.KBlockType
	if isTrunk && isKBlock {
		epoch = blk.QC.EpochID
	} else {
		epoch = blk.QC.EpochID
	}
	result := &JSONBlockSummary{
		Number:    header.Number(),
		ID:        header.ID(),
		ParentID:  header.ParentID,
		Timestamp: header.Timestamp,

		Signer:           signer,
		Size:             uint32(blk.Size()),
		TxsRoot:          header.TxsRoot,
		IsTrunk:          isTrunk,
		BlockType:        blockType,
		LastKBlockHeight: header.LastKBlockHeight,
		Epoch:            epoch,
		Nonce:            blk.Nonce(),
	}
	var err error
	if blk.QC != nil {
		result.QC, err = convertQC(blk.QC)
		if err != nil {
			return nil
		}
	}

	return result
}

func buildJSONEmbeddedTxs(txs tx.Transactions) []*JSONEmbeddedTx {
	jTxs := make([]*JSONEmbeddedTx, 0, len(txs))
	for _, tx := range txs {
		jTxs = append(jTxs, &JSONEmbeddedTx{Hash: hex.EncodeToString(tx.Hash()), Raw: hex.EncodeToString(tx)})

	}
	return jTxs
}

type QC struct {
	QCHeight         uint32 `json:"qcHeight"`
	QCRound          uint32 `json:"qcRound"`
	VoterBitArrayStr string `json:"voterBitArrayStr"`
	EpochID          uint64 `json:"epochID"`
}

type QCWithRaw struct {
	QCHeight         uint32 `json:"qcHeight"`
	QCRound          uint32 `json:"qcRound"`
	VoterBitArrayStr string `json:"voterBitArrayStr"`
	EpochID          uint64 `json:"epochID"`
	Raw              string `json:"raw"`
}

type CommitteeMember struct {
	Index uint32 `json:"index"`
	// Name    string `json:"name"`
	NetAddr string `json:"netAddr"`
	PubKey  string `json:"pubKey"`
}

func convertQC(qc *block.QuorumCert) (*QC, error) {
	return &QC{
		QCHeight:         qc.QCHeight,
		QCRound:          qc.QCRound,
		VoterBitArrayStr: qc.BitArray.String(),
		EpochID:          qc.EpochID,
	}, nil
}

func convertQCWithRaw(qc *block.QuorumCert) (*QCWithRaw, error) {
	raw := hex.EncodeToString(qc.ToBytes())
	return &QCWithRaw{
		QCHeight:         qc.QCHeight,
		QCRound:          qc.QCRound,
		VoterBitArrayStr: qc.BitArray.String(),
		EpochID:          qc.EpochID,
		Raw:              raw,
	}, nil
}

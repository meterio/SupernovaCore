// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package blocks

import (
	"encoding/hex"
	"math/big"

	cmtbytes "github.com/cometbft/cometbft/libs/bytes"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/meterio/meter-pov/block"
	"github.com/meterio/meter-pov/meter"
	"github.com/meterio/meter-pov/tx"
)

type JSONBlockSummary struct {
	Number           uint32             `json:"number"`
	ID               meter.Bytes32      `json:"id"`
	Size             uint32             `json:"size"`
	ParentID         meter.Bytes32      `json:"parentID"`
	Timestamp        uint64             `json:"timestamp"`
	GasUsed          uint64             `json:"gasUsed"`
	TotalScore       uint64             `json:"totalScore"`
	TxsRoot          cmtbytes.HexBytes  `json:"txsRoot"`
	TxsFeatures      uint32             `json:"txsFeatures"`
	Signer           meter.Address      `json:"signer"`
	IsTrunk          bool               `json:"isTrunk"`
	BlockType        string             `json:"blockType"`
	LastKBlockHeight uint32             `json:"lastKBlockHeight"`
	CommitteeInfo    []*CommitteeMember `json:"committee"`
	QC               *QC                `json:"qc"`
	Nonce            uint64             `json:"nonce"`
	Epoch            uint64             `json:"epoch"`
}

type JSONCollapsedBlock struct {
	*JSONBlockSummary
	Transactions []string `json:"transactions"`
}

type JSONClause struct {
	To    *meter.Address       `json:"to"`
	Value math.HexOrDecimal256 `json:"value"`
	Token uint32               `json:"token"`
	Data  string               `json:"data"`
}

type JSONTransfer struct {
	Sender    meter.Address         `json:"sender"`
	Recipient meter.Address         `json:"recipient"`
	Amount    *math.HexOrDecimal256 `json:"amount"`
	Token     uint32                `json:"token"`
}

type JSONEvent struct {
	Address meter.Address   `json:"address"`
	Topics  []meter.Bytes32 `json:"topics"`
	Data    string          `json:"data"`
}

type JSONOutput struct {
	ContractAddress *meter.Address  `json:"contractAddress"`
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
	isKBlock := header.BlockType() == block.KBlockType
	if isTrunk && isKBlock {
		epoch = blk.QC.EpochID
	} else if len(blk.CommitteeInfos.CommitteeInfo) > 0 {
		epoch = blk.CommitteeInfos.Epoch
	} else {
		epoch = blk.QC.EpochID
	}
	result := &JSONBlockSummary{
		Number:    header.Number(),
		ID:        header.ID(),
		ParentID:  header.ParentID(),
		Timestamp: header.Timestamp(),

		Signer:           signer,
		Size:             uint32(blk.Size()),
		TxsRoot:          header.TxsRoot(),
		IsTrunk:          isTrunk,
		BlockType:        blockType,
		LastKBlockHeight: header.LastKBlockHeight(),
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

	if len(blk.CommitteeInfos.CommitteeInfo) > 0 {
		result.CommitteeInfo = convertCommitteeList(blk.CommitteeInfos)
	} else {
		result.CommitteeInfo = make([]*CommitteeMember, 0)
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
		VoterBitArrayStr: qc.VoterBitArrayStr,
		EpochID:          qc.EpochID,
	}, nil
}

func convertQCWithRaw(qc *block.QuorumCert) (*QCWithRaw, error) {
	raw := hex.EncodeToString(qc.ToBytes())
	return &QCWithRaw{
		QCHeight:         qc.QCHeight,
		QCRound:          qc.QCRound,
		VoterBitArrayStr: qc.VoterBitArrayStr,
		EpochID:          qc.EpochID,
		Raw:              raw,
	}, nil
}

func convertCommitteeList(cml block.CommitteeInfos) []*CommitteeMember {
	committeeList := make([]*CommitteeMember, len(cml.CommitteeInfo))

	for i, cm := range cml.CommitteeInfo {
		committeeList[i] = &CommitteeMember{
			Index: cm.Index,
			// Name:    "",
			NetAddr: cm.NetAddr.IP.String(),
			PubKey:  hex.EncodeToString(cm.PubKey),
		}
	}
	return committeeList
}

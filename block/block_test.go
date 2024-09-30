// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package block_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/ethereum/go-ethereum/rlp"
	. "github.com/meterio/meter-pov/block"
	"github.com/meterio/meter-pov/meter"

	// "crypto/rand"
	// cmn "github.com/meterio/meter-pov/libs/common"

	"github.com/meterio/meter-pov/types"
)

func TestSerialize(t *testing.T) {

	now := uint64(time.Now().UnixNano())

	var (
		emptyRoot meter.Bytes32 = meter.BytesToBytes32([]byte("0"))
	)

	block := new(Builder).
		Timestamp(now).
		ParentID(emptyRoot).
		Build()

	h := block.Header()

	txs := block.Transactions()
	body := block.Body()
	txsRootHash := txs.RootHash()

	assert.Equal(t, body.Txs, txs)
	assert.Equal(t, Compose(h, txs), block)
	assert.Equal(t, now, h.Timestamp())
	assert.Equal(t, emptyRoot, h.ParentID())
	assert.Equal(t, txsRootHash, h.TxsRoot())

	// key, _ := crypto.HexToECDSA(privKey)
	// _, _ := crypto.Sign(block.Header().SigningHash().Bytes(), key)

	// block = block.WithSignature(sig)

	qc := QuorumCert{QCHeight: 1, QCRound: 1, EpochID: 1, VoterAggSig: []byte{1, 2, 3}, VoterBitArrayStr: "**-", VoterMsgHash: [32]byte{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1}, VoterViolation: []*Violation{}}
	block.SetQC(&qc)

	addr := types.NetAddress{IP: []byte{}, Port: 4444}
	committeeInfo := []CommitteeInfo{CommitteeInfo{
		Name:    "Testee",
		Index:   0,
		PubKey:  []byte{},
		NetAddr: addr,
	}}
	_, err := rlp.EncodeToBytes(addr)
	_, err = rlp.EncodeToBytes(committeeInfo)
	assert.Equal(t, err, nil)

	block.SetCommitteeInfo(committeeInfo)

	// fmt.Println("BEFORE KBlockData data:", kBlockData)
	// fmt.Println("BEFORE block.KBlockData:", block.KBlockData)
	// fmt.Println("BEFORE block.CommitteeInfo: ", committeeInfo)
	// fmt.Println("BEFORE block.CommitteeInfo: ", block.CommitteeInfos)
	// fmt.Println("BEFORE BLOCK:", block)
	data, err := rlp.EncodeToBytes(block)
	assert.Equal(t, err, nil)
	// fmt.Println("BLOCK SERIALIZED TO:", data)

	b := &Block{}

	err = rlp.DecodeBytes(data, b)
	assert.Equal(t, err, nil)
	// fmt.Println("AFTER BLOCK:", b)
	assert.Equal(t, err, nil)

	ci, err := b.GetCommitteeInfo()
	assert.Equal(t, err, nil)
	assert.Equal(t, len(committeeInfo), len(ci))
	assert.Equal(t, committeeInfo[0].Name, ci[0].Name)

	dqc := b.GetQC()
	assert.Equal(t, qc.QCHeight, dqc.QCHeight)
}

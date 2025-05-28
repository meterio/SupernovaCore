// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package block_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/ethereum/go-ethereum/rlp"
	. "github.com/meterio/supernova/block"

	// "crypto/rand"
	// cmn "github.com/meterio/supernova/libs/common"

	"github.com/meterio/supernova/types"
)

func TestSerialize(t *testing.T) {

	nowNano := uint64(time.Now().UnixNano())

	var (
		emptyRoot types.Bytes32 = types.BytesToBytes32([]byte("0"))
	)

	block := new(Builder).
		NanoTimestamp(nowNano).
		ParentID(emptyRoot).
		Build()

	h := block.Header()

	txs := block.Transactions()
	body := block.Body()
	txsRootHash := txs.RootHash()

	assert.Equal(t, body.Txs, txs)
	assert.Equal(t, Compose(h, txs), block)
	assert.Equal(t, nowNano, h.NanoTimestamp)

	assert.Equal(t, emptyRoot, h.ParentID)
	assert.Equal(t, txsRootHash, h.TxsRoot)

	// key, _ := crypto.HexToECDSA(privKey)
	// _, _ := crypto.Sign(block.Header().SigningHash().Bytes(), key)

	// block = block.WithSignature(sig)

	qc := QuorumCert{Epoch: 1, Round: 1, AggSig: []byte{1, 2, 3}}
	block.SetQC(&qc)

	addr := types.NetAddress{IP: []byte{}, Port: 4444}
	_, err := rlp.EncodeToBytes(addr)
	assert.Equal(t, err, nil)

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

	assert.Equal(t, err, nil)

}

// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package types

import (
	"github.com/ethereum/go-ethereum/common"
)

// Constants of block chain.
const (
	// --------------------- Epoch --------------------------
	MTR  = byte(0)
	MTRG = byte(1)
	// minimum height for committee relay.

	//  ------------------ Basics ----------------------------
	BlockInterval uint64 = 10          // time interval between two consecutive blocks.
	BaseTxGas     uint64 = ParamsTxGas // 21000
	TxGas         uint64 = 5000

	// InitialGasLimit was 10 *1000 *100, only accommodates 476 Txs, block size 61k, so change to 200M
	SloadGas       uint64 = 200 // EIP158 gas table
	SstoreSetGas   uint64 = ParamsSstoreSetGas
	SstoreResetGas uint64 = ParamsSstoreResetGas

	NBlockDelayToEnableValidatorSet = 4
)

// Keys of governance params.
var (
	// Keys
	KeyExecutorAddress = BytesToBytes32([]byte("executor"))

	ZeroAddress = common.HexToAddress("0x0000000000000000000000000000000000000000")
)

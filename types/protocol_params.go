// Copyright 2015 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package types

import "math/big"

const (
	MaximumExtraDataSize        uint64 = 32    // Maximum size extra data may be after Genesis.
	ExpByteGas                  uint64 = 10    // Times ceil(log256(exponent)) for the EXP instruction.
	ParamsSloadGas              uint64 = 50    // Multiplied by the number of 32-byte words that are copied (round up) for any *COPY operation and added.
	CallValueTransferGas        uint64 = 9000  // Paid for CALL when the value transfer is non-zero.
	CallNewAccountGas           uint64 = 25000 // Paid for CALL when the destination address didn't exist prior.
	ParamsTxGas                 uint64 = 21000 // Per transaction not creating a contract. NOTE: Not payable on data of calls between transactions.
	ParamsTxGasContractCreation uint64 = 53000 // Per transaction that creates a contract. NOTE: Not payable on data of calls between transactions.
	TxDataZeroGas               uint64 = 4     // Per byte of data attached to a transaction that equals zero. NOTE: Not payable on data of calls between transactions.
	QuadCoeffDiv                uint64 = 512   // Divisor for the quadratic particle of the memory cost equation.
	LogDataGas                  uint64 = 8     // Per byte in a LOG* operation's data.
	CallStipend                 uint64 = 2300  // Free gas given at beginning of call.

	Sha3Gas     uint64 = 30 // Once per SHA3 operation.
	Sha3WordGas uint64 = 4  // Once per word of the SHA3 operation's data.

	ParamsSstoreSetGas   uint64 = 20000 // Once per SLOAD operation.
	ParamsSstoreResetGas uint64 = 5000  // Once per SSTORE operation if the zeroness changes from zero.
	SstoreClearGas       uint64 = 5000  // Once per SSTORE operation if the zeroness doesn't change.
	SstoreRefundGas      uint64 = 15000 // Once per SSTORE operation if the zeroness changes to zero.

	InitCodeWordGas uint64 = 2 // Once per word of the init code when creating a contract.

	NetSstoreNoopGas  uint64 = 200   // Once per SSTORE operation if the value doesn't change.
	NetSstoreInitGas  uint64 = 20000 // Once per SSTORE operation from clean zero.
	NetSstoreCleanGas uint64 = 5000  // Once per SSTORE operation from clean non-zero.
	NetSstoreDirtyGas uint64 = 200   // Once per SSTORE operation from dirty.

	NetSstoreClearRefund      uint64 = 15000 // Once per SSTORE operation for clearing an originally existing storage slot
	NetSstoreResetRefund      uint64 = 4800  // Once per SSTORE operation for resetting to the original non-zero value
	NetSstoreResetClearRefund uint64 = 19800 // Once per SSTORE operation for resetting to the original zero value

	WarmStorageReadCostEIP2929 = uint64(100) // WARM_STORAGE_READ_COST

	JumpdestGas             uint64 = 1     // Once per JUMPDEST operation.
	EpochDuration           uint64 = 30000 // Duration between proof-of-work epochs.
	CallGas                 uint64 = 40    // Once per CALL operation & message call transaction.
	CreateDataGas           uint64 = 200   //
	CallCreateDepth         uint64 = 1024  // Maximum depth of call/create stack.
	ExpGas                  uint64 = 10    // Once per EXP instruction
	LogGas                  uint64 = 375   // Per LOG* operation.
	CopyGas                 uint64 = 3     //
	StackLimit              uint64 = 1024  // Maximum size of VM stack allowed.
	TierStepGas             uint64 = 0     // Once per operation, for a selection of them.
	LogTopicGas             uint64 = 375   // Multiplied by the * of the LOG*, per LOG transaction. e.g. LOG0 incurs 0 * c_txLogTopicGas, LOG4 incurs 4 * c_txLogTopicGas.
	CreateGas               uint64 = 32000 // Once per CREATE operation & contract-creation transaction.
	Create2Gas              uint64 = 32000 // Once per CREATE2 operation
	SuicideRefundGas        uint64 = 24000 // Refunded following a suicide operation.
	MemoryGas               uint64 = 3     // Times the address of the (highest referenced byte in memory + 1). NOTE: referencing happens on read, write and in instructions such as RETURN and CALL.
	TxDataNonZeroGas        uint64 = 68    // Per byte of data attached to a transaction that is not equal to zero. NOTE: Not payable on data of calls between transactions.
	TxDataNonZeroGasEIP2028 uint64 = 16    // Per byte of non zero data attached to a transaction after EIP 2028 (part in Istanbul)

	MaxCodeSize     = 24576           // Maximum bytecode to permit for a contract
	MaxInitCodeSize = 2 * MaxCodeSize // Maximum initcode to permit in a creation transaction and create instructions

	Keccak256Gas     uint64 = 30 // Once per KECCAK256 operation.
	Keccak256WordGas uint64 = 6  // Once per word of the KECCAK256 operation's data.

	// Precompiled contract gas prices

	EcrecoverGas            uint64 = 3000   // Elliptic curve sender recovery gas price
	Sha256BaseGas           uint64 = 60     // Base price for a SHA256 operation
	Sha256PerWordGas        uint64 = 12     // Per-word price for a SHA256 operation
	Ripemd160BaseGas        uint64 = 600    // Base price for a RIPEMD160 operation
	Ripemd160PerWordGas     uint64 = 120    // Per-word price for a RIPEMD160 operation
	IdentityBaseGas         uint64 = 15     // Base price for a data copy operation
	IdentityPerWordGas      uint64 = 3      // Per-work price for a data copy operation
	ModExpQuadCoeffDiv      uint64 = 20     // Divisor for the quadratic particle of the big int modular exponentiation
	Bn256AddGas             uint64 = 500    // Gas needed for an elliptic curve addition
	Bn256ScalarMulGas       uint64 = 40000  // Gas needed for an elliptic curve scalar multiplication
	Bn256PairingBaseGas     uint64 = 100000 // Base price for an elliptic curve pairing check
	Bn256PairingPerPointGas uint64 = 80000  // Per-point price for an elliptic curve pairing check
)

var (
	DifficultyBoundDivisor = big.NewInt(2048)   // The bound divisor of the difficulty, used in the update calculations.
	GenesisDifficulty      = big.NewInt(131072) // Difficulty of the Genesis block.
	MinimumDifficulty      = big.NewInt(131072) // The minimum that the difficulty may ever be.
	DurationLimit          = big.NewInt(13)     // The decision boundary on the blocktime duration used to determine whether difficulty should go up or not.
)

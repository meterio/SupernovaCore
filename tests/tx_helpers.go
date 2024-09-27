package tests

import (
	"crypto/ecdsa"
	"math/big"
	"math/rand"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/meterio/meter-pov/meter"
	"github.com/meterio/meter-pov/tx"
)

func BuildCallTx(chainTag byte, bestRef uint32, toAddr *meter.Address, data []byte, nonce uint64, key *ecdsa.PrivateKey) *tx.Transaction {
	builder := new(tx.Builder)
	builder.ChainTag(chainTag).
		BlockRef(tx.NewBlockRef(bestRef)).
		Expiration(720).
		GasPriceCoef(0).
		Gas(meter.BaseTxGas * 100). //buffer for builder.Build().IntrinsicGas()
		DependsOn(nil).
		Nonce(nonce)

	builder.Clause(
		tx.NewClause(toAddr).WithValue(big.NewInt(0)).WithToken(meter.MTRG).WithData(data),
	)
	trx := builder.Build()
	sig, _ := crypto.Sign(trx.SigningHash().Bytes(), key)
	trx = trx.WithSignature(sig)
	return trx
}

func BuildMintTx(chainTag byte, bestRef uint32, to meter.Address, amount *big.Int, token byte, nonce uint64) *tx.Transaction {
	builder := new(tx.Builder)
	builder.ChainTag(chainTag).
		BlockRef(tx.NewBlockRef(bestRef)).
		Expiration(720).
		GasPriceCoef(0).
		Gas(meter.BaseTxGas * 10). //buffer for builder.Build().IntrinsicGas()
		DependsOn(nil).
		Nonce(nonce)

	builder.Clause(
		tx.NewClause(&to).WithValue(amount).WithToken(token).WithData([]byte{}),
	)
	trx := builder.Build()
	// sig, _ := crypto.Sign(trx.SigningHash().Bytes(), key)
	// trx = trx.WithSignature(sig)
	return trx
}

func BuildTransferTx(chainTag byte, bestRef uint32, to meter.Address, amount *big.Int, signerKey *ecdsa.PrivateKey) *tx.Transaction {
	builder := new(tx.Builder)
	builder.ChainTag(chainTag).
		BlockRef(tx.NewBlockRef(bestRef)).
		Expiration(720).
		GasPriceCoef(0).
		Gas(meter.BaseTxGas * 2).
		DependsOn(nil).
		Nonce(uint64(rand.Intn(9999)))

	builder.Clause(
		tx.NewClause(&to).WithValue(amount).WithToken(meter.MTRG).WithData(make([]byte, 0)),
	)
	trx := builder.Build()
	sig, _ := crypto.Sign(trx.SigningHash().Bytes(), signerKey)
	trx = trx.WithSignature(sig)
	return trx
}

func BuildContractCallTx(chainTag byte, bestRef uint32, to meter.Address, data []byte, signerKey *ecdsa.PrivateKey) *tx.Transaction {
	builder := new(tx.Builder)
	builder.ChainTag(chainTag).
		BlockRef(tx.NewBlockRef(bestRef)).
		Expiration(720).
		GasPriceCoef(0).
		Gas(meter.BaseTxGas * 10).
		DependsOn(nil).
		Nonce(uint64(rand.Intn(9999)))

	builder.Clause(
		tx.NewClause(&to).WithValue(big.NewInt(0)).WithToken(meter.MTRG).WithData(data),
	)
	trx := builder.Build()
	sig, _ := crypto.Sign(trx.SigningHash().Bytes(), signerKey)
	trx = trx.WithSignature(sig)
	return trx
}

func BuildContractDeployTx(chainTag byte, bestRef uint32, data []byte, signerKey *ecdsa.PrivateKey) *tx.Transaction {
	nonce := uint64(0)
	builder := new(tx.Builder)
	builder.ChainTag(chainTag).
		BlockRef(tx.NewBlockRef(bestRef)).
		Expiration(720).
		GasPriceCoef(0).
		Gas(2200000).
		DependsOn(nil).
		Nonce(nonce)

	builder.Clause(
		tx.NewClause(nil).WithValue(big.NewInt(0)).WithToken(meter.MTRG).WithData(data),
	)
	trx := builder.Build()
	sig, _ := crypto.Sign(trx.SigningHash().Bytes(), signerKey)
	trx = trx.WithSignature(sig)
	return trx
}

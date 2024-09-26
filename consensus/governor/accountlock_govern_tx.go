package governor

import (
	"math/big"

	"github.com/meterio/meter-pov/meter"
	"github.com/meterio/meter-pov/script"
	"github.com/meterio/meter-pov/script/accountlock"
	"github.com/meterio/meter-pov/tx"
)

func BuildAccountLockGoverningTx(chainTag byte, bestNum uint32, curEpoch uint32) *tx.Transaction {
	// 1. signer is nil
	// 1. transaction in kblock.
	builder := new(tx.Builder)
	builder.ChainTag(chainTag).
		BlockRef(tx.NewBlockRef(bestNum + 1)).
		Expiration(720).
		GasPriceCoef(0).
		Gas(meter.BaseTxGas * 10). //buffer for builder.Build().IntrinsicGas()
		DependsOn(nil).
		Nonce(12345678)

	body := &accountlock.AccountLockBody{
		Opcode:  accountlock.OP_GOVERNING,
		Version: curEpoch,
		Option:  uint32(0),
	}
	data, _ := script.EncodeScriptData(body)
	builder.Clause(
		tx.NewClause(&meter.AccountLockModuleAddr).
			WithValue(big.NewInt(0)).
			WithToken(meter.MTRG).
			WithData(data))

	return builder.Build()
}

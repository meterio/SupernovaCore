package common

import (
	"encoding/hex"
	"fmt"

	cmttypes "github.com/cometbft/cometbft/types"
)

func ValidatorName(v *cmttypes.Validator) string {
	addr := v.Address.Bytes()
	pubkey := v.PubKey.Bytes()
	return fmt.Sprintf("0x%s..%s:key:%s..%s", hex.EncodeToString(addr[:4]), hex.EncodeToString(addr[len(addr)-4:]), hex.EncodeToString(pubkey[:4]), hex.EncodeToString(pubkey[len(pubkey)-4:]))
}

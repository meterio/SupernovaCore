// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package genesis

import "github.com/meterio/meter-pov/types"

// NewMainnet create mainnet genesis.
func NewMainnet() *Genesis {
	launchTime := uint64(1593907199) // 2020-07-04T23:59:59+00:00

	vset := types.NewValidatorSet(make([]*types.Validator, 0))

	builder := new(Builder).
		Timestamp(launchTime).ValidatorSet(vset)

	///// initialize builtin contracts

	var extra [28]byte
	copy(extra[:], "In Math We Trust !!!")
	builder.ExtraData(extra)
	id, err := builder.ComputeID()
	if err != nil {
		panic(err)
	}
	return &Genesis{builder, id, "mainnet", vset}

}

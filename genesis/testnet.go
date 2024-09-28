// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package genesis

import (
	"github.com/meterio/meter-pov/meter"
)

// NewTestnet create genesis for testnet.
func NewTestnet() *Genesis {
	launchTime := uint64(1530014400) // 'Tue Jun 26 2018 20:00:00 GMT+0800 (CST)'

	builder := new(Builder).
		Timestamp(launchTime).
		GasLimit(meter.InitialGasLimit)

		// set initial params
		// use an external account as executor to manage testnet easily

	id, err := builder.ComputeID()
	if err != nil {
		panic(err)
	}
	return &Genesis{builder, id, "testnet"}
}

// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package genesis_test

import (
	"testing"

	"github.com/meterio/meter-pov/genesis"
	"github.com/meterio/meter-pov/lvldb"
	"github.com/stretchr/testify/assert"
)

func TestTestnetGenesis(t *testing.T) {
	kv, _ := lvldb.NewMem()
	gene := genesis.NewTestnet()

	b0, _, err := gene.Build()
	assert.Nil(t, err)

	assert.Nil(t, err)
}

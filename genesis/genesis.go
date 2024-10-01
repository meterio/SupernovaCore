// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package genesis

import (
	"encoding/hex"

	"github.com/meterio/meter-pov/block"
	"github.com/meterio/meter-pov/meter"
	"github.com/meterio/meter-pov/types"
)

const (
	GenesisNonce = uint64(1001)
)

// Genesis to build genesis block.
type Genesis struct {
	builder *Builder
	id      meter.Bytes32
	name    string
	vset    *types.ValidatorSet
}

// Build build the genesis block.
func (g *Genesis) Build() (*block.Block, error) {
	blk, err := g.builder.Build()
	if err != nil {
		return nil, err
	}
	if blk.ID() != g.id {
		panic("built genesis ID incorrect")
	}
	blk.QC = block.GenesisQC()
	return blk, nil
}

// ID returns genesis block ID.
func (g *Genesis) ID() meter.Bytes32 {
	return g.id
}

// Name returns network name.
func (g *Genesis) Name() string {
	return g.name
}

func (g *Genesis) ValidatorSet() *types.ValidatorSet {
	return g.vset
}

func mustDecodeHex(str string) []byte {
	data, err := hex.DecodeString(str)
	if err != nil {
		panic(err)
	}
	return data
}

var emptyRuntimeBytecode = mustDecodeHex("6060604052600256")

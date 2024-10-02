// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package genesis

import (
	"encoding/hex"

	"github.com/meterio/supernova/block"
	cmn "github.com/meterio/supernova/libs/common"
	"github.com/meterio/supernova/types"
)

const (
	GenesisNonce = uint64(1001)
)

// Genesis to build genesis block.
type Genesis struct {
	builder *Builder
	id      types.Bytes32
	vset    *types.ValidatorSet

	Name    string
	ChainId uint64
}

func newGenesis(gdoc *types.GenesisDoc) *Genesis {
	builder := &Builder{}
	builder.GenesisDoc(gdoc)
	id, err := builder.ComputeID()
	if err != nil {
		panic(err)
	}
	return &Genesis{builder, id, types.NewValidatorSet(gdoc.Validators), gdoc.Name, gdoc.ChainId}
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
func (g *Genesis) ID() types.Bytes32 {
	return g.id
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

func LoadGenesis(baseDir string) *Genesis {
	cmn.EnsureDir(baseDir, 0700)
	loader := types.NewGenesisDocLoader(baseDir)
	gdoc, err := loader.Load()
	if err != nil {
		panic("could not load genesis doc")
	}
	return newGenesis(gdoc)
}

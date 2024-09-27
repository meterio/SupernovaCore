// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package packer

import (
	"github.com/meterio/meter-pov/block"
	"github.com/meterio/meter-pov/chain"
	"github.com/meterio/meter-pov/runtime"
	"github.com/meterio/meter-pov/state"
	"github.com/meterio/meter-pov/types"
	"github.com/meterio/meter-pov/xenv"
	"github.com/pkg/errors"
)

// Packer to pack txs and build new blocks.
type Packer struct {
	chain          *chain.Chain
	blsMaster      *types.BlsMaster
	stateCreator   *state.Creator
	targetGasLimit uint64
}

// New create a new Packer instance.
func New(
	chain *chain.Chain,
	blsMaster *types.BlsMaster,
	stateCreator *state.Creator,

) *Packer {

	return &Packer{
		chain,
		blsMaster,
		stateCreator,
		0,
	}
}

// Mock create a packing flow upon given parent, but with a designated timestamp.
func (p *Packer) Mock(parent *block.Header, targetTime uint64, gasLimit uint64) (*Flow, error) {
	state, err := p.stateCreator.NewState(parent.StateRoot())
	if err != nil {
		return nil, errors.Wrap(err, "state")
	}

	rt := runtime.New(
		p.chain.NewSeeker(parent.ID()),
		state,
		&xenv.BlockContext{
			Signer:     p.blsMaster.GetAddress(),
			Number:     parent.Number() + 1,
			Time:       targetTime,
			GasLimit:   gasLimit,
			TotalScore: parent.TotalScore() + 1,
		})

	return newFlow(p, parent, rt), nil
}

func (p *Packer) GasLimit(parentGasLimit uint64) uint64 {
	if p.targetGasLimit != 0 {
		return block.GasLimit(p.targetGasLimit).Qualify(parentGasLimit)
	}
	return parentGasLimit
}

// SetTargetGasLimit set target gas limit, the Packer will adjust block gas limit close to
// it as it can.
func (p *Packer) SetTargetGasLimit(gl uint64) {
	p.targetGasLimit = gl
}

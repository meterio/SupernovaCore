// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package genesis

import (
	"github.com/meterio/meter-pov/block"
	"github.com/meterio/meter-pov/meter"
	"github.com/pkg/errors"
)

// Builder helper to build genesis block.
type Builder struct {
	timestamp uint64

	calls     []call
	extraData [28]byte
}

type call struct {
	caller meter.Address
}

// Timestamp set timestamp.
func (b *Builder) Timestamp(t uint64) *Builder {
	b.timestamp = t
	return b
}

// ExtraData set extra data, which will be put into last 28 bytes of genesis parent id.
func (b *Builder) ExtraData(data [28]byte) *Builder {
	b.extraData = data
	return b
}

// ComputeID compute genesis ID.
func (b *Builder) ComputeID() (meter.Bytes32, error) {

	blk, err := b.Build()
	if err != nil {
		return meter.Bytes32{}, err
	}
	return blk.ID(), nil
}

// Build build genesis block according to presets.
func (b *Builder) Build() (blk *block.Block, err error) {
	if err != nil {
		return nil, errors.Wrap(err, "commit state")
	}

	parentID := meter.Bytes32{0xff, 0xff, 0xff, 0xff} //so, genesis number is 0
	copy(parentID[4:], b.extraData[:])

	return new(block.Builder).
		ParentID(parentID).
		Timestamp(b.timestamp).
		Build(), nil
}

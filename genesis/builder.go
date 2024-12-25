// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package genesis

import (
	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/meterio/supernova/block"
	cmn "github.com/meterio/supernova/libs/common"
	"github.com/meterio/supernova/types"
	"github.com/pkg/errors"
)

// Builder helper to build genesis block.
type Builder struct {
	timestamp uint64

	extraData [28]byte
	vset      *cmttypes.ValidatorSet
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
func (b *Builder) ComputeID() (types.Bytes32, error) {

	blk, err := b.Build()
	if err != nil {
		return types.Bytes32{}, err
	}
	return blk.ID(), nil
}

func (b *Builder) GenesisDoc(gdoc *types.GenesisDoc) *Builder {
	vs := make([]*cmttypes.Validator, 0)

	for _, v := range gdoc.Validators {

		vs = append(vs, &cmttypes.Validator{
			Address:     v.Address,
			PubKey:      v.PubKey,
			VotingPower: v.Power,
		})
	}
	b.vset = cmttypes.NewValidatorSet(vs)
	b.timestamp = uint64(gdoc.GenesisTime.Unix())
	return b
}

func (b *Builder) ValidatorSet() *cmttypes.ValidatorSet {
	return b.vset
}

func (b *Builder) Build() (blk *block.Block, err error) {
	if err != nil {
		return nil, errors.Wrap(err, "commit state")
	}

	parentID := types.Bytes32{0xff, 0xff, 0xff, 0xff} //so, genesis number is 0
	copy(parentID[4:], b.extraData[:])

	return new(block.Builder).
		ParentID(parentID).
		Timestamp(b.timestamp).
		ValidatorsHash([]byte{0x00}).
		NextValidatorsHash(b.vset.Hash()).
		Nonce(GenesisNonce).
		QC(&block.QuorumCert{Round: 0, Epoch: 0, BlockID: types.Bytes32{}, AggSig: make([]byte, 0), BitArray: cmn.NewBitArray(1)}).
		Build(), nil
}

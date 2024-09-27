// Copyright (c) 2020 The Meter.io developers
// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying

// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package types

import (
	"encoding/hex"
	"fmt"

	"github.com/meterio/meter-pov/meter"
	"github.com/prysmaticlabs/prysm/v5/crypto/bls"
)

// Volatile state for each Validator
// NOTE: The Accum is not included in Validator.Hash();
// make sure to update that method if changes are made here
type Validator struct {
	Name        string
	Address     meter.Address
	BlsPubKey   bls.PublicKey
	VotingPower int64
	NetAddr     NetAddress
	SortKey     []byte
}

func NewValidator(name string, address meter.Address, blsPub bls.PublicKey, votingPower int64) *Validator {
	return &Validator{
		Name:        name,
		Address:     address,
		BlsPubKey:   blsPub,
		VotingPower: votingPower,
	}
}

// Creates a new copy of the validator so we can mutate accum.
// Panics if the validator is nil.
func (v *Validator) Copy() *Validator {
	vCopy := *v
	return &vCopy
}

func (v *Validator) String() string {
	if v == nil {
		return "nil-Validator"
	}
	name := v.Name
	if len(v.Name) > 26 {
		name = v.Name[:26]
	}
	return fmt.Sprintf("%-26v %-15v blspub: %v",
		name,
		v.NetAddr.IP.String(),
		hex.EncodeToString(v.BlsPubKey.Marshal()),
	)
}

func (v *Validator) NameAndIP() string {
	return fmt.Sprintf("%s(%s)", v.Name, v.NetAddr.IP)
}

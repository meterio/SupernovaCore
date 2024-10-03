package types

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/cometbft/cometbft/crypto/merkle"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/prysmaticlabs/prysm/v5/crypto/bls"
)

const (
	// MaxTotalVotingPower - the maximum allowed total voting power.
	// It needs to be sufficiently small to, in all cases:
	// 1. prevent clipping in incrementProposerPriority()
	// 2. let (diff+diffMax-1) not overflow in IncrementProposerPriority()
	// (Proof of 1 is tricky, left to the reader).
	// It could be higher, but this is sufficiently large for our purposes,
	// and leaves room for defensive purposes.
	MaxTotalVotingPower = int64(math.MaxInt64) / 8

	// PriorityWindowSizeFactor - is a constant that when multiplied with the
	// total voting power gives the maximum allowed distance between validator
	// priorities.
	PriorityWindowSizeFactor = 2
)

// ErrTotalVotingPowerOverflow is returned if the total voting power of the
// resulting validator set exceeds MaxTotalVotingPower.
var ErrTotalVotingPowerOverflow = fmt.Errorf("total voting power of resulting valset exceeds max %d",
	MaxTotalVotingPower)

// ErrProposerNotInVals is returned if the proposer is not in the validator set.
var ErrProposerNotInVals = errors.New("proposer not in validator set")

// ValidatorSet represent a set of *Validator at a given height.
//
// The validators can be fetched by address or index.
// The index is in order of .VotingPower, so the indices are fixed for all
// rounds of a given blockchain height - ie. the validators are sorted by their
// voting power (descending). Secondary index - .Address (ascending).
//
// On the other hand, the .ProposerPriority of each validator and the
// designated .GetProposer() of a set changes every round, upon calling
// .IncrementProposerPriority().
//
// NOTE: Not goroutine-safe.
// NOTE: All get/set to validators should copy the value for safety.
type ValidatorSet struct {
	// NOTE: persisted via reflect, must be exported.
	Validators []*Validator `json:"validators"`
}

// NewValidatorSet initializes a ValidatorSet by copying over the values from
// `valz`, a list of Validators. If valz is nil or empty, the new ValidatorSet
// will have an empty list of Validators.
//
// The addresses of validators in `valz` must be unique otherwise the function
// panics.
//
// Note the validator set size has an implied limit equal to that of the
// MaxVotesCount - commits by a validator set larger than this will fail
// validation.
func NewValidatorSet(valz []*Validator) *ValidatorSet {
	vals := &ValidatorSet{
		Validators: valz,
	}
	return vals
}

// IsNilOrEmpty returns true if validator set is nil or empty.
func (vals *ValidatorSet) IsNilOrEmpty() bool {
	return vals == nil || len(vals.Validators) == 0
}

// validatorListCopy makes a copy of the validator list.
func validatorListCopy(valsList []*Validator) []*Validator {
	if valsList == nil {
		return nil
	}
	valsCopy := make([]*Validator, len(valsList))
	for i, val := range valsList {
		valsCopy[i] = val.Copy()
	}
	return valsCopy
}

// Copy each validator into a new ValidatorSet.
func (vals *ValidatorSet) Copy() *ValidatorSet {
	return &ValidatorSet{
		Validators: validatorListCopy(vals.Validators),
	}

}

// HasAddress returns true if address given is in the validator set, false -
// otherwise.
func (vals *ValidatorSet) HasAddress(address common.Address) bool {
	for _, val := range vals.Validators {
		if bytes.Equal(val.Address.Bytes(), address.Bytes()) {
			return true
		}
	}
	return false
}

func (vals *ValidatorSet) GetByPubkey(pubkey bls.PublicKey) (val *Validator) {
	for _, v := range vals.Validators {
		if bytes.Equal(v.PubKey.Marshal(), pubkey.Marshal()) {
			return v
		}
	}
	return nil
}

// GetByIndex returns the validator's address and validator itself (copy) by
// index.
// It returns nil values if index is less than 0 or greater or equal to
// len(ValidatorSet.Validators).
func (vals *ValidatorSet) GetByIndex(index uint32) (val *Validator) {
	if index < 0 || int(index) >= len(vals.Validators) {
		return nil
	}
	val = vals.Validators[index]
	return val.Copy()
}

// Size returns the length of the validator set.
func (vals *ValidatorSet) Size() int {
	return len(vals.Validators)
}

// Hash returns the Merkle root hash build using validators (as leaves) in the
// set.
//
// See merkle.HashFromByteSlices.
func (vals *ValidatorSet) Hash() []byte {
	bzs := make([][]byte, len(vals.Validators))
	for i, val := range vals.Validators {
		bzs[i], _ = rlp.EncodeToBytes(val)
	}
	return merkle.HashFromByteSlices(bzs)
}

func (vals *ValidatorSet) SortWithNonce(nonce uint64) *ValidatorSet {
	buf := make([]byte, binary.MaxVarintLen64)
	binary.PutUvarint(buf, nonce)

	sortKeys := make([]*ValidatorSortKey, 0)
	for _, d := range vals.Validators {
		sortKeys = append(sortKeys, &ValidatorSortKey{
			PubKey:  d.PubKey,
			SortKey: crypto.Keccak256(append(d.PubKey.Marshal(), buf...)),
		})
	}

	sort.SliceStable(sortKeys, func(i, j int) bool {
		return (bytes.Compare(sortKeys[i].SortKey, sortKeys[j].SortKey) <= 0)
	})

	vs := make([]*Validator, 0)
	for _, key := range sortKeys {
		vs = append(vs, vals.GetByPubkey(key.PubKey))
	}

	newVals := NewValidatorSet(vs)
	return newVals
}

func (vals *ValidatorSet) UnmarshalJSON(b []byte) error {
	vaildatorDefs := make([]*ValidatorDef, 0)
	err := json.Unmarshal(b, &vaildatorDefs)
	if err != nil {
		return err
	}
	validators := make([]*Validator, 0)
	for _, vd := range vaildatorDefs {

		var addr common.Address
		if len(vd.Address) != 0 {
			addr = common.HexToAddress(strings.ReplaceAll(vd.Address, "0x", ""))
		}

		decoded, err := base64.StdEncoding.DecodeString(vd.PubKey)
		if err != nil {
			return err
		}
		v := NewValidator(addr, hex.EncodeToString(decoded), vd.IP, uint32(vd.Port)).WithName(vd.Name)
		validators = append(validators, v)
	}
	vals = NewValidatorSet(validators)
	return nil
}

func (vals ValidatorSet) MarhsalJSON() ([]byte, error) {
	validatorDefs := make([]ValidatorDef, 0)
	for _, v := range vals.Validators {
		validatorDefs = append(validatorDefs, ValidatorDef{Name: v.Name, Address: v.Address.String(), PubKey: hex.EncodeToString(v.PubKey.Marshal()), IP: v.IP.String(), Port: v.Port})
	}
	r, err := json.Marshal(validatorDefs)
	return r, err
}

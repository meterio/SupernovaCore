package common

import (
	"bytes"
	"encoding/binary"
	"sort"
	"strconv"
	"strings"

	cmtcrypto "github.com/cometbft/cometbft/crypto"
	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/ethereum/go-ethereum/crypto"
)

type ValidatorSetAdapter struct {
	Validators []*cmttypes.Validator `json:"validators"`
}

// Volatile state for each Validator
// NOTE: The Accum is not included in Validator.Hash();
// make sure to update that method if changes are made here
type ValidatorSortKey struct {
	PubKey  cmtcrypto.PubKey
	SortKey []byte
}

func NewValidatorSetAdapter(vset *cmttypes.ValidatorSet) *ValidatorSetAdapter {
	vals := &ValidatorSetAdapter{
		Validators: vset.Copy().Validators,
	}
	return vals
}

func (vals *ValidatorSetAdapter) String() string {
	s := make([]string, 0)
	for i, v := range vals.Validators {
		s = append(s, strconv.Itoa(i+1)+v.String())
	}
	return strings.Join(s, "\n")
}

func (vals *ValidatorSetAdapter) Upsert(newV *cmttypes.Validator) {
	for _, v := range vals.Validators {
		if bytes.Equal(v.PubKey.Bytes(), newV.PubKey.Bytes()) {
			v.Address = newV.Address
		}
	}
	vals.Validators = append(vals.Validators, newV)
}

func (vals *ValidatorSetAdapter) GetByPubkey(pubkeyBytes []byte) (val *cmttypes.Validator) {
	for _, v := range vals.Validators {
		if bytes.Equal(v.PubKey.Bytes(), pubkeyBytes) {
			return v
		}
	}
	return nil
}

func (vals *ValidatorSetAdapter) DeleteByPubkey(pubkeyBytes []byte) {
	for index, v := range vals.Validators {
		if bytes.Equal(v.PubKey.Bytes(), pubkeyBytes) {
			vals.Validators = append(vals.Validators[:index], vals.Validators[index+1:]...)
			return
		}
	}
}

func (vals *ValidatorSetAdapter) SortWithNonce(nonce uint64) *cmttypes.ValidatorSet {
	buf := make([]byte, binary.MaxVarintLen64)
	binary.PutUvarint(buf, nonce)

	sortKeys := make([]*ValidatorSortKey, 0)
	for _, d := range vals.Validators {
		sortKeys = append(sortKeys, &ValidatorSortKey{
			PubKey:  d.PubKey,
			SortKey: crypto.Keccak256(append(d.PubKey.Bytes(), buf...)),
		})
	}

	sort.SliceStable(sortKeys, func(i, j int) bool {
		return (bytes.Compare(sortKeys[i].SortKey, sortKeys[j].SortKey) <= 0)
	})

	sorted := make([]*cmttypes.Validator, 0)
	for _, key := range sortKeys {
		sorted = append(sorted, vals.GetByPubkey(key.PubKey.Bytes()))
	}

	sortedVSet := cmttypes.NewValidatorSet(sorted)
	return sortedVSet
}

func (vals *ValidatorSetAdapter) ToValidatorSet() *cmttypes.ValidatorSet {
	return cmttypes.NewValidatorSet(vals.Validators)
}

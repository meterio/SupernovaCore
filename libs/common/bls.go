package common

import (
	"errors"

	"github.com/prysmaticlabs/prysm/v5/crypto/bls/common"

	cmtcrypto "github.com/cometbft/cometbft/crypto"
	prysmbls "github.com/prysmaticlabs/prysm/v5/crypto/bls"
	blst "github.com/supranational/blst/bindings/go"
)

type (
	blstPublicKey          = blst.P1Affine
	blstSignature          = blst.P2Affine
	blstAggregateSignature = blst.P1Aggregate
	blstAggregatePublicKey = blst.P2Aggregate
)

var (
	// ErrDeserialization is returned when deserialization fails.
	ErrDeserialization = errors.New("bls12381: deserialization error")
	// ErrInfinitePubKey is returned when the public key is infinite. It is part
	// of a more comprehensive subgroup check on the key.
	ErrInfinitePubKey = errors.New("bls12381: pubkey is infinite")
)

// PublicKeyFromBytes creates a BLS public key from a se
func PublicKeyFromBytes(pubKey []byte) (common.PublicKey, error) {
	pk := new(blstPublicKey).Deserialize(pubKey)
	if pk == nil {
		return nil, ErrDeserialization
	}
	// Subgroup and infinity check
	if !pk.KeyValidate() {
		return nil, ErrInfinitePubKey
	}
	return prysmbls.PublicKeyFromBytes(pk.Compress())
}

func BlsPublicKeyFromCmtPubKey(cmtPubKey cmtcrypto.PubKey) (common.PublicKey, error) {
	pk := new(blstPublicKey).Deserialize(cmtPubKey.Bytes())
	if pk == nil {
		return nil, ErrDeserialization
	}
	// Subgroup and infinity check
	if !pk.KeyValidate() {
		return nil, ErrInfinitePubKey
	}
	return prysmbls.PublicKeyFromBytes(pk.Compress())
}

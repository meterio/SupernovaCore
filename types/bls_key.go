package types

import (
	"bytes"
	"crypto"

	"github.com/OffchainLabs/prysm/v6/crypto/bls"
	cmtcrypto "github.com/cometbft/cometbft/crypto"
	cmtjson "github.com/cometbft/cometbft/libs/json"
	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/ethereum/go-ethereum/common"
)

func init() {
	cmtjson.RegisterType((*BLSPubKey)(nil), "bls12-381.pubkey")
	cmtjson.RegisterType((*BLSPrivKey)(nil), "bls12-381.secret")
}

/*
crypto.PubKey

	type PubKey interface {
		Address() Address
		Bytes() []byte
		VerifySignature(msg []byte, sig []byte) bool
		Equals(other PubKey) bool
		Type() string
	}
*/
type BLSPubKey []byte

func (k BLSPubKey) Bytes() []byte {
	return k
}

func (k BLSPubKey) Address() cmttypes.Address {
	return common.BytesToAddress(k).Bytes()
}

func (k BLSPubKey) VerifySignature(msg []byte, sig []byte) bool {
	signature, err := bls.SignatureFromBytes(sig)
	if err != nil {
		return false
	}
	pubkey, err := bls.PublicKeyFromBytes(k)
	if err != nil {
		return false
	}
	return signature.Verify(pubkey, msg)
}

func (k BLSPubKey) Equals(other cmtcrypto.PubKey) bool {
	return bytes.Equal(k, other.Bytes())
}

func (k BLSPubKey) Type() string {
	return "bls12-381.pubkey"
}

var _ crypto.PublicKey = BLSPubKey{}

/*
crypto.PrivKey

	type PrivKey interface {
		Bytes() []byte
		Sign(msg []byte) ([]byte, error)
		PubKey() PubKey
		Equals(other PrivKey) bool
		Type() string
	}
*/

type BLSPrivKey []byte

func NewBlsPrivKey() BLSPrivKey {
	key, _ := bls.RandKey()
	return key.Marshal()
}

func (k BLSPrivKey) Bytes() []byte {
	return k
}

func (k BLSPrivKey) Sign(msg []byte) ([]byte, error) {
	secret, _ := bls.SecretKeyFromBytes(k)
	sig := secret.Sign(msg)
	return sig.Marshal(), nil
}

func (k BLSPrivKey) Equals(other cmtcrypto.PrivKey) bool {
	return bytes.Equal(k, other.Bytes())
}

func (k BLSPrivKey) PubKey() cmtcrypto.PubKey {
	secret, _ := bls.SecretKeyFromBytes(k)
	return BLSPubKey(secret.PublicKey().Marshal())
}

func (k BLSPrivKey) Type() string {
	return "bls12-381.secret"
}

var _ cmtcrypto.PrivKey = BLSPrivKey{}

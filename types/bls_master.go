// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package types

import (
	sha256 "crypto/sha256"
	"fmt"

	"github.com/meterio/meter-pov/meter"
	"github.com/prysmaticlabs/prysm/v5/crypto/bls"
)

type BlsMaster struct {
	PrivKey bls.SecretKey //my private key
	PubKey  bls.PublicKey //my public key

}

func NewBlsMasterWithRandKey() *BlsMaster {
	secretKey, err := bls.RandKey()
	if err != nil {
		return nil
	}
	return &BlsMaster{
		PrivKey: secretKey,
		PubKey:  secretKey.PublicKey(),
	}
}

func NewBlsMaster(privKey bls.SecretKey, pubKey bls.PublicKey) *BlsMaster {
	return &BlsMaster{
		PrivKey: privKey,
		PubKey:  pubKey,
	}
}

// BLS is implemented by C, memeory need to be freed.
// Signatures also need to be freed but Not here!!!
func (bm *BlsMaster) Destroy() bool {
	return true
}

func (bm *BlsMaster) GetPublicKey() bls.PublicKey {
	return bm.PubKey
}

func (bm *BlsMaster) GetAddress() meter.Address {
	return meter.BytesToAddress(bm.PubKey.Marshal())
}

// func (bm *BlsMaster) GetPrivateKey() *bls.PrivateKey {
// 	return &bm.PrivKey
// }

// sign the part of msg
func (bm *BlsMaster) SignMessage(msg []byte) (bls.Signature, [32]byte) {
	hash := sha256.Sum256(msg)
	sig := bm.PrivKey.Sign(hash[:])
	return sig, hash
}

func (bm *BlsMaster) SignHash(hash [32]byte) []byte {
	return bm.PrivKey.Sign(hash[:]).Marshal()
}

func (bm *BlsMaster) VerifySignature(signature, msgHash, blsPK []byte) (bool, error) {
	var fixedMsgHash [32]byte
	copy(fixedMsgHash[:], msgHash[32:])
	pubkey, err := bls.PublicKeyFromBytes(blsPK)
	if err != nil {
		fmt.Println("pubkey unmarshal failed")
		return false, nil
	}
	return bls.VerifySignature(signature, [32]byte(msgHash), pubkey)
}

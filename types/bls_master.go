// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package types

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"

	"github.com/OffchainLabs/prysm/v6/crypto/bls"
	cmtcrypto "github.com/cometbft/cometbft/crypto"
	"github.com/cometbft/cometbft/crypto/bls12381"
	"github.com/ethereum/go-ethereum/common"
)

type BlsMaster struct {
	PrivKey    bls.SecretKey //my private key
	PubKey     bls.PublicKey //my public key
	CmtPrivKey cmtcrypto.PrivKey
	CmtPubKey  cmtcrypto.PubKey
}

func NewBlsMasterWithRandKey() *BlsMaster {
	cmtPrivKey, err := bls12381.GenPrivKey()
	if err != nil {
		panic(err)
	}
	cmtPubKey := cmtPrivKey.PubKey()
	return NewBlsMasterWithCometKeys(cmtPrivKey, cmtPubKey)
}

func NewBlsMasterWithCometKeys(cmtPrivKey cmtcrypto.PrivKey, cmtPubKey cmtcrypto.PubKey) *BlsMaster {
	secretBytes := cmtPrivKey.Bytes()
	blsPrivKey, err := bls.SecretKeyFromBytes(secretBytes)
	if err != nil {
		panic(err)
	}
	blsPubKey := blsPrivKey.PublicKey()

	bm := &BlsMaster{
		PrivKey:    blsPrivKey,
		PubKey:     blsPubKey,
		CmtPrivKey: cmtPrivKey,
		CmtPubKey:  cmtPubKey,
	}
	validated := bm.ValidateKeyPair()
	if !validated {
		panic("invalid bls secret")
	}
	return bm
}

func (bm *BlsMaster) ValidateKeyPair() bool {
	h := md5.New()

	_, err := io.WriteString(h, "This is a message to be signed and verified by BLS!")
	if err != nil {
		return false
	}
	msg := h.Sum(nil)
	sig := bm.SignMessage(msg)

	return sig.Verify(bm.PubKey, msg)
}

// BLS is implemented by C, memeory need to be freed.
// Signatures also need to be freed but Not here!!!
func (bm *BlsMaster) Destroy() bool {
	return true
}

func (bm *BlsMaster) GetPublicKey() bls.PublicKey {
	return bm.PubKey
}

func (bm *BlsMaster) GetAddress() common.Address {
	return common.BytesToAddress(bm.PubKey.Marshal())
}

// func (bm *BlsMaster) GetPrivateKey() *bls.PrivateKey {
// 	return &bm.PrivKey
// }

// sign the part of msg
func (bm *BlsMaster) SignMessage(msg []byte) bls.Signature {
	sig := bm.PrivKey.Sign(msg)
	return sig
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

func (bm *BlsMaster) Print() {
	fmt.Println("Bls Secret (Hex): ", hex.EncodeToString(bm.PrivKey.Marshal()))
	fmt.Println("Bls Secret (B64): ", base64.StdEncoding.EncodeToString(bm.PrivKey.Marshal()))
	fmt.Println("Bls Pubkey (Hex): ", hex.EncodeToString(bm.PubKey.Marshal()))
	fmt.Println("Bls Pubkey (B64): ", base64.StdEncoding.EncodeToString(bm.PubKey.Marshal()))
}

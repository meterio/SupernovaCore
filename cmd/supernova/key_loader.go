// Copyright (c) 2020 The Meter.io developers
// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying

// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package main

import (
	"crypto/ecdsa"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	b64 "encoding/base64"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/meterio/meter-pov/types"
	"github.com/prysmaticlabs/prysm/v5/crypto/bls"
	cli "gopkg.in/urfave/cli.v1"
)

// fileExists checks if a file exists and is not a directory before we
// try using it to prevent further errors.
func fileExists(filename string) bool {
	info, err := os.Stat(filename)
	if os.IsNotExist(err) {
		return false
	}
	return !info.IsDir()
}

func verifyECDSA(privKey *ecdsa.PrivateKey, pubKey *ecdsa.PublicKey) bool {
	hash := []byte("testing")
	h := md5.New()

	_, err := io.WriteString(h, "This is a message to be signed and verified by ECDSA!")
	if err != nil {
		return false
	}
	signhash := h.Sum(nil)
	r, s, err := ecdsa.Sign(rand.Reader, privKey, signhash)
	if err != nil {
		fmt.Println("Error during sign: ", err)
		return false
	}
	return ecdsa.Verify(pubKey, hash, r, s)
}

func verifyBls(privKey bls.SecretKey, pubKey bls.PublicKey) bool {
	signMsg := string("This is a test")
	msgHash := sha256.Sum256([]byte(signMsg))
	sig := privKey.Sign(msgHash[:])
	valid := sig.Verify(pubKey, msgHash[:])
	return valid
}

type KeyLoader struct {
	masterPath   string
	publicPath   string
	masterBytes  []byte
	publicBytes  []byte
	ecdsaPrivKey *ecdsa.PrivateKey
	ecdsaPubKey  *ecdsa.PublicKey
	blsPrivKey   bls.SecretKey
	blsPubKey    bls.PublicKey

	updated bool
}

func NewKeyLoader(ctx *cli.Context) *KeyLoader {
	masterPath := masterKeyPath(ctx)
	publicPath := publicKeyPath(ctx)
	var masterBytes, publicBytes []byte
	if fileExists(masterPath) {
		masterBytes, _ = os.ReadFile(masterPath)
		masterBytes = []byte(strings.TrimSuffix(string(masterBytes), "\n"))
	}
	if fileExists(publicPath) {
		publicBytes, _ = os.ReadFile(publicPath)
		publicBytes = []byte(strings.TrimSuffix(string(publicBytes), "\n"))
	}
	return &KeyLoader{
		masterPath:  masterPath,
		publicPath:  publicPath,
		masterBytes: masterBytes,
		publicBytes: publicBytes,

		updated: false,
	}
}

func (k *KeyLoader) genECDSA() error {
	k.updated = true
	key, err := crypto.GenerateKey()
	if err != nil {
		fmt.Println("Error during ECDSA key pair generation: ", err)
		return err
	}
	k.ecdsaPrivKey = key
	k.ecdsaPubKey = &key.PublicKey
	return nil
}

func (k *KeyLoader) validateECDSA() error {
	split := strings.Split(string(k.masterBytes), ":::")
	if len(split) == 0 {
		return k.genECDSA()
	}
	privBytes, err := b64.StdEncoding.DecodeString(split[0])
	if err != nil {
		return k.genECDSA()
	}
	privKey, err := crypto.ToECDSA(privBytes)
	if err != nil {
		return k.genECDSA()
	}
	k.ecdsaPrivKey = privKey
	psplit := strings.Split(string(k.publicBytes), ":::")
	if len(split) == 0 {
		k.ecdsaPubKey = &k.ecdsaPrivKey.PublicKey
		return nil
	}
	pubBytes, err := b64.StdEncoding.DecodeString(psplit[0])
	if err != nil {
		k.ecdsaPubKey = &k.ecdsaPrivKey.PublicKey
		return nil
	}
	pubKey, err := crypto.UnmarshalPubkey(pubBytes)
	if err != nil || !verifyECDSA(privKey, pubKey) {
		k.ecdsaPubKey = &k.ecdsaPrivKey.PublicKey
	} else {
		k.ecdsaPubKey = pubKey
	}
	return nil
}

func (k *KeyLoader) genBls() error {
	k.updated = true
	secretKey, err := bls.RandKey()
	if err != nil {
		return err
	}
	k.blsPrivKey = secretKey
	k.blsPubKey = secretKey.PublicKey()
	return nil
}

func (k *KeyLoader) Load() (*ecdsa.PrivateKey, *ecdsa.PublicKey, *types.BlsCommon, error) {
	err := k.validateECDSA()
	if err != nil {
		fmt.Println("could not validate ecdsa keys, error:", err)
		panic("could not validate ecdsa keys")
	}

	blsCommon := types.NewBlsCommonFromParams(k.blsPubKey, k.blsPrivKey)
	return k.ecdsaPrivKey, k.ecdsaPubKey, blsCommon, nil
}

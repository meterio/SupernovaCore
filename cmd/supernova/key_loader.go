// Copyright (c) 2020 The Meter.io developers
// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying

// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package main

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/meterio/supernova/types"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/prysm/v5/crypto/bls/blst"
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

func verifyBLS(blsMaster *types.BlsMaster) bool {
	h := md5.New()

	_, err := io.WriteString(h, "This is a message to be signed and verified by BLS!")
	if err != nil {
		return false
	}
	msg := h.Sum(nil)
	sig := blsMaster.SignMessage(msg)
	fmt.Println("msg: ", hex.EncodeToString(msg))

	return sig.Verify(blsMaster.PubKey, msg)
}

type KeyLoader struct {
	masterPath string
	publicPath string
}

func NewKeyLoader(ctx *cli.Context) *KeyLoader {
	masterPath := masterKeyPath(ctx)
	publicPath := publicKeyPath(ctx)

	return &KeyLoader{
		masterPath: masterPath,
		publicPath: publicPath,
	}
}

func (k *KeyLoader) Load() (*types.BlsMaster, error) {
	masterBytes := make([]byte, 0)
	publicBytes := make([]byte, 0)
	if fileExists(k.masterPath) {
		masterBytes, _ = os.ReadFile(k.masterPath)
		masterBytes = []byte(strings.TrimSuffix(string(masterBytes), "\n"))
	} else {
		secretKey, err := blst.RandKey()
		if err != nil {
			return nil, err
		}
		err = os.WriteFile(k.masterPath, secretKey.Marshal(), 0600)
		if err != nil {
			return nil, err
		}
		err = os.WriteFile(k.publicPath, secretKey.PublicKey().Marshal(), 0600)
		if err != nil {
			return nil, err
		}
		masterBytes = secretKey.Marshal()
		publicBytes = secretKey.PublicKey().Marshal()
		return types.NewBlsMaster(secretKey, secretKey.PublicKey()), nil
	}
	if fileExists(k.publicPath) {
		publicBytes, _ = os.ReadFile(k.publicPath)
		publicBytes = []byte(strings.TrimSuffix(string(publicBytes), "\n"))
	}

	secret, err := blst.SecretKeyFromBytes(masterBytes)
	if err != nil {
		return nil, err
	}
	pubkey, err := blst.PublicKeyFromBytes(publicBytes)
	if err != nil {
		return nil, err
	}
	blsMaster := types.NewBlsMaster(secret, pubkey)
	verified := verifyBLS(blsMaster)
	if !verified {
		return nil, errors.New("secret and pubkey mismatch")
	}
	return blsMaster, nil
}

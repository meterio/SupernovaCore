// Copyright (c) 2020 The Meter.io developers
// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying

// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package main

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/meterio/supernova/types"
	"github.com/prysmaticlabs/prysm/v5/crypto/bls"
	"github.com/prysmaticlabs/prysm/v5/crypto/bls/blst"
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

type KeyLoader struct {
	baseDir  string
	keysPath string
}

type KeysContent struct {
	Secret string `json:"secret"`
	Pubkey string `json:"pubkey"`
}

func NewKeyLoader(baseDir string) *KeyLoader {
	keysPath := filepath.Join(baseDir, "keys.json")

	return &KeyLoader{
		baseDir:  baseDir,
		keysPath: keysPath,
	}
}

func (k *KeyLoader) Load() (*types.BlsMaster, error) {
	var secret bls.SecretKey
	var pubkey bls.PublicKey

	var keysContent KeysContent
	if fileExists(k.keysPath) {

	} else {
		secretKey, err := blst.RandKey()
		if err != nil {
			return nil, err
		}
		keysContent := KeysContent{
			Secret: base64.StdEncoding.EncodeToString(secretKey.Marshal()),
			Pubkey: base64.StdEncoding.EncodeToString(secretKey.PublicKey().Marshal()),
		}
		keysBytes, err := json.Marshal(keysContent)
		if err != nil {
			return nil, err
		}
		err = os.WriteFile(k.keysPath, keysBytes, 0600)
	}
	keysBytes, err := os.ReadFile(k.keysPath)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(keysBytes, &keysContent)
	if err != nil {
		return nil, err
	}
	secretBytes, err := base64.StdEncoding.DecodeString(keysContent.Secret)
	if err != nil {
		return nil, err
	}
	secret, err = bls.SecretKeyFromBytes(secretBytes)

	pubBytes, err := base64.StdEncoding.DecodeString(keysContent.Pubkey)
	if err != nil {
		return nil, err
	}
	pubkey, err = bls.PublicKeyFromBytes(pubBytes)
	return types.NewBlsMaster(secret, pubkey), nil
}

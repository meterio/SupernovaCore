// Copyright (c) 2020 The Meter.io developers
// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying

// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package types

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/ethereum/go-ethereum/common"
)

type GenesisDocLoader struct {
	baseDir  string
	gdocPath string
}

func NewGenesisDocLoader(baseDir string) *GenesisDocLoader {
	gdocPath := filepath.Join(baseDir, "genesis.json")

	return &GenesisDocLoader{
		baseDir:  baseDir,
		gdocPath: gdocPath,
	}
}

func (k *GenesisDocLoader) Load() (*GenesisDoc, error) {
	var gdoc GenesisDoc
	if !common.FileExist(k.gdocPath) {
		panic("genesis doc not exist")
	}
	gbytes, err := os.ReadFile(k.gdocPath)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(gbytes, &gdoc)
	if err != nil {
		return nil, err
	}
	return &gdoc, nil
}

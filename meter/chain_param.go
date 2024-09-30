// Copyright (c) 2020 The Meter.io developers
// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying

// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package meter

import (
	"fmt"
	"log/slog"
)

// chainID
const (
	MainnetChainID = 82 // 0x52 for mainnet
	TestnetChainID = 83 // 0x53 for testnet
)

// Edision: The initial Basic Release. Features include
const (
	EdisonMainnetStartNum             = 0
	EdisonTestnetStartNum             = 0
	EdisonSysContract_MainnetStartNum = 4900000 //around 11/18/2020
	EdisonSysContract_TestnetStartNum = 100000
)

var (
	// BlocktChainConfig is the chain parameters to run a node on the main network.
	BlockChainConfig = &ChainConfig{
		ChainFlag:   "",
		Initialized: false,
	}
)

type ChainConfig struct {
	ChainFlag   string
	Initialized bool
}

func (c *ChainConfig) ToString() string {
	return fmt.Sprintf("ChainFlag: %v, Initialized: %v",
		c.ChainFlag, c.Initialized)
}

func (c *ChainConfig) IsInitialized() bool {
	return c.Initialized
}

// chain flag right now ONLY 3: "main"/"test"/"warringstakes"
func (c *ChainConfig) IsMainnet() bool {
	if !c.IsInitialized() {
		slog.Warn("Chain is not initialized", "chain-flag", c.ChainFlag)
		return false
	}

	switch c.ChainFlag {
	case "main":
		return true
	case "staging":
		return true
	default:
		// slog.Error("Unknown chain", "chain", c.ChainFlag)
		return false
	}
}

func (c *ChainConfig) IsStaging() bool {
	if !c.IsInitialized() {
		slog.Warn("Chain is not initialized", "chain-flag", c.ChainFlag)
		return false
	}

	switch c.ChainFlag {
	case "staging":
		return true
	default:
		return false
	}
}

// TBD: There would be more rules when 2nd fork is there.

func InitBlockChainConfig(chainFlag string) {
	BlockChainConfig.ChainFlag = chainFlag
	BlockChainConfig.Initialized = true
}

func IsMainNet() bool {
	return BlockChainConfig.IsMainnet()
}

func IsStaging() bool {
	return BlockChainConfig.IsStaging()
}

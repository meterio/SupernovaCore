// Copyright (c) 2020 The Meter.io developers
// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying

// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package types

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/meterio/meter-pov/meter"
	"github.com/meterio/meter-pov/preset"
	"github.com/prysmaticlabs/prysm/v5/crypto/bls"
)

type Distributor struct {
	Address meter.Address
	Autobid uint8  // autobid percentile
	Shares  uint64 // unit is shannon, 1E09
}

// make sure to update that method if changes are made here
type Delegate struct {
	Name        []byte         `json:"name"`
	Address     meter.Address  `json:"address"`
	BlsPubKey   bls.PublicKey  `json:"bsl_pubkey"`
	VotingPower int64          `json:"voting_power"`
	NetAddr     NetAddress     `json:"network_addr"`
	Commission  uint64         `json:"commission"`
	DistList    []*Distributor `json:"distibutor_list"`

	comboPubKeyStr string
}

func NewDelegate(name []byte, addr meter.Address, blsPub bls.PublicKey, comboPubKeyStr string, votingPower int64, commission uint64, netAddr NetAddress) *Delegate {
	return &Delegate{
		Name:           name,
		Address:        addr,
		BlsPubKey:      blsPub,
		comboPubKeyStr: comboPubKeyStr,
		VotingPower:    votingPower,
		Commission:     commission,
		NetAddr:        netAddr,
	}
}

// Creates a new copy of the Delegate so we can mutate accum.
// Panics if the Delegate is nil.
func (v *Delegate) Copy() *Delegate {
	vCopy := *v
	return &vCopy
}

func (v *Delegate) String() string {
	if v == nil {
		return "Delegate{nil}"
	}

	return fmt.Sprintf("%v ( Addr:%v VP:%v Commission:%v%% #Dists:%v, BlsPubKey:%v )",
		string(v.Name), v.Address, v.VotingPower, v.Commission/1e7, len(v.DistList), hex.EncodeToString(v.BlsPubKey.Marshal()))
}

// =================================
// commission rate 1% presents 1e07, unit is shannon (1e09)
const (
	COMMISSION_RATE_MAX     = uint64(100 * 1e07) // 100%
	COMMISSION_RATE_MIN     = uint64(1 * 1e07)   // 1%
	COMMISSION_RATE_DEFAULT = uint64(10 * 1e07)  // 10%
)

func LoadDelegatesFile(network string, dataDir string, blsMaster *BlsMaster) []*Delegate {
	delegates1 := make([]*DelegateDef, 0)

	// load delegates from presets
	var content []byte
	if network == "warringstakes" {
		content = preset.MustAsset("shoal/delegates.json")
	} else if network == "main" {
		content = preset.MustAsset("mainnet/delegates.json")
	} else {
		// load delegates from file system
		filePath := path.Join(dataDir, "delegates.json")
		file, err := os.ReadFile(filePath)
		content = file
		if err != nil {
			fmt.Println("Unable load delegate file at", filePath, "error", err)
			os.Exit(1)
			return nil
		}
	}
	err := json.Unmarshal(content, &delegates1)
	if err != nil {
		fmt.Println("Unable unmarshal delegate file, please check your config", "error", err)
		os.Exit(1)
		return nil
	}

	delegates := make([]*Delegate, 0)
	for _, d := range delegates1 {
		// first part is ecdsa public, 2nd part is bls public key
		pubkeyBytes, err := hex.DecodeString(strings.Replace(d.PubKey, "0x", "", 1))
		blsPubKey, err := bls.PublicKeyFromBytes(pubkeyBytes)

		var addr meter.Address
		if len(d.Address) != 0 {
			addr, err = meter.ParseAddress(d.Address)
			if err != nil {
				fmt.Println("can't read address of delegates:", d.String(), "error", err)
				os.Exit(1)
				return nil
			}
		}

		dd := NewDelegate([]byte(d.Name), addr, blsPubKey, d.PubKey, d.VotingPower, COMMISSION_RATE_DEFAULT, d.NetAddr)
		delegates = append(delegates, dd)
	}
	return delegates
}

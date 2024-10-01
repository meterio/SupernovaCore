// Copyright (c) 2020 The Meter.io developers
// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying

// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package types

import (
	sha256 "crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/netip"
	"os"
	"path"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/prysmaticlabs/prysm/v5/crypto/bls"
)

// Volatile state for each Validator
// NOTE: The Accum is not included in Validator.Hash();
// make sure to update that method if changes are made here
type ValidatorSortKey struct {
	PubKey  bls.PublicKey
	SortKey []byte
}

type Validator struct {
	Name        string
	Address     common.Address
	PubKey      bls.PublicKey
	IP          netip.Addr
	Port        uint32
	VotingPower uint64
}

func NewValidator(address common.Address, hexPubkey string, ip string, port uint32) *Validator {
	decoded, err := hex.DecodeString(strings.ReplaceAll(hexPubkey, "0x", ""))
	if err != nil {
		panic("could not decode pubkey")
	}
	pubkey, err := bls.PublicKeyFromBytes(decoded)
	if err != nil {
		panic("pubkey not valid")
	}
	parsedIP := netip.MustParseAddr(ip)

	return &Validator{
		Address: address,
		PubKey:  pubkey,
		IP:      parsedIP,
		Port:    port,
	}
}

func (v *Validator) WithVotingPower(vp uint64) *Validator {
	v.VotingPower = vp
	return v
}

func (v *Validator) WithName(name string) *Validator {
	v.Name = name
	return v
}

// Creates a new copy of the validator so we can mutate accum.
// Panics if the validator is nil.
func (v *Validator) Copy() *Validator {
	vCopy := *v
	return &vCopy
}

func (v *Validator) String() string {
	if v == nil {
		return "nil-Validator"
	}
	name := v.Name
	if len(v.Name) > 26 {
		name = v.Name[:26]
	}
	return fmt.Sprintf("%-26v %-15v blspub: %v",
		name,
		v.IP.String(),
		hex.EncodeToString(v.PubKey.Marshal()),
	)
}

func (v *Validator) NameAndIP() string {
	return fmt.Sprintf("%s(%s)", v.Name, v.IP)
}

// EncodeRLP implements rlp.Encoder.
func (v *Validator) EncodeRLP(w io.Writer) error {
	if v == nil {
		w.Write([]byte{})
		return nil
	}
	return rlp.Encode(w, []interface{}{
		v.Name, v.Address, v.PubKey.Marshal(), v.IP.String(), v.Port, v.VotingPower,
	})
}

func (v *Validator) Hash() []byte {
	b, _ := rlp.EncodeToBytes(v)
	hash := sha256.Sum256(b)
	return hash[:]
}

// DecodeRLP implements rlp.Decoder.
func (v *Validator) DecodeRLP(s *rlp.Stream) error {
	payload := struct {
		Name        string
		Address     common.Address
		Pubkey      []byte
		IP          string
		Port        uint64
		VotingPower uint64
	}{}

	if err := s.Decode(&payload); err != nil {
		return err
	}

	pubkey, _ := bls.PublicKeyFromBytes(payload.Pubkey)

	*v = Validator{
		Name:        payload.Name,
		Address:     payload.Address,
		PubKey:      pubkey,
		IP:          netip.MustParseAddr(payload.IP),
		Port:        v.Port,
		VotingPower: v.VotingPower,
	}
	return nil
}

func LoadValidatorsFromFile(network string, dataDir string) *ValidatorSet {
	vaildatorDefs := make([]*ValidatorDef, 0)

	// load validators from file system
	filePath := path.Join(dataDir, "validators.json")
	content, err := os.ReadFile(filePath)
	if err != nil {
		fmt.Println("Unable load validator file at", filePath, "error", err)
		os.Exit(1)
		return nil
	}

	err = json.Unmarshal(content, &vaildatorDefs)
	if err != nil {
		fmt.Println("Unable unmarshal validator file, please check your config", "error", err)
		os.Exit(1)
		return nil
	}

	validators := make([]*Validator, 0)
	for _, vd := range vaildatorDefs {

		var addr common.Address
		if len(vd.Address) != 0 {
			addr = common.HexToAddress(strings.ReplaceAll(vd.Address, "0x", ""))
		}

		v := NewValidator(addr, vd.PubKey, vd.IP, uint32(vd.Port)).WithName(vd.Name)
		validators = append(validators, v)
	}
	return NewValidatorSet(validators)
}

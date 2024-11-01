package consensus

import (
	"encoding/hex"
	"errors"
	"log/slog"
	"net/netip"
	"strconv"

	abcitypes "github.com/cometbft/cometbft/abci/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/meterio/supernova/chain"
	"github.com/meterio/supernova/types"
	"github.com/prysmaticlabs/prysm/v5/crypto/bls"
)

type ValidatorSetRegistry struct {
	CurrentVSet map[uint32]*types.ValidatorSet
	NextVSet    map[uint32]*types.ValidatorSet
	Chain       *chain.Chain
	logger      *slog.Logger
}

func NewValidatorSetRegistry(c *chain.Chain) *ValidatorSetRegistry {
	vset := c.GetBestValidatorSet()
	nxtVSet := c.GetBestNextValidatorSet()
	best := c.BestBlock()
	bestNum := best.Number()

	registry := &ValidatorSetRegistry{
		CurrentVSet: make(map[uint32]*types.ValidatorSet),
		NextVSet:    make(map[uint32]*types.ValidatorSet),
		Chain:       c,
		logger:      slog.With("pkg", "vreg"),
	}

	if best.IsKBlock() {
		registry.registerNewValidatorSet(bestNum, vset, nxtVSet)
	}
	return registry
}

func (vr *ValidatorSetRegistry) registerNewValidatorSet(curNum uint32, vset *types.ValidatorSet, nxtVSet *types.ValidatorSet) error {
	bestNum := vr.Chain.BestBlock().Number()
	enableNum := curNum + types.NBlockDelayToEnableValidatorSet - 1
	if curNum == 0 {
		enableNum = 0
	}
	if enableNum <= bestNum {
		return errors.New("could not enable validator set before best block")
	}
	if vset != nil {
		vr.CurrentVSet[enableNum] = vset
	}
	if nxtVSet == nil {
		panic("next validator set could not be empty")
	}
	vr.NextVSet[enableNum] = nxtVSet
	vr.Chain.SaveValidatorSet(nxtVSet)
	vr.logger.Info("registered next vset", "hash", nxtVSet.Hex(), "cur", curNum, "num", enableNum, "size", nxtVSet.Size())

	vr.Prune()
	return nil
}

func (vr *ValidatorSetRegistry) Get(num uint32) *types.ValidatorSet {
	if vset, exist := vr.CurrentVSet[num]; exist {
		return vset
	}
	return nil
}

func (vr *ValidatorSetRegistry) GetNext(num uint32) *types.ValidatorSet {
	if vset, exist := vr.NextVSet[num]; exist {
		return vset
	}
	return nil
}

type validatorExtra struct {
	Name    string
	Address common.Address
	Pubkey  []byte
	IP      string
	Port    uint64
}

func (vr *ValidatorSetRegistry) Update(num uint32, vset *types.ValidatorSet, updates abcitypes.ValidatorUpdates, events []abcitypes.Event) (nxtVSet *types.ValidatorSet, addedValidators []*types.Validator, err error) {
	if updates.Len() <= 0 {
		return
	}
	nxtVSet = vset.Copy()

	veMap := make(map[string]validatorExtra)
	for _, ev := range events {
		if ev.Type == "ValidatorExtra" {
			ve := validatorExtra{}
			for _, attr := range ev.Attributes {
				switch attr.Key {
				case "address":
					ve.Address = common.Address{}
				case "name":
					ve.Name = attr.Value
				case "pubkey":
					ve.Pubkey, _ = hex.DecodeString(attr.Value)
				case "ip":
					ve.IP = attr.Value
				case "port":
					ve.Port, _ = strconv.ParseUint(attr.Value, 10, 32)
				}
			}
			veMap[hex.EncodeToString(ve.Pubkey)] = ve
		}
	}
	for _, update := range updates {
		pubkey, err := bls.PublicKeyFromBytes(update.PubKeyBytes)
		pubkeyHex := hex.EncodeToString(update.PubKeyBytes)
		if err != nil {
			panic(err)
		}
		if update.Power == 0 {
			nxtVSet.DeleteByPubkey(pubkey)
		} else {
			v := nxtVSet.GetByPubkey(pubkey)

			added := false
			if v == nil {
				added = true
				v = &types.Validator{PubKey: pubkey, VotingPower: uint64(update.Power)}
			}

			if ve, existed := veMap[pubkeyHex]; existed {
				v.IP, err = netip.ParseAddr(ve.IP)
				if err != nil {
					continue
				}
				if ve.Port != 0 {
					v.Port = uint32(ve.Port)
				}
				if ve.Name != "" {
					v.Name = ve.Name
				}
				v.Address = common.Address{}
			}

			v.VotingPower = uint64(update.Power)
			nxtVSet.Upsert(v)

			if added {
				addedValidators = append(addedValidators, v)
			}
		}
	}
	err = vr.registerNewValidatorSet(num, vset, nxtVSet)
	return
}

func (vr *ValidatorSetRegistry) Prune() {
	bestNum := vr.Chain.BestBlock().Number()
	for num, _ := range vr.CurrentVSet {
		if num < bestNum {
			delete(vr.CurrentVSet, num)
		}
	}
}

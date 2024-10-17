package consensus

import (
	"errors"

	abcitypes "github.com/cometbft/cometbft/abci/types"
	"github.com/meterio/supernova/chain"
	"github.com/meterio/supernova/types"
	"github.com/prysmaticlabs/prysm/v5/crypto/bls"
)

type ValidatorSetRegistry struct {
	CurrentVSet map[uint32]*types.ValidatorSet
	NextVSet    map[uint32]*types.ValidatorSet
	Chain       *chain.Chain
}

func NewValidatorSetRegistry(c *chain.Chain) *ValidatorSetRegistry {
	vset := c.GetBestValidatorSet()
	nxtVSet := c.GetBestNextValidatorSet()
	bestNum := c.BestBlock().Number()

	registry := &ValidatorSetRegistry{
		CurrentVSet: make(map[uint32]*types.ValidatorSet),
		NextVSet:    make(map[uint32]*types.ValidatorSet),
		Chain:       c,
	}

	registry.registerReelect(bestNum, vset, nxtVSet)
	return registry
}

func (vr *ValidatorSetRegistry) registerReelect(curNum uint32, vset *types.ValidatorSet, nxtVSet *types.ValidatorSet) error {
	bestNum := vr.Chain.BestBlock().Number()
	enableNum := curNum + types.NBlockDelayToEnableValidatorSet - 1
	if enableNum <= bestNum {
		return errors.New("could not enable validator set before best block")
	}
	if vset != nil {
		vr.CurrentVSet[enableNum-1] = vset
	}
	if nxtVSet == nil {
		panic("next validator set could not be empty")
	}
	vr.NextVSet[enableNum-1] = nxtVSet
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
	if vset, exist := vr.CurrentVSet[num]; exist {
		return vset
	}
	return nil
}

func (vr *ValidatorSetRegistry) Update(vset *types.ValidatorSet, updates abcitypes.ValidatorUpdates) error {
	nxtVSet := vset.Copy()
	bestNum := vr.Chain.BestBlock().Number()
	for _, update := range updates {
		pubkey, err := bls.PublicKeyFromBytes(update.PubKeyBytes)
		if err != nil {
			panic(err)
		}
		if update.Power == 0 {
			nxtVSet.DeleteByPubkey(pubkey)
		} else {
			v := nxtVSet.GetByPubkey(pubkey)
			v.VotingPower = uint64(update.Power)
		}
	}
	return vr.registerReelect(bestNum+types.NBlockDelayToEnableValidatorSet, vset, nxtVSet)
}

func (vr *ValidatorSetRegistry) Prune() {
	bestNum := vr.Chain.BestBlock().Number()
	for num, _ := range vr.CurrentVSet {
		if num < bestNum {
			delete(vr.CurrentVSet, num)
		}
	}
}

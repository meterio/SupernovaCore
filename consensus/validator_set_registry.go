package consensus

import (
	"errors"

	abcitypes "github.com/cometbft/cometbft/abci/types"
	"github.com/meterio/supernova/chain"
	"github.com/meterio/supernova/types"
	"github.com/prysmaticlabs/prysm/v5/crypto/bls"
)

type ValidatorSetRegistry struct {
	ValidatorMap map[uint32]*types.ValidatorSet
	Chain        *chain.Chain
	current      *types.ValidatorSet
}

func NewValidatorSetRegistry(c *chain.Chain) *ValidatorSetRegistry {
	vset := c.GetBestValidatorSet()
	blk := c.BestBlock()
	vmap := make(map[uint32]*types.ValidatorSet)
	vmap[blk.Number()] = vset
	return &ValidatorSetRegistry{
		ValidatorMap: vmap,
		Chain:        c,
	}
}

func (vr *ValidatorSetRegistry) register(enableNum uint32, vset *types.ValidatorSet) error {
	bestNum := vr.Chain.BestBlock().Number()
	if enableNum <= bestNum {
		return errors.New("could not enable validator set before best block")
	}
	if vset == nil {
		return errors.New("empty validator set")
	}
	vr.ValidatorMap[enableNum] = vset
	vr.Prune()
	return nil
}

func (vr *ValidatorSetRegistry) Get(num uint32) *types.ValidatorSet {
	if vset, exist := vr.ValidatorMap[num]; exist {
		return vset
	}
	return nil
}

func (vr *ValidatorSetRegistry) Update(vset *types.ValidatorSet, updates abcitypes.ValidatorUpdates) error {
	newVSet := vset.Copy()
	bestNum := vr.Chain.BestBlock().Number()
	for _, update := range updates {
		pubkey, err := bls.PublicKeyFromBytes(update.PubKeyBytes)
		if err != nil {
			panic(err)
		}
		if update.Power == 0 {
			newVSet.DeleteByPubkey(pubkey)
		} else {
			v := newVSet.GetByPubkey(pubkey)
			v.VotingPower = uint64(update.Power)
		}
	}
	return vr.register(bestNum+types.NBlockDelayToEnableValidatorSet, newVSet)
}

func (vr *ValidatorSetRegistry) Prune() {
	bestNum := vr.Chain.BestBlock().Number()
	for num, _ := range vr.ValidatorMap {
		if num < bestNum {
			delete(vr.ValidatorMap, num)
		}
	}
}

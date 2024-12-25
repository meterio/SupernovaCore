package consensus

import (
	"errors"
	"log/slog"

	abcitypes "github.com/cometbft/cometbft/abci/types"
	"github.com/cometbft/cometbft/crypto/bls12381"
	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/meterio/supernova/chain"
	cmn "github.com/meterio/supernova/libs/common"
	"github.com/meterio/supernova/types"
)

type ValidatorSetRegistry struct {
	CurrentVSet map[uint32]*cmttypes.ValidatorSet
	NextVSet    map[uint32]*cmttypes.ValidatorSet
	Chain       *chain.Chain
	logger      *slog.Logger
}

func NewValidatorSetRegistry(c *chain.Chain) *ValidatorSetRegistry {
	vset := c.GetBestValidatorSet()
	nxtVSet := c.GetBestNextValidatorSet()
	best := c.BestBlock()
	bestNum := best.Number()

	registry := &ValidatorSetRegistry{
		CurrentVSet: make(map[uint32]*cmttypes.ValidatorSet),
		NextVSet:    make(map[uint32]*cmttypes.ValidatorSet),
		Chain:       c,
		logger:      slog.With("pkg", "vreg"),
	}

	if best.IsKBlock() {
		registry.registerNewValidatorSet(bestNum, vset, nxtVSet)
	}
	return registry
}

func (vr *ValidatorSetRegistry) registerNewValidatorSet(curNum uint32, vset *cmttypes.ValidatorSet, nxtVSet *cmttypes.ValidatorSet) error {
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
	vr.logger.Info("registered next vset", "hash", nxtVSet.Hash(), "cur", curNum, "num", enableNum, "size", nxtVSet.Size())

	vr.Prune()
	return nil
}

func (vr *ValidatorSetRegistry) Get(num uint32) *cmttypes.ValidatorSet {
	if vset, exist := vr.CurrentVSet[num]; exist {
		return vset
	}
	return nil
}

func (vr *ValidatorSetRegistry) GetNext(num uint32) *cmttypes.ValidatorSet {
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

func (vr *ValidatorSetRegistry) Update(num uint32, vset *cmttypes.ValidatorSet, updates abcitypes.ValidatorUpdates, events []abcitypes.Event) (nxtVSet *cmttypes.ValidatorSet, addedValidators []*cmttypes.Validator, err error) {
	if updates.Len() <= 0 {
		return
	}
	nxtVSetAdapter := cmn.NewValidatorSetAdapter(vset)

	for _, update := range updates {
		pubkey, err := bls12381.NewPublicKeyFromBytes(update.PubKeyBytes)
		if err != nil {
			panic(err)
		}
		if update.Power == 0 {
			nxtVSetAdapter.DeleteByPubkey(update.PubKeyBytes)
		} else {
			v := nxtVSetAdapter.GetByPubkey(update.PubKeyBytes)

			added := false
			if v == nil {
				added = true
				v = &cmttypes.Validator{PubKey: pubkey, VotingPower: update.Power}
			}

			v.VotingPower = update.Power
			nxtVSetAdapter.Upsert(v)

			if added {
				addedValidators = append(addedValidators, v)
			}
		}
	}
	err = vr.registerNewValidatorSet(num, vset, nxtVSetAdapter.ToValidatorSet())
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

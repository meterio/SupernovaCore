package consensus

import (
	"math/big"
	"net"

	"github.com/meterio/meter-pov/block"
	"github.com/meterio/meter-pov/meter"
	"github.com/meterio/meter-pov/types"
	"github.com/prysmaticlabs/prysm/v5/crypto/bls"
)

// build block committee info part
func (r *Reactor) MakeBlockCommitteeInfo() []block.CommitteeInfo {
	cis := []block.CommitteeInfo{}

	for index, cm := range r.committee {
		ci := block.NewCommitteeInfo(cm.Name, cm.BlsPubKey, cm.NetAddr,
			uint32(index))
		cis = append(cis, *ci)
	}
	return (cis)
}

func convertDistList(dist []*meter.Distributor) []*types.Distributor {
	list := []*types.Distributor{}
	for _, d := range dist {
		l := &types.Distributor{
			Address: d.Address,
			Autobid: d.Autobid,
			Shares:  d.Shares,
		}
		list = append(list, l)
	}
	return list
}

func (r *Reactor) getDelegatesFromStaking(revision *block.Block) ([]*types.Delegate, error) {
	delegateList := []*types.Delegate{}

	state, err := r.stateCreator.NewState(revision.StateRoot())
	if err != nil {
		return delegateList, err
	}

	list := state.GetDelegateList()
	r.logger.Info("Loaded delegateList from staking", "len", len(list.Delegates))
	for _, s := range list.Delegates {
		blsPub, e := bls.PublicKeyFromBytes(s.PubKey)
		if e != nil {
			// FIXME: maybe a better way of just skipping
			continue
		}

		d := types.NewDelegate([]byte(s.Name), s.Address, blsPub, string(s.PubKey), new(big.Int).Div(s.VotingPower, big.NewInt(1e12)).Int64(), s.Commission, types.NetAddress{
			IP:   net.ParseIP(string(s.IPAddr)),
			Port: s.Port})
		d.DistList = convertDistList(s.DistList)
		delegateList = append(delegateList, d)
	}
	return delegateList, nil
}

package consensus

import (
	"encoding/hex"

	"github.com/ethereum/go-ethereum/common"
)

// ------------------------------------
// USED FOR PROBE ONLY
// ------------------------------------

func (r *Reactor) PacemakerProbe() *PMProbeResult {
	return r.pacemaker.Probe()
}

func (r *Reactor) InCommittee() bool {
	return r.inCommittee
}

// ------------------------------------
// USED FOR API ONLY
// ------------------------------------
type ApiCommitteeMember struct {
	Name        string
	Address     common.Address
	PubKey      string
	VotingPower uint64
	IP          string
	Port        uint32
	Index       int
	InCommittee bool
}

func (r *Reactor) GetLatestCommitteeList() ([]*ApiCommitteeMember, error) {
	var committeeMembers []*ApiCommitteeMember
	inCommittee := make([]bool, r.committee.Size())
	for i := range inCommittee {
		inCommittee[i] = false
	}

	for index, v := range r.committee.Validators {
		apiCm := &ApiCommitteeMember{
			Name:        v.Name,
			Address:     v.Address,
			PubKey:      hex.EncodeToString(v.PubKey.Marshal()),
			Index:       index,
			VotingPower: v.VotingPower,
			IP:          v.IP.String(),
			Port:        v.Port,
			InCommittee: true,
		}
		committeeMembers = append(committeeMembers, apiCm)
		inCommittee[index] = true
	}
	for i, val := range inCommittee {
		if val == false {
			v := r.committee.GetByIndex(uint32(i))
			apiCm := &ApiCommitteeMember{
				Name:        v.Name,
				Address:     v.Address,
				PubKey:      hex.EncodeToString(v.PubKey.Marshal()),
				Index:       i,
				InCommittee: false,
			}
			committeeMembers = append(committeeMembers, apiCm)
		}
	}
	return committeeMembers, nil
}

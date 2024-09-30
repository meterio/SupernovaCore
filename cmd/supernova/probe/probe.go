// Copyright (c) 2020 The Meter.io developers
// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying

// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package probe

import (
	"encoding/hex"
	"net/http"

	"github.com/meterio/meter-pov/api/utils"
	"github.com/meterio/meter-pov/chain"
	"github.com/meterio/meter-pov/consensus"
	"github.com/prysmaticlabs/prysm/v5/crypto/bls"
)

type Probe struct {
	Cons      *consensus.Reactor
	BlsPubKey bls.PublicKey
	Chain     *chain.Chain
	Version   string
	Network   Network
}

func (p *Probe) HandleProbe(w http.ResponseWriter, r *http.Request) {
	name := ""
	pubkeyMatch := false

	bestBlock, _ := convertBlock(p.Chain.BestBlock())
	bestQC, _ := convertQC(p.Chain.BestQC())
	pmProbe := p.Cons.PacemakerProbe()
	pacemaker, _ := convertPacemakerProbe(pmProbe)
	chainProbe := &ChainProbe{
		BestBlock: bestBlock,
		BestQC:    bestQC,
	}
	result := ProbeResult{
		Name:            name,
		PubKey:          hex.EncodeToString(p.BlsPubKey.Marshal()),
		PubKeyValid:     pubkeyMatch,
		Version:         p.Version,
		DelegatesSource: p.Cons.GetDelegatesSource(),
		InCommittee:     pmProbe.InCommittee,
		CommitteeSize:   uint32(pmProbe.CommitteeSize),
		CommitteeIndex:  uint32(pmProbe.CommitteeIndex),

		InDelegateList: true, // FIXME: correct value
		BestQC:         bestQC.Height,
		BestBlock:      bestBlock.Number,
		Pacemaker:      pacemaker,
		Chain:          chainProbe,
	}

	utils.WriteJSON(w, result)
}

func (p *Probe) HandleVersion(w http.ResponseWriter, r *http.Request) {
	utils.WriteJSON(w, p.Version)
}

func (p *Probe) HandlePeers(w http.ResponseWriter, r *http.Request) {
	utils.WriteJSON(w, p.Network.PeersStats())
}

// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package node

import (
	"encoding/hex"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/meterio/supernova/api/utils"
	"github.com/meterio/supernova/chain"
	"github.com/meterio/supernova/consensus"
	"github.com/meterio/supernova/libs/comm"
)

type Node struct {
	version string
	comm    *comm.Communicator
	Cons    *consensus.Reactor
	Chain   *chain.Chain
	pubkey  string
}

func New(version string, comm *comm.Communicator, cons *consensus.Reactor, c *chain.Chain, pubkey []byte) *Node {
	return &Node{
		version,
		comm,
		cons,
		c,
		hex.EncodeToString(pubkey),
	}
}

func (n *Node) PeersStats() []*PeerStats {
	return ConvertPeersStats(n.comm.PeersStats())
}

func (n *Node) handlePeerStat(w http.ResponseWriter, req *http.Request) error {
	return utils.WriteJSON(w, n.PeersStats())
}

func (n *Node) handleDiscoveredPeers(w http.ResponseWriter, req *http.Request) error {
	nodes := n.comm.GetDiscoveredNodes()
	result := make([]*Peer, 0)
	for _, n := range nodes {
		peer := convertNode(n)
		result = append(result, peer)
	}
	return utils.WriteJSON(w, result)
}

func (n *Node) handleChainId(w http.ResponseWriter, req *http.Request) error {

	// FIXME: get the correct chainId
	return utils.WriteJSON(w, 82) // mainnet

}

func (n *Node) handleVersion(w http.ResponseWriter, r *http.Request) error {
	return utils.WriteJSON(w, n.version)
}

func (n *Node) handleProbe(w http.ResponseWriter, r *http.Request) error {
	name := ""

	bestBlock, _ := convertBlock(n.Chain.BestBlock())
	bestQC, _ := convertQC(n.Chain.BestQC())
	pmProbe := n.Cons.PacemakerProbe()
	pacemaker, _ := convertPacemakerProbe(pmProbe)
	chainProbe := &ChainProbe{
		BestBlock: bestBlock,
		BestQC:    bestQC,
	}
	result := ProbeResult{
		Name:           name,
		PubKey:         n.pubkey,
		Version:        n.version,
		InCommittee:    pmProbe.InCommittee,
		CommitteeSize:  uint32(pmProbe.CommitteeSize),
		CommitteeIndex: uint32(pmProbe.CommitteeIndex),

		BestQC:    bestQC.Height,
		BestBlock: bestBlock.Number,
		Pacemaker: pacemaker,
		Chain:     chainProbe,
	}

	return utils.WriteJSON(w, result)
}

func (n *Node) Mount(root *mux.Router, pathPrefix string) {
	sub := root.PathPrefix(pathPrefix).Subrouter()

	sub.Path("/peerstat").Methods("Get").HandlerFunc(utils.WrapHandlerFunc(n.handlePeerStat))
	sub.Path("/peers").Methods("Get").HandlerFunc(utils.WrapHandlerFunc(n.handleDiscoveredPeers))
	sub.Path("/chainid").Methods("Get").HandlerFunc(utils.WrapHandlerFunc(n.handleChainId))
	sub.Path("/version").Methods("Get").HandlerFunc(utils.WrapHandlerFunc(n.handleVersion))
	sub.Path("/probe").Methods("Get").HandlerFunc(utils.WrapHandlerFunc(n.handleProbe))
	sub.Path("/msg").Methods("Post").HandlerFunc(n.Cons.OnReceiveMsg)

}

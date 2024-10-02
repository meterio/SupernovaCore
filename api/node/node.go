// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package node

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/meterio/supernova/api/utils"
	"github.com/meterio/supernova/meter"
)

type Node struct {
	nw Network
}

func New(nw Network) *Node {
	return &Node{
		nw,
	}
}

func (n *Node) PeersStats() []*PeerStats {
	return ConvertPeersStats(n.nw.PeersStats())
}

func (n *Node) handleNetwork(w http.ResponseWriter, req *http.Request) error {
	return utils.WriteJSON(w, n.PeersStats())
}

func (n *Node) handleGetChainId(w http.ResponseWriter, req *http.Request) error {
	if meter.IsMainNet() {
		return utils.WriteJSON(w, 82) // mainnet
	}
	return utils.WriteJSON(w, 83) // testnet
}

func (n *Node) Mount(root *mux.Router, pathPrefix string) {
	sub := root.PathPrefix(pathPrefix).Subrouter()

	sub.Path("/network/peers").Methods("Get").HandlerFunc(utils.WrapHandlerFunc(n.handleNetwork))
	sub.Path("/chainid").Methods("Get").HandlerFunc(utils.WrapHandlerFunc(n.handleGetChainId))
}

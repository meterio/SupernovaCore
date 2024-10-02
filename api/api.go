// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package api

import (
	"net/http"
	"strings"

	assetfs "github.com/elazarl/go-bindata-assetfs"
	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/meterio/supernova/api/blocks"
	"github.com/meterio/supernova/api/doc"
	"github.com/meterio/supernova/api/node"
	"github.com/meterio/supernova/api/peers"
	"github.com/meterio/supernova/chain"
	"github.com/meterio/supernova/p2psrv"
	"github.com/meterio/supernova/txpool"
)

// New return api router
func New(chain *chain.Chain, txPool *txpool.TxPool, nw node.Network, allowedOrigins string, backtraceLimit uint32, p2pServer *p2psrv.Server) (http.HandlerFunc, func()) {
	origins := strings.Split(strings.TrimSpace(allowedOrigins), ",")
	for i, o := range origins {
		origins[i] = strings.ToLower(strings.TrimSpace(o))
	}

	router := mux.NewRouter()

	// to serve api doc and swagger-ui
	router.PathPrefix("/doc").Handler(
		http.StripPrefix("/doc/", http.FileServer(
			&assetfs.AssetFS{
				Asset:     doc.Asset,
				AssetDir:  doc.AssetDir,
				AssetInfo: doc.AssetInfo})))

	// redirect swagger-ui
	router.Path("/").HandlerFunc(
		func(w http.ResponseWriter, req *http.Request) {
			http.Redirect(w, req, "doc/swagger-ui/", http.StatusTemporaryRedirect)
		})

	blocks.New(chain).
		Mount(router, "/blocks")
	node.New(nw).
		Mount(router, "/node")
	peers.New(p2pServer).Mount(router, "/peers")

	return handlers.CORS(
			handlers.AllowedOrigins(origins),
			handlers.AllowedHeaders([]string{"content-type"}))(router).ServeHTTP,
		func() {} // subscriptions handles hijacked conns, which need to be closed
}

// Copyright (c) 2020 The Meter.io developers

// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying
// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"time"

	cmtproxy "github.com/cometbft/cometbft/v2/proxy"
	cmtrpctypes "github.com/cometbft/cometbft/v2/rpc/jsonrpc/types"
	assetfs "github.com/elazarl/go-bindata-assetfs"

	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/meterio/supernova/api/doc"
	"github.com/meterio/supernova/api/jsonrpc"
	"github.com/meterio/supernova/chain"
	"github.com/meterio/supernova/consensus"
	"github.com/meterio/supernova/libs/co"
	"github.com/meterio/supernova/libs/p2p"
	"github.com/meterio/supernova/txpool"
)

// CheckTx result.
type ResultBroadcastTx struct {
	Code      uint32 `json:"code"`
	Data      []byte `json:"data"`
	Log       string `json:"log"`
	Codespace string `json:"codespace"`

	Hash []byte `json:"hash"`
}

type Result struct {
	JSONRPC string                `json:"jsonrpc"`
	Id      uint32                `json:"id"`
	Result  ResultBroadcastTx     `json:"result"`
	Error   *cmtrpctypes.RPCError `json:"error,omitempty"`
}

type APIServer struct {
	listenAddr string
	handler    http.Handler
}

// loggingMiddleware logs request and response
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Log request
		bodyBytes, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes)) // Restore body
		log.Printf("REQUEST: %s %s\nHeaders: %v\nBody: %s\n",
			r.Method, r.URL, r.Header, string(bodyBytes))

		// Capture response
		rw := &responseCapture{ResponseWriter: w, body: new(bytes.Buffer)}
		next.ServeHTTP(rw, r)

		// Log response
		log.Printf("RESPONSE: status=%d, body=%s\n", rw.statusCode, rw.body.String())
	})
}

type responseCapture struct {
	http.ResponseWriter
	body       *bytes.Buffer
	statusCode int
}

func (rw *responseCapture) WriteHeader(statusCode int) {
	rw.statusCode = statusCode
	rw.ResponseWriter.WriteHeader(statusCode)
}

func (rw *responseCapture) Write(b []byte) (int, error) {
	rw.body.Write(b) // copy to buffer
	return rw.ResponseWriter.Write(b)
}

// New return api router
func NewAPIServer(proxyAppQuery cmtproxy.AppConnQuery, listenAddr string, chainId uint64, version string, chain *chain.Chain, txPool *txpool.TxPool, pacemaker *consensus.Pacemaker, pubkey []byte, p2pSrv p2p.P2P) *APIServer {
	router := mux.NewRouter()

	logger := slog.With("mod", "api")

	// to serve api doc and swagger-ui
	router.PathPrefix("/doc").Handler(
		http.StripPrefix("/doc/", http.FileServer(
			&assetfs.AssetFS{
				Asset:     doc.Asset,
				AssetDir:  doc.AssetDir,
				AssetInfo: doc.AssetInfo})))

	// jsonrpc.New(chain).Mount(router, "/")

	handler := jsonrpc.New(chain, txPool, proxyAppQuery)

	router.Path("/").Methods("POST").HandlerFunc(
		func(w http.ResponseWriter, req *http.Request) {
			jsonReq := cmtrpctypes.RPCRequest{}
			decoder := json.NewDecoder(req.Body)
			err := decoder.Decode(&jsonReq)
			if err != nil {
				panic(err)
			}

			params := jsonReq.Params
			logger.Info("received JSON-RPC call", "method", jsonReq.Method, "params", string(params))

			var m map[string]interface{}
			if err := json.Unmarshal(params, &m); err != nil {
				panic(err)
			}

			var result json.RawMessage
			switch jsonReq.Method {
			case "abci_query":
				result, err = handler.HandleABCIQuery(params)
			case "broadcast_tx_sync":
				result, err = handler.HandleBroadcastTx(params)
			}

			if err != nil {
				panic(err)
			}

			p := cmtrpctypes.RPCResponse{
				JSONRPC: jsonReq.JSONRPC,
				ID:      jsonReq.ID,
				Result:  result,
			}
			json.NewEncoder(w).Encode(p)
		})

	// Walk through and print all registered routes
	// router.Walk(func(route *mux.Route, router *mux.Router, ancestors []*mux.Route) error {
	// 	pathTemplate, err := route.GetPathTemplate()
	// 	if err != nil {
	// 		return err
	// 	}

	// 	methods, err := route.GetMethods()
	// 	if err != nil {
	// 		// No methods explicitly set, ignore
	// 		methods = []string{"ANY"}
	// 	}

	// 	fmt.Printf("Path: %-20s Methods: %v Handler: %v\n", pathTemplate, methods, route.GetHandler())
	// 	return nil
	// })

	router.NotFoundHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slog.Warn("404 Not Found", "url", r.URL.Path)
		http.Error(w, "Not found", http.StatusNotFound)
	})

	return &APIServer{
		listenAddr: listenAddr,
		handler: handlers.CORS(
			handlers.AllowedOrigins([]string{"*"}),
			handlers.AllowedHeaders([]string{"content-type"}))(router)}
}

func (api *APIServer) Start(ctx context.Context) {

	listener, err := net.Listen("tcp", api.listenAddr)
	if err != nil {
		panic(err)
	}

	timeout := 10000
	handler := api.handler
	handler = handleAPITimeout(handler, time.Duration(timeout)*time.Millisecond)
	handler = handleXVersion(handler)
	handler = requestBodyLimit(handler)
	handler = loggingMiddleware(handler)
	srv := &http.Server{
		Handler:      handler,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 18 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
	var goes co.Goes
	goes.Go(func() {
		slog.Info("API started", "addr", api.listenAddr)
		err := srv.Serve(listener)
		if err != nil {
			slog.Warn(err.Error())
		}
	})
	<-ctx.Done()
	srv.Shutdown(ctx)
	goes.Wait()

}

// middleware to set 'x-meter-ver' to response headers.
func handleXVersion(h http.Handler) http.Handler {
	const headerKey = "x-version"
	ver := doc.Version()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(headerKey, ver)
		h.ServeHTTP(w, r)
	})
}

// middleware for http request timeout.
func handleAPITimeout(h http.Handler, timeout time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		r = r.WithContext(ctx)
		h.ServeHTTP(w, r)
	})
}

// middleware to limit request body size.
func requestBodyLimit(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 96*1000)
		h.ServeHTTP(w, r)
	})
}

package httprestful

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"io/ioutil"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/elastos/Elastos.ELA.Elephant.Node/servers"
	"github.com/elastos/Elastos.ELA/common/config"
	"github.com/elastos/Elastos.ELA/common/log"
	. "github.com/elastos/Elastos.ELA/errors"
)

const (
	ApiGetConnectionCount  = "/api/v1/node/connectioncount"
	ApiGetNodeState        = "/api/v1/node/state"
	ApiGetBlockTxsByHeight = "/api/v1/block/transactions/height/:height"
	ApiGetBlockByHeight    = "/api/v1/block/details/height/:height"
	ApiGetBlockByHash      = "/api/v1/block/details/hash/:hash"
	ApiGetBlockHeight      = "/api/v1/block/height"
	ApiGetBlockHash        = "/api/v1/block/hash/:height"
	ApiGetTransaction      = "/api/v1/transaction/:hash"
	ApiGetAsset            = "/api/v1/asset/:hash"
	ApiGetBalanceByAddr    = "/api/v1/asset/balances/:addr"
	ApiGetBalanceByAsset   = "/api/v1/asset/balance/:addr/:assetid"
	ApiGetUTXOByAsset      = "/api/v1/asset/utxo/:addr/:assetid"
	ApiGetUTXOByAddr       = "/api/v1/asset/utxos/:addr"
	ApiSendRawTransaction  = "/api/v1/transaction"
	ApiGetTransactionPool  = "/api/v1/transactionpool"

	//extended
	ApiGetHistory                   = "/api/v1/history/:addr"
	ApiGetHistory_v2                = "/api/v2/history/:addr"
	ApiGetHistory_v3                = "/api/v3/history/:addr"
	ApiCreateTx                     = "/api/v1/createTx"
	ApiGetPublicKey                 = "/api/v1/pubkey/:addr"
	ApiGetBalance                   = "/api/v1/balance/:addr"
	ApiSendRawTx                    = "/api/v1/sendRawTx"
	ApiCreateVoteTx                 = "/api/v1/createVoteTx"
	ApiCurrHeight                   = "/api/v1/currHeight"
	ApiGetNodeFee                   = "/api/v1/fee"
	ApiProducerStatistic            = "/api/v1/dpos/producer/:producer/:height"
	ApiProducerStatistic_compatible = "/api/v1/dpos/producer/:producer"
	ApiVoterStatistic               = "/api/v1/dpos/address/:addr"
	ApiProducerRankByHeight         = "/api/v1/dpos/rank/height/:height"
	ApiTotalVoteByHeight            = "/api/v1/dpos/vote/height/:height"
	ApiGetProducerByTxs             = "/api/v1/dpos/transaction/producer"
	ApiNodeRewardAddr               = "/api/v1/node/reward/address"
	ApiGetSpendUtxos                = "/api/v1/spend/utxos"
	ApiGetTx                        = "/api/v1/tx/:hash"
	ApiCrDidStatistic               = "/api/v1/crc/did/:did/:height"
	ApiCrVoterStatistic             = "/api/v1/crc/address/:addr"
	ApiCrCandidateRankByHeight      = "/api/v1/crc/rank/height/:height"
	ApiTotalCandidateVoteByHeight   = "/api/v1/crc/vote/height/:height"
	ApiGetCandidateByTxs            = "/api/v1/crc/transaction/producer"
)

var ext_api_handle = map[string]bool{
	ApiGetHistory:                   true,
	ApiGetHistory_v2:                true,
	ApiGetHistory_v3:                true,
	ApiCreateTx:                     true,
	ApiGetPublicKey:                 true,
	ApiGetBalance:                   true,
	ApiSendRawTx:                    true,
	ApiCreateVoteTx:                 true,
	ApiCurrHeight:                   true,
	ApiGetNodeFee:                   true,
	ApiProducerStatistic:            true,
	ApiProducerStatistic_compatible: true,
	ApiVoterStatistic:               true,
	ApiProducerRankByHeight:         true,
	ApiTotalVoteByHeight:            true,
	ApiGetProducerByTxs:             true,
	ApiNodeRewardAddr:               true,
	ApiGetSpendUtxos:                true,
	ApiGetTx:                        true,

	ApiCrDidStatistic:             true,
	ApiCrVoterStatistic:           true,
	ApiCrCandidateRankByHeight:    true,
	ApiTotalCandidateVoteByHeight: true,
	ApiGetCandidateByTxs:          true,
}

type Action struct {
	sync.RWMutex
	name    string
	handler func(servers.Params) map[string]interface{}
}

type restServer struct {
	router   *Router
	listener net.Listener
	server   *http.Server
	postMap  map[string]Action
	getMap   map[string]Action
}

type ApiServer interface {
	Start()
	Stop()
}

func StartServer() {
	rest := InitRestServer()
	rest.Start()
}

func InitRestServer() ApiServer {
	rt := &restServer{}
	rt.router = &Router{}
	rt.initializeMethod()
	rt.initGetHandler()
	rt.initPostHandler()
	return rt
}

func (rt *restServer) Start() {
	if config.Parameters.HttpRestPort == 0 {
		log.Fatal("Not configure HttpRestPort port ")
	}

	if config.Parameters.HttpRestPort%1000 == servers.TlsPort {
		var err error
		rt.listener, err = rt.initTlsListen()
		if err != nil {
			log.Error("Https Cert: ", err.Error())
		}
	} else {
		var err error
		rt.listener, err = net.Listen("tcp", ":"+strconv.Itoa(config.Parameters.HttpRestPort))
		if err != nil {
			log.Fatal("net.Listen: ", err.Error())
		}
	}

	rt.server = &http.Server{Handler: rt.router}
	err := rt.server.Serve(rt.listener)

	if err != nil {
		log.Fatal("ListenAndServe: ", err.Error())
	}
}

func (rt *restServer) initializeMethod() {

	getMethodMap := map[string]Action{
		ApiGetConnectionCount:  {name: "getconnectioncount", handler: servers.GetConnectionCount},
		ApiGetNodeState:        {name: "getnodestate", handler: servers.GetNodeState},
		ApiGetBlockTxsByHeight: {name: "getblocktransactionsbyheight", handler: servers.GetTransactionsByHeight},
		ApiGetBlockByHeight:    {name: "getblockbyheight", handler: servers.GetBlockByHeight},
		ApiGetBlockByHash:      {name: "getblockbyhash", handler: servers.GetBlockByHash},
		ApiGetBlockHeight:      {name: "getblockheight", handler: servers.GetBlockHeight},
		ApiGetBlockHash:        {name: "getblockhash", handler: servers.GetBlockHash},
		ApiGetTransactionPool:  {name: "gettransactionpool", handler: servers.GetTransactionPool},
		ApiGetTransaction:      {name: "gettransaction", handler: servers.GetTransactionByHash},
		ApiGetAsset:            {name: "getasset", handler: servers.GetAssetByHash},
		ApiGetUTXOByAddr:       {name: "getutxobyaddr", handler: servers.GetUnspends},
		ApiGetUTXOByAsset:      {name: "getutxobyasset", handler: servers.GetUnspendOutput},
		ApiGetBalanceByAddr:    {name: "getbalancebyaddr", handler: servers.GetBalanceByAddr},
		ApiGetBalanceByAsset:   {name: "getbalancebyasset", handler: servers.GetBalanceByAsset},

		// extended
		ApiGetHistory:                   {name: "gethistory", handler: servers.GetHistory},
		ApiGetHistory_v2:                {name: "gethistory_v2", handler: servers.GetHistory},
		ApiGetHistory_v3:                {name: "gethistory_v3", handler: servers.GetHistory},
		ApiGetPublicKey:                 {name: "getpublickey", handler: servers.GetPublicKey},
		ApiGetBalance:                   {name: "getbalance", handler: servers.GetBalance},
		ApiCurrHeight:                   {name: "currHeight", handler: servers.CurrHeight},
		ApiGetNodeFee:                   {name: "nodeFee", handler: servers.GetNodeFee},
		ApiProducerStatistic:            {name: "producerStatistic", handler: servers.ProducerStatistic},
		ApiProducerStatistic_compatible: {name: "producerStatistic_compatible", handler: servers.ProducerStatistic},
		ApiVoterStatistic:               {name: "voterStatistic", handler: servers.VoterStatistic},
		ApiProducerRankByHeight:         {name: "producerRankByHeight", handler: servers.ProducerRankByHeight},
		ApiTotalVoteByHeight:            {name: "totalVoteByHeight", handler: servers.TotalVoteByHeight},
		ApiNodeRewardAddr:               {name: "nodeRewardAddr", handler: servers.NodeRewardAddr},
		ApiGetTx:                        {name: "gettx", handler: servers.GetTx},

		ApiCrDidStatistic:             {name: "ApiCrDidStatistic", handler: servers.CrDidStatistic},
		ApiCrVoterStatistic:           {name: "ApiCrVoterStatistic", handler: servers.CrVoterStatistic},
		ApiCrCandidateRankByHeight:    {name: "ApiCrCandidateRankByHeight", handler: servers.CrCandidateRankByHeight},
		ApiTotalCandidateVoteByHeight: {name: "ApiTotalCandidateVoteByHeight", handler: servers.TotalCandidateVoteByHeight},
	}
	postMethodMap := map[string]Action{
		ApiSendRawTransaction: {name: "sendrawtransaction", handler: servers.SendRawTransaction},

		// extended
		ApiCreateTx:          {name: "createTx", handler: servers.CreateTx},
		ApiSendRawTx:         {name: "sendRawTx", handler: servers.SendRawTx},
		ApiCreateVoteTx:      {name: "createVoteTx", handler: servers.CreateVoteTx},
		ApiGetProducerByTxs:  {name: "getProducerByTxs", handler: servers.GetProducerByTxs},
		ApiGetSpendUtxos:     {name: "getSpendUtxos", handler: servers.GetSpendUtxos},
		ApiGetCandidateByTxs: {name: "ApiGetCandidateByTxs", handler: servers.GetCandidateByTxs},
	}
	rt.postMap = postMethodMap
	rt.getMap = getMethodMap
}

func (rt *restServer) getPath(url string) string {

	if strings.Contains(url, strings.TrimRight(ApiGetBlockTxsByHeight, ":height")) {
		return ApiGetBlockTxsByHeight
	} else if strings.Contains(url, strings.TrimRight(ApiGetBlockByHeight, ":height")) {
		return ApiGetBlockByHeight
	} else if strings.Contains(url, strings.TrimRight(ApiGetBlockByHash, ":hash")) {
		return ApiGetBlockByHash
	} else if strings.Contains(url, strings.TrimRight(ApiGetBlockHash, ":height")) {
		return ApiGetBlockHash
	} else if strings.Contains(url, strings.TrimRight(ApiGetTransaction, ":hash")) {
		return ApiGetTransaction
	} else if strings.Contains(url, strings.TrimRight(ApiGetBalanceByAddr, ":addr")) {
		return ApiGetBalanceByAddr
	} else if strings.Contains(url, strings.TrimRight(ApiGetBalanceByAsset, ":addr/:assetid")) {
		return ApiGetBalanceByAsset
	} else if strings.Contains(url, strings.TrimRight(ApiGetUTXOByAddr, ":addr")) {
		return ApiGetUTXOByAddr
	} else if strings.Contains(url, strings.TrimRight(ApiGetUTXOByAsset, ":addr/:assetid")) {
		return ApiGetUTXOByAsset
	} else if strings.Contains(url, strings.TrimRight(ApiGetAsset, ":hash")) {
		return ApiGetAsset
	} else if strings.Contains(url, strings.TrimRight(ApiGetHistory, ":addr")) {
		return ApiGetHistory
	} else if strings.Contains(url, strings.TrimRight(ApiGetHistory_v2, ":addr")) {
		return ApiGetHistory_v2
	} else if strings.Contains(url, strings.TrimRight(ApiGetHistory_v3, ":addr")) {
		return ApiGetHistory_v3
	} else if strings.Contains(url, strings.TrimRight(ApiGetPublicKey, ":addr")) {
		return ApiGetPublicKey
	} else if strings.Contains(url, strings.TrimRight(ApiGetBalance, ":addr")) {
		return ApiGetBalance
	} else if strings.Contains(url, strings.TrimSuffix(ApiProducerStatistic, ":producer/:height")) {
		return ApiProducerStatistic
	} else if strings.Contains(url, strings.TrimSuffix(ApiProducerStatistic_compatible, ":producer")) {
		return ApiProducerStatistic_compatible
	} else if strings.Contains(url, strings.TrimSuffix(ApiVoterStatistic, ":addr")) {
		return ApiVoterStatistic
	} else if strings.Contains(url, strings.TrimSuffix(ApiProducerRankByHeight, ":height")) {
		return ApiProducerRankByHeight
	} else if strings.Contains(url, strings.TrimSuffix(ApiTotalVoteByHeight, ":height")) {
		return ApiTotalVoteByHeight
	} else if strings.Contains(url, strings.TrimSuffix(ApiGetTx, ":hash")) {
		return ApiGetTx

	} else if strings.Contains(url, strings.TrimSuffix(ApiCrDidStatistic, ":did/:height")) {
		return ApiCrDidStatistic
	} else if strings.Contains(url, strings.TrimSuffix(ApiCrVoterStatistic, ":addr")) {
		return ApiCrVoterStatistic
	} else if strings.Contains(url, strings.TrimSuffix(ApiCrCandidateRankByHeight, ":height")) {
		return ApiCrCandidateRankByHeight
	} else if strings.Contains(url, strings.TrimSuffix(ApiTotalCandidateVoteByHeight, ":height")) {
		return ApiTotalCandidateVoteByHeight
	}
	return url
}

func (rt *restServer) getParams(r *http.Request, url string, req map[string]interface{}) map[string]interface{} {
	switch url {
	case ApiGetConnectionCount:

	case ApiGetNodeState:

	case ApiGetBlockTxsByHeight:
		req["height"] = getParam(r, "height")

	case ApiGetBlockByHeight:
		req["height"] = getParam(r, "height")

	case ApiGetBlockByHash:
		req["blockhash"] = getParam(r, "hash")

	case ApiGetBlockHeight:

	case ApiGetTransactionPool:

	case ApiGetBlockHash:
		req["height"] = getParam(r, "height")

	case ApiGetTransaction:
		req["hash"] = getParam(r, "hash")

	case ApiGetAsset:
		req["hash"] = getParam(r, "hash")

	case ApiGetBalanceByAsset:
		req["addr"] = getParam(r, "addr")
		req["assetid"] = getParam(r, "assetid")

	case ApiGetBalanceByAddr:
		req["addr"] = getParam(r, "addr")

	case ApiGetUTXOByAddr:
		req["addr"] = getParam(r, "addr")

	case ApiGetUTXOByAsset:
		req["addr"] = getParam(r, "addr")
		req["assetid"] = getParam(r, "assetid")

	case ApiSendRawTransaction:

	case ApiGetHistory:
		req["addr"] = getParam(r, "addr")
		req["version"] = "1"
		getQueryParam(r, req)
	case ApiGetHistory_v2:
		req["addr"] = getParam(r, "addr")
		req["version"] = "2"
		getQueryParam(r, req)
	case ApiGetHistory_v3:
		req["addr"] = getParam(r, "addr")
		req["version"] = "3"
		getQueryParam(r, req)
	case ApiGetPublicKey:
		req["addr"] = getParam(r, "addr")
		getQueryParam(r, req)
	case ApiGetBalance:
		req["addr"] = getParam(r, "addr")
		getQueryParam(r, req)
	case ApiCreateTx:
	case ApiSendRawTx:
	case ApiCurrHeight:
	case ApiGetNodeFee:
	case ApiProducerStatistic:
		req["producer"] = getParam(r, "producer")
		req["height"] = getParam(r, "height")
		getQueryParam(r, req)
	case ApiProducerStatistic_compatible:
		req["producer"] = getParam(r, "producer")
		req["height"] = getParam(r, "height")
		getQueryParam(r, req)
	case ApiVoterStatistic:
		req["addr"] = getParam(r, "addr")
		getQueryParam(r, req)
	case ApiProducerRankByHeight:
		req["height"] = getParam(r, "height")
		getQueryParam(r, req)
	case ApiTotalVoteByHeight:
		req["height"] = getParam(r, "height")
		getQueryParam(r, req)
	case ApiGetProducerByTxs:
	case ApiNodeRewardAddr:
	case ApiGetSpendUtxos:
	case ApiGetTx:
		req["hash"] = getParam(r, "hash")
	case ApiCrDidStatistic:
		req["did"] = getParam(r, "did")
		req["height"] = getParam(r, "height")
		getQueryParam(r, req)
	case ApiCrVoterStatistic:
		req["addr"] = getParam(r, "addr")
		getQueryParam(r, req)
	case ApiCrCandidateRankByHeight:
		req["height"] = getParam(r, "height")
		getQueryParam(r, req)
	case ApiTotalCandidateVoteByHeight:
		req["height"] = getParam(r, "height")
		getQueryParam(r, req)
	}
	return req
}

func (rt *restServer) initGetHandler() {

	for k, _ := range rt.getMap {
		rt.router.Get(k, func(w http.ResponseWriter, r *http.Request) {

			var req = make(map[string]interface{})
			var resp map[string]interface{}

			url := rt.getPath(r.URL.Path)
			if h, ok := rt.getMap[url]; ok {
				req = rt.getParams(r, url, req)
				resp = h.handler(req)
			} else {
				if _, ok = ext_api_handle[k]; !ok {
					resp = servers.ResponsePack(InvalidMethod, "")
				} else {
					resp = servers.ResponsePackEx(InvalidMethod, "")
				}
			}
			rt.response(w, resp)
		})
		k = strings.Replace(k, "/api/v", "/api/", 1)
		rt.router.Get(k, func(w http.ResponseWriter, r *http.Request) {

			var req = make(map[string]interface{})
			var resp map[string]interface{}
			if !strings.Contains(r.URL.Path, "/api/v") {
				r.URL.Path = strings.Replace(r.URL.Path, "/api/", "/api/v", 1)
			}
			url := rt.getPath(r.URL.Path)
			if h, ok := rt.getMap[url]; ok {
				req = rt.getParams(r, url, req)
				resp = h.handler(req)
			} else {
				if _, ok = ext_api_handle[k]; !ok {
					resp = servers.ResponsePack(InvalidMethod, "")
				} else {
					resp = servers.ResponsePackEx(InvalidMethod, "")
				}
			}
			rt.response(w, resp)
		})
	}
}

func (rt *restServer) initPostHandler() {
	for k, _ := range rt.postMap {
		rt.router.Post(k, func(w http.ResponseWriter, r *http.Request) {

			body, _ := ioutil.ReadAll(r.Body)
			defer r.Body.Close()

			var req = make(map[string]interface{})
			var resp map[string]interface{}

			url := rt.getPath(r.URL.Path)
			if h, ok := rt.postMap[url]; ok {
				if err := json.Unmarshal(body, &req); err == nil {
					req = rt.getParams(r, url, req)
					resp = h.handler(req)
				} else {
					if _, ok = ext_api_handle[k]; !ok {
						resp = servers.ResponsePack(InvalidMethod, "")
					} else {
						resp = servers.ResponsePackEx(InvalidMethod, "")
					}
				}
			} else {
				if _, ok = ext_api_handle[k]; !ok {
					resp = servers.ResponsePack(InvalidMethod, "")
				} else {
					resp = servers.ResponsePackEx(InvalidMethod, "")
				}
			}
			rt.response(w, resp)
		})
		k = strings.Replace(k, "/api/v", "/api/", 1)
		rt.router.Post(k, func(w http.ResponseWriter, r *http.Request) {

			body, _ := ioutil.ReadAll(r.Body)
			defer r.Body.Close()

			var req = make(map[string]interface{})
			var resp map[string]interface{}
			if !strings.Contains(r.URL.Path, "/api/v") {
				r.URL.Path = strings.Replace(r.URL.Path, "/api/", "/api/v", 1)
			}
			url := rt.getPath(r.URL.Path)
			if h, ok := rt.postMap[url]; ok {
				if err := json.Unmarshal(body, &req); err == nil {
					req = rt.getParams(r, url, req)
					resp = h.handler(req)
				} else {
					if _, ok = ext_api_handle[k]; !ok {
						resp = servers.ResponsePack(InvalidMethod, "")
					} else {
						resp = servers.ResponsePackEx(InvalidMethod, "")
					}
				}
			} else {
				if _, ok = ext_api_handle[k]; !ok {
					resp = servers.ResponsePack(InvalidMethod, "")
				} else {
					resp = servers.ResponsePackEx(InvalidMethod, "")
				}
			}
			rt.response(w, resp)
		})
	}
	//Options
	for k, _ := range rt.postMap {
		rt.router.Options(k, func(w http.ResponseWriter, r *http.Request) {
			rt.write(w, []byte{})
		})
	}

}

func (rt *restServer) write(w http.ResponseWriter, data []byte) {
	w.Header().Add("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("content-type", "application/json;charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Write(data)
}

func (rt *restServer) response(w http.ResponseWriter, resp map[string]interface{}) {
	data, err := json.Marshal(resp)
	if err != nil {
		log.Fatal("HTTP Handle - json.Marshal: %v", err)
		return
	}
	log.Debugf("Response : %s", string(data))
	rt.write(w, data)
}

func (rt *restServer) Stop() {
	if rt.server != nil {
		rt.server.Shutdown(context.Background())
		log.Error("Close restful ")
	}
}

func (rt *restServer) initTlsListen() (net.Listener, error) {

	CertPath := config.Parameters.RestCertPath
	KeyPath := config.Parameters.RestKeyPath

	// load cert
	cert, err := tls.LoadX509KeyPair(CertPath, KeyPath)
	if err != nil {
		log.Error("load keys fail", err)
		return nil, err
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
	}

	log.Info("TLS listen port is ", strconv.Itoa(config.Parameters.HttpRestPort))
	listener, err := tls.Listen("tcp", ":"+strconv.Itoa(config.Parameters.HttpRestPort), tlsConfig)
	if err != nil {
		log.Error(err)
		return nil, err
	}
	return listener, nil
}

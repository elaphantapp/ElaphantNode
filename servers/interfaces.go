package servers

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	blockchain2 "github.com/elastos/Elastos.ELA.Elephant.Node/blockchain"
	common2 "github.com/elastos/Elastos.ELA.Elephant.Node/common"
	"github.com/elastos/Elastos.ELA.Elephant.Node/core/types"
	. "github.com/elastos/Elastos.ELA.Elephant.Node/servers/errors"
	"github.com/elastos/Elastos.ELA/account"
	aux "github.com/elastos/Elastos.ELA/auxpow"
	"github.com/elastos/Elastos.ELA/blockchain"
	"github.com/elastos/Elastos.ELA/common"
	"github.com/elastos/Elastos.ELA/common/config"
	"github.com/elastos/Elastos.ELA/common/log"
	"github.com/elastos/Elastos.ELA/core/contract"
	pg "github.com/elastos/Elastos.ELA/core/contract/program"
	. "github.com/elastos/Elastos.ELA/core/types"
	"github.com/elastos/Elastos.ELA/core/types/outputpayload"
	"github.com/elastos/Elastos.ELA/core/types/payload"
	crstate "github.com/elastos/Elastos.ELA/cr/state"
	"github.com/elastos/Elastos.ELA/crypto"
	"github.com/elastos/Elastos.ELA/dpos"
	"github.com/elastos/Elastos.ELA/dpos/state"
	"github.com/elastos/Elastos.ELA/elanet"
	"github.com/elastos/Elastos.ELA/elanet/pact"
	"github.com/elastos/Elastos.ELA/mempool"
	"github.com/elastos/Elastos.ELA/p2p/msg"
	"github.com/elastos/Elastos.ELA/pow"
	"github.com/elastos/Elastos.ELA/wallet"
	"github.com/tidwall/gjson"
	"math"
	"sort"
	"strconv"
	"strings"
)

var (
	NodePrivKey []byte
	NodePubKey  []byte
	Compile     string
	Config      *config.Configuration
	ChainParams *config.Params
	Chain       *blockchain.BlockChain
	Store       blockchain.IChainStore
	TxMemPool   *mempool.TxPool
	Pow         *pow.Service
	Server      elanet.Server
	Arbiter     *dpos.Arbitrator
	Arbiters    state.Arbitrators
	Wallet      *wallet.Wallet
	emptyHash   = common.Uint168{}
)

func ToReversedString(hash common.Uint256) string {
	return common.BytesToHexString(common.BytesReverse(hash[:]))
}

func FromReversedString(reversed string) ([]byte, error) {
	bytes, err := common.HexStringToBytes(reversed)
	return common.BytesReverse(bytes), err
}

func GetTransactionInfo(tx *Transaction) *TransactionInfo {
	inputs := make([]InputInfo, len(tx.Inputs))
	for i, v := range tx.Inputs {
		inputs[i].TxID = ToReversedString(v.Previous.TxID)
		inputs[i].VOut = v.Previous.Index
		inputs[i].Sequence = v.Sequence
	}

	outputs := make([]RpcOutputInfo, len(tx.Outputs))
	for i, v := range tx.Outputs {
		outputs[i].Value = v.Value.String()
		outputs[i].Index = uint32(i)
		address, _ := v.ProgramHash.ToAddress()
		outputs[i].Address = address
		outputs[i].AssetID = ToReversedString(v.AssetID)
		outputs[i].OutputLock = v.OutputLock
		outputs[i].OutputType = uint32(v.Type)
		outputs[i].OutputPayload = getOutputPayloadInfo(v.Payload)
	}

	attributes := make([]AttributeInfo, len(tx.Attributes))
	for i, v := range tx.Attributes {
		attributes[i].Usage = v.Usage
		attributes[i].Data = common.BytesToHexString(v.Data)
	}

	programs := make([]ProgramInfo, len(tx.Programs))
	for i, v := range tx.Programs {
		programs[i].Code = common.BytesToHexString(v.Code)
		programs[i].Parameter = common.BytesToHexString(v.Parameter)
	}

	var txHash = tx.Hash()
	var txHashStr = ToReversedString(txHash)
	var size = uint32(tx.GetSize())
	return &TransactionInfo{
		TxID:           txHashStr,
		Hash:           txHashStr,
		Size:           size,
		VSize:          size,
		Version:        tx.Version,
		TxType:         tx.TxType,
		PayloadVersion: tx.PayloadVersion,
		Payload:        getPayloadInfo(tx.Payload, tx.PayloadVersion),
		Attributes:     attributes,
		Inputs:         inputs,
		Outputs:        outputs,
		LockTime:       tx.LockTime,
		Programs:       programs,
	}
}

func GetTransactionContextInfo(header *Header, tx *Transaction) *TransactionContextInfo {
	var blockHash string
	var confirmations uint32
	var time uint32
	var blockTime uint32
	if header != nil {
		confirmations = Store.GetHeight() - header.Height + 1
		blockHash = ToReversedString(header.Hash())
		time = header.Timestamp
		blockTime = header.Timestamp
	}

	txInfo := GetTransactionInfo(tx)

	return &TransactionContextInfo{
		TransactionInfo: txInfo,
		BlockHash:       blockHash,
		Confirmations:   confirmations,
		Time:            time,
		BlockTime:       blockTime,
	}
}

// Input JSON string examples for getblock method as following:
func GetRawTransaction(param Params) map[string]interface{} {
	str, ok := param.String("txid")
	if !ok {
		return ResponsePack(InvalidParams, "")
	}

	hex, err := FromReversedString(str)
	if err != nil {
		return ResponsePack(InvalidParams, "")
	}
	var hash common.Uint256
	err = hash.Deserialize(bytes.NewReader(hex))
	if err != nil {
		return ResponsePack(InvalidTransaction, "")
	}

	var header *Header
	tx, height, err := Store.GetTransaction(hash)
	if err != nil {
		//try to find transaction in transaction pool.
		tx = TxMemPool.GetTransaction(hash)
		if tx == nil {
			return ResponsePack(UnknownTransaction,
				"cannot find transaction in blockchain and transactionpool")
		}
	} else {
		hash, err := Chain.GetBlockHash(height)
		if err != nil {
			return ResponsePack(UnknownTransaction, "")
		}
		header, err = Chain.GetHeader(hash)
		if err != nil {
			return ResponsePack(UnknownTransaction, "")
		}
	}

	verbose, _ := param.Bool("verbose")
	if verbose {
		return ResponsePack(Success, GetTransactionContextInfo(header, tx))
	} else {
		buf := new(bytes.Buffer)
		tx.Serialize(buf)
		return ResponsePack(Success, common.BytesToHexString(buf.Bytes()))
	}
}

func GetNeighbors(param Params) map[string]interface{} {
	peers := Server.ConnectedPeers()
	neighborAddrs := make([]string, 0, len(peers))
	for _, peer := range peers {
		neighborAddrs = append(neighborAddrs, peer.ToPeer().String())
	}
	return ResponsePack(Success, neighborAddrs)
}

func GetNodeState(param Params) map[string]interface{} {
	peers := Server.ConnectedPeers()
	states := make([]*PeerInfo, 0, len(peers))
	for _, peer := range peers {
		snap := peer.ToPeer().StatsSnapshot()
		states = append(states, &PeerInfo{
			NetAddress:     snap.Addr,
			Services:       pact.ServiceFlag(snap.Services).String(),
			RelayTx:        snap.RelayTx != 0,
			LastSend:       snap.LastSend.String(),
			LastRecv:       snap.LastRecv.String(),
			ConnTime:       snap.ConnTime.String(),
			TimeOffset:     snap.TimeOffset,
			Version:        snap.Version,
			Inbound:        snap.Inbound,
			StartingHeight: snap.StartingHeight,
			LastBlock:      snap.LastBlock,
			LastPingTime:   snap.LastPingTime.String(),
			LastPingMicros: snap.LastPingMicros,
		})
	}
	return ResponsePack(Success, ServerInfo{
		Compile:   Compile,
		Height:    Chain.GetHeight(),
		Version:   pact.DPOSStartVersion,
		Services:  Server.Services().String(),
		Port:      Config.NodePort,
		RPCPort:   uint16(Config.HttpJsonPort),
		RestPort:  uint16(Config.HttpRestPort),
		WSPort:    uint16(Config.HttpWsPort),
		Neighbors: states,
	})
}

func SetLogLevel(param Params) map[string]interface{} {
	if rtn := checkRPCServiceLevel(config.ConfigurationPermitted); rtn != nil {
		return rtn
	}

	level, ok := param.Int("level")
	if !ok || level < 0 {
		return ResponsePack(InvalidParams, "level must be an integer in 0-6")
	}

	log.SetPrintLevel(uint8(level))
	return ResponsePack(Success, fmt.Sprint("log level has been set to ", level))
}

func CreateAuxBlock(param Params) map[string]interface{} {
	if rtn := checkRPCServiceLevel(config.MiningPermitted); rtn != nil {
		return rtn
	}

	payToAddr, ok := param.String("paytoaddress")
	if !ok {
		return ResponsePack(InvalidParams, "parameter paytoaddress not found")
	}

	block, err := Pow.CreateAuxBlock(payToAddr)
	if err != nil {
		return ResponsePack(InternalError, "generate block failed")
	}

	type AuxBlock struct {
		ChainID           int            `json:"chainid"`
		Height            uint32         `json:"height"`
		CoinBaseValue     common.Fixed64 `json:"coinbasevalue"`
		Bits              string         `json:"bits"`
		Hash              string         `json:"hash"`
		PreviousBlockHash string         `json:"previousblockhash"`
	}

	SendToAux := AuxBlock{
		ChainID:           aux.AuxPowChainID,
		Height:            Chain.GetHeight(),
		CoinBaseValue:     block.Transactions[0].Outputs[1].Value,
		Bits:              fmt.Sprintf("%x", block.Header.Bits),
		Hash:              block.Hash().String(),
		PreviousBlockHash: Chain.GetCurrentBlockHash().String(),
	}
	return ResponsePack(Success, &SendToAux)
}

func SubmitAuxBlock(param Params) map[string]interface{} {
	if rtn := checkRPCServiceLevel(config.MiningPermitted); rtn != nil {
		return rtn
	}

	blockHashHex, ok := param.String("blockhash")
	if !ok {
		return ResponsePack(InvalidParams, "parameter blockhash not found")
	}
	blockHash, err := common.Uint256FromHexString(blockHashHex)
	if err != nil {
		return ResponsePack(InvalidParams, "bad blockhash")
	}

	auxPow, ok := param.String("auxpow")
	if !ok {
		return ResponsePack(InvalidParams, "parameter auxpow not found")
	}
	var aux aux.AuxPow
	buf, _ := common.HexStringToBytes(auxPow)
	if err := aux.Deserialize(bytes.NewReader(buf)); err != nil {
		log.Debug("[json-rpc:SubmitAuxBlock] auxpow deserialization failed", auxPow)
		return ResponsePack(InternalError, "auxpow deserialization failed")
	}

	err = Pow.SubmitAuxBlock(blockHash, &aux)
	if err != nil {
		log.Debug(err)
		return ResponsePack(InternalError, "adding block failed")
	}

	log.Debug("AddBlock called finished and Pow.MsgBlock.MapNewBlock has been deleted completely")
	log.Info(auxPow, blockHash)
	return ResponsePack(Success, true)
}

func SubmitSidechainIllegalData(param Params) map[string]interface{} {
	if rtn := checkRPCServiceLevel(config.TransactionPermitted); rtn != nil {
		return rtn
	}

	if Arbiter == nil {
		return ResponsePack(InternalError, "arbiter disabled")
	}

	rawHex, ok := param.String("illegaldata")
	if !ok {
		return ResponsePack(InvalidParams, "parameter illegaldata not found")
	}

	var data payload.SidechainIllegalData
	buf, _ := common.HexStringToBytes(rawHex)
	if err := data.DeserializeUnsigned(bytes.NewReader(buf),
		payload.SidechainIllegalDataVersion); err != nil {
		log.Debug("[json-rpc:SubmitSidechainIllegalData] illegaldata deserialization failed", rawHex)
		return ResponsePack(InternalError, "illegaldata deserialization failed")
	}

	Arbiter.OnSidechainIllegalEvidenceReceived(&data)

	return ResponsePack(Success, true)
}

func GetArbiterPeersInfo(params Params) map[string]interface{} {
	if Arbiter == nil {
		return ResponsePack(InternalError, "arbiter disabled")
	}

	type peerInfo struct {
		OwnerPublicKey string `json:"ownerpublickey"`
		NodePublicKey  string `json:"nodepublickey"`
		IP             string `json:"ip"`
		ConnState      string `json:"connstate"`
	}

	peers := Arbiter.GetArbiterPeersInfo()

	result := make([]peerInfo, 0)
	for _, p := range peers {
		producer := Arbiters.GetConnectedProducer(p.PID[:])
		if producer == nil {
			continue
		}
		result = append(result, peerInfo{
			OwnerPublicKey: common.BytesToHexString(
				producer.GetOwnerPublicKey()),
			NodePublicKey: common.BytesToHexString(
				producer.GetNodePublicKey()),
			IP:        p.Addr,
			ConnState: p.State.String(),
		})
	}
	return ResponsePack(Success, result)
}

func GetArbitersInfo(params Params) map[string]interface{} {
	type arbitersInfo struct {
		Arbiters               []string `json:"arbiters"`
		Candidates             []string `json:"candidates"`
		NextArbiters           []string `json:"nextarbiters"`
		NextCandidates         []string `json:"nextcandidates"`
		OnDutyArbiter          string   `json:"ondutyarbiter"`
		CurrentTurnStartHeight int      `json:"currentturnstartheight"`
		NextTurnStartHeight    int      `json:"nextturnstartheight"`
	}

	dutyIndex := Arbiters.GetDutyIndex()
	result := &arbitersInfo{
		Arbiters:       make([]string, 0),
		Candidates:     make([]string, 0),
		NextArbiters:   make([]string, 0),
		NextCandidates: make([]string, 0),
		OnDutyArbiter:  common.BytesToHexString(Arbiters.GetOnDutyArbitrator()),

		CurrentTurnStartHeight: int(Chain.GetHeight()) - dutyIndex,
		NextTurnStartHeight: int(Chain.GetHeight()) +
			Arbiters.GetArbitersCount() - dutyIndex,
	}
	for _, v := range Arbiters.GetArbitrators() {
		var nodePK string
		if v.IsNormal {
			nodePK = common.BytesToHexString(v.NodePublicKey)
		}
		result.Arbiters = append(result.Arbiters, nodePK)
	}
	for _, v := range Arbiters.GetCandidates() {
		result.Candidates = append(result.Candidates, common.BytesToHexString(v))
	}
	for _, v := range Arbiters.GetNextArbitrators() {
		var nodePK string
		if v.IsNormal {
			nodePK = common.BytesToHexString(v.NodePublicKey)
		}
		result.NextArbiters = append(result.NextArbiters, nodePK)
	}
	for _, v := range Arbiters.GetNextCandidates() {
		result.NextCandidates = append(result.NextCandidates,
			common.BytesToHexString(v))
	}
	return ResponsePack(Success, result)
}

func GetInfo(param Params) map[string]interface{} {
	RetVal := struct {
		Version       uint32 `json:"version"`
		Balance       int    `json:"balance"`
		Blocks        uint32 `json:"blocks"`
		Timeoffset    int    `json:"timeoffset"`
		Connections   int32  `json:"connections"`
		Testnet       bool   `json:"testnet"`
		Keypoololdest int    `json:"keypoololdest"`
		Keypoolsize   int    `json:"keypoolsize"`
		UnlockedUntil int    `json:"unlocked_until"`
		Paytxfee      int    `json:"paytxfee"`
		Relayfee      int    `json:"relayfee"`
		Errors        string `json:"errors"`
	}{
		Version:       pact.DPOSStartVersion,
		Balance:       0,
		Blocks:        Chain.GetHeight(),
		Timeoffset:    0,
		Connections:   Server.ConnectedCount(),
		Keypoololdest: 0,
		Keypoolsize:   0,
		UnlockedUntil: 0,
		Paytxfee:      0,
		Relayfee:      0,
		Errors:        "Tobe written"}
	return ResponsePack(Success, &RetVal)
}

func AuxHelp(param Params) map[string]interface{} {
	return ResponsePack(Success, "createauxblock==submitauxblock")
}

func GetMiningInfo(param Params) map[string]interface{} {
	block, err := Chain.GetBlockByHash(Chain.GetCurrentBlockHash())
	if err != nil {
		return ResponsePack(InternalError, "get tip block failed")
	}

	miningInfo := struct {
		Blocks         uint32 `json:"blocks"`
		CurrentBlockTx uint32 `json:"currentblocktx"`
		Difficulty     string `json:"difficulty"`
		NetWorkHashPS  string `json:"networkhashps"`
		PooledTx       uint32 `json:"pooledtx"`
		Chain          string `json:"chain"`
	}{
		Blocks:         Chain.GetHeight() + 1,
		CurrentBlockTx: uint32(len(block.Transactions)),
		Difficulty:     Chain.CalcCurrentDifficulty(block.Bits),
		NetWorkHashPS:  Chain.GetNetworkHashPS().String(),
		PooledTx:       uint32(len(TxMemPool.GetTxsInPool())),
		Chain:          Config.ActiveNet,
	}

	return ResponsePack(Success, miningInfo)
}

func ToggleMining(param Params) map[string]interface{} {
	if rtn := checkRPCServiceLevel(config.ConfigurationPermitted); rtn != nil {
		return rtn
	}

	mining, ok := param.Bool("mining")
	if !ok {
		return ResponsePack(InvalidParams, "")
	}

	var message string
	if mining {
		go Pow.Start()
		message = "mining started"
	} else {
		go Pow.Halt()
		message = "mining stopped"
	}

	return ResponsePack(Success, message)
}

func DiscreteMining(param Params) map[string]interface{} {
	if rtn := checkRPCServiceLevel(config.MiningPermitted); rtn != nil {
		return rtn
	}

	if Pow == nil {
		return ResponsePack(PowServiceNotStarted, "")
	}
	count, ok := param.Uint("count")
	if !ok {
		return ResponsePack(InvalidParams, "")
	}

	ret := make([]string, 0)

	blockHashes, err := Pow.DiscreteMining(uint32(count))
	if err != nil {
		return ResponsePack(Error, err.Error())
	}

	for _, hash := range blockHashes {
		retStr := ToReversedString(*hash)
		ret = append(ret, retStr)
	}

	return ResponsePack(Success, ret)
}

func GetConnectionCount(param Params) map[string]interface{} {
	return ResponsePack(Success, Server.ConnectedCount())
}

func GetTransactionPool(param Params) map[string]interface{} {
	txs := make([]*TransactionContextInfo, 0)
	for _, tx := range TxMemPool.GetTxsInPool() {
		txs = append(txs, GetTransactionContextInfo(nil, tx))
	}
	return ResponsePack(Success, txs)
}

func GetBlockInfo(block *Block, verbose bool) BlockInfo {
	var txs []interface{}
	if verbose {
		for _, tx := range block.Transactions {
			txs = append(txs, GetTransactionContextInfo(&block.Header, tx))
		}
	} else {
		for _, tx := range block.Transactions {
			txs = append(txs, ToReversedString(tx.Hash()))
		}
	}
	var versionBytes [4]byte
	binary.BigEndian.PutUint32(versionBytes[:], block.Header.Version)

	var chainWork [4]byte
	binary.BigEndian.PutUint32(chainWork[:], Chain.GetHeight()-block.Header.Height)

	nextBlockHash, _ := Chain.GetBlockHash(block.Header.Height + 1)

	auxPow := new(bytes.Buffer)
	block.Header.AuxPow.Serialize(auxPow)

	return BlockInfo{
		Hash:              ToReversedString(block.Hash()),
		Confirmations:     Chain.GetHeight() - block.Header.Height + 1,
		StrippedSize:      uint32(block.GetSize()),
		Size:              uint32(block.GetSize()),
		Weight:            uint32(block.GetSize() * 4),
		Height:            block.Header.Height,
		Version:           block.Header.Version,
		VersionHex:        common.BytesToHexString(versionBytes[:]),
		MerkleRoot:        ToReversedString(block.Header.MerkleRoot),
		Tx:                txs,
		Time:              block.Header.Timestamp,
		MedianTime:        block.Header.Timestamp,
		Nonce:             block.Header.Nonce,
		Bits:              block.Header.Bits,
		Difficulty:        Chain.CalcCurrentDifficulty(block.Header.Bits),
		ChainWork:         common.BytesToHexString(chainWork[:]),
		PreviousBlockHash: ToReversedString(block.Header.Previous),
		NextBlockHash:     ToReversedString(nextBlockHash),
		AuxPow:            common.BytesToHexString(auxPow.Bytes()),
		MinerInfo:         string(block.Transactions[0].Payload.(*payload.CoinBase).Content[:]),
	}
}

func GetConfirmInfo(confirm *payload.Confirm) ConfirmInfo {
	votes := make([]VoteInfo, 0)
	for _, vote := range confirm.Votes {
		votes = append(votes, VoteInfo{
			Signer: common.BytesToHexString(vote.Signer),
			Accept: vote.Accept,
		})
	}

	return ConfirmInfo{
		BlockHash:  ToReversedString(confirm.Proposal.BlockHash),
		Sponsor:    common.BytesToHexString(confirm.Proposal.Sponsor),
		ViewOffset: confirm.Proposal.ViewOffset,
		Votes:      votes,
	}
}

func getBlock(hash common.Uint256, verbose uint32) (interface{}, ServerErrCode) {
	block, err := Chain.GetBlockByHash(hash)
	if err != nil {
		return "", UnknownBlock
	}
	switch verbose {
	case 0:
		w := new(bytes.Buffer)
		block.Serialize(w)
		return common.BytesToHexString(w.Bytes()), Success
	case 2:
		return GetBlockInfo(block, true), Success
	}
	return GetBlockInfo(block, false), Success
}

func getConfirm(hash common.Uint256, verbose uint32) (interface{}, ServerErrCode) {
	block, _ := Store.GetFFLDB().GetBlock(hash)
	if block == nil || !block.HaveConfirm {
		return "", UnknownBlock
	}
	if verbose == 0 {
		w := new(bytes.Buffer)
		block.Confirm.Serialize(w)
		return common.BytesToHexString(w.Bytes()), Success
	}

	return GetConfirmInfo(block.Confirm), Success
}

func GetBlockByHash(param Params) map[string]interface{} {
	str, ok := param.String("blockhash")
	if !ok {
		return ResponsePack(InvalidParams, "block hash not found")
	}

	var hash common.Uint256
	hashBytes, err := FromReversedString(str)
	if err != nil {
		return ResponsePack(InvalidParams, "invalid block hash")
	}
	if err := hash.Deserialize(bytes.NewReader(hashBytes)); err != nil {
		ResponsePack(InvalidParams, "invalid block hash")
	}

	verbosity, ok := param.Uint("verbosity")
	if !ok {
		verbosity = 1
	}

	result, error := getBlock(hash, verbosity)

	return ResponsePack(error, result)
}

func GetConfirmByHeight(param Params) map[string]interface{} {
	height, ok := param.Uint("height")
	if !ok {
		return ResponsePack(InvalidParams, "height parameter should be a positive integer")
	}

	hash, err := Chain.GetBlockHash(height)
	if err != nil {
		return ResponsePack(UnknownBlock, err.Error())
	}

	verbosity, ok := param.Uint("verbosity")
	if !ok {
		verbosity = 1
	}

	result, errCode := getConfirm(hash, verbosity)
	return ResponsePack(errCode, result)
}

func GetConfirmByHash(param Params) map[string]interface{} {
	str, ok := param.String("blockhash")
	if !ok {
		return ResponsePack(InvalidParams, "block hash not found")
	}

	var hash common.Uint256
	hashBytes, err := FromReversedString(str)
	if err != nil {
		return ResponsePack(InvalidParams, "invalid block hash")
	}
	if err := hash.Deserialize(bytes.NewReader(hashBytes)); err != nil {
		ResponsePack(InvalidParams, "invalid block hash")
	}

	verbosity, ok := param.Uint("verbosity")
	if !ok {
		verbosity = 1
	}

	result, error := getConfirm(hash, verbosity)
	return ResponsePack(error, result)
}

func SendRawTransaction(param Params) map[string]interface{} {
	if rtn := checkRPCServiceLevel(config.TransactionPermitted); rtn != nil {
		return rtn
	}

	str, ok := param.String("data")
	if !ok {
		return ResponsePack(InvalidParams, "need a string parameter named data")
	}

	bys, err := common.HexStringToBytes(str)
	if err != nil {
		return ResponsePack(InvalidParams, "hex string to bytes error")
	}
	var txn Transaction
	if err := txn.Deserialize(bytes.NewReader(bys)); err != nil {
		return ResponsePack(InvalidTransaction, err.Error())
	}

	if err := VerifyAndSendTx(&txn); err != nil {
		return ResponsePack(InvalidTransaction, err.Error())
	}

	return ResponsePack(Success, ToReversedString(txn.Hash()))
}

func SendRawTx(param Params) map[string]interface{} {

	str, ok := param.String("data")
	var rawTxs []interface{}
	var t int
	if ok {
		rawTxs = append(rawTxs, str)
		t = 1
	} else {
		rawTxs, ok = param["data"].([]interface{})
		if !ok {
			return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "not valid request format")
		}
		if !ok {
			return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "not valid request format")
		}
		t = 2
	}
	var retTxs []string
	for _, rawTx := range rawTxs {
		_, ok := rawTx.(string)
		if !ok {
			return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "not valid request format")
		}
		bys, err := common.HexStringToBytes(rawTx.(string))
		if err != nil {
			return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "hex string to bytes error")
		}
		var txn Transaction
		if err := txn.Deserialize(bytes.NewReader(bys)); err != nil {
			return ResponsePackEx(ELEPHANT_PROCESS_ERROR, err.Error())
		}

		if common2.Conf.EarnReward && !CheckTransactionReward(&txn) {
			return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "Invalid raw transaction, node reward address can not find or node reward amount not match")
		}

		if err := VerifyAndSendTx(&txn); err != nil {
			return ResponsePackEx(ELEPHANT_PROCESS_ERROR, err.Error())
		}
		retTxs = append(retTxs, ToReversedString(txn.Hash()))
	}

	if t == 1 {
		return ResponsePackEx(ELEPHANT_SUCCESS, retTxs[0])
	}
	return ResponsePackEx(ELEPHANT_SUCCESS, retTxs)
}

func CheckTransactionReward(tx *Transaction) bool {
	for _, out := range tx.Outputs {
		if addr, _ := out.ProgramHash.ToAddress(); addr == Config.PowConfiguration.PayToAddr && int64(out.Value) == int64(Config.PowConfiguration.MinTxFee-100) {
			return true
		}
	}
	return false
}

func GetBlockHeight(param Params) map[string]interface{} {
	return ResponsePack(Success, Chain.GetHeight())
}

func CurrHeight(param Params) map[string]interface{} {
	return ResponsePackEx(ELEPHANT_SUCCESS, Chain.GetHeight())
}

func GetNodeFee(param Params) map[string]interface{} {
	return ResponsePackEx(ELEPHANT_SUCCESS, config.Parameters.PowConfiguration.MinTxFee)
}

func GetBestBlockHash(param Params) map[string]interface{} {
	hash, err := Chain.GetBlockHash(Chain.GetHeight())
	if err != nil {
		return ResponsePack(InvalidParams, "")
	}
	return ResponsePack(Success, ToReversedString(hash))
}

func GetBlockCount(param Params) map[string]interface{} {
	return ResponsePack(Success, Chain.GetHeight()+1)
}

func GetBlockHash(param Params) map[string]interface{} {
	height, ok := param.Uint("height")
	if !ok {
		return ResponsePack(InvalidParams, "height parameter should be a positive integer")
	}

	hash, err := Chain.GetBlockHash(height)
	if err != nil {
		return ResponsePack(InvalidParams, "")
	}
	return ResponsePack(Success, ToReversedString(hash))
}

func GetBlockTransactions(block *Block) interface{} {
	trans := make([]string, len(block.Transactions))
	for i := 0; i < len(block.Transactions); i++ {
		trans[i] = ToReversedString(block.Transactions[i].Hash())
	}
	type BlockTransactions struct {
		Hash         string
		Height       uint32
		Transactions []string
	}
	b := BlockTransactions{
		Hash:         ToReversedString(block.Hash()),
		Height:       block.Header.Height,
		Transactions: trans,
	}
	return b
}

func GetTransactionsByHeight(param Params) map[string]interface{} {
	height, ok := param.Uint("height")
	if !ok {
		return ResponsePack(InvalidParams, "height parameter should be a positive integer")
	}

	hash, err := Chain.GetBlockHash(height)
	if err != nil {
		return ResponsePack(UnknownBlock, "")

	}
	block, err := Chain.GetBlockByHash(hash)
	if err != nil {
		return ResponsePack(UnknownBlock, "")
	}
	return ResponsePack(Success, GetBlockTransactions(block))
}

func GetBlockByHeight(param Params) map[string]interface{} {
	height, ok := param.Uint("height")
	if !ok {
		return ResponsePack(InvalidParams, "height parameter should be a positive integer")
	}

	hash, err := Chain.GetBlockHash(height)
	if err != nil {
		return ResponsePack(UnknownBlock, err.Error())
	}

	result, errCode := getBlock(hash, 2)

	return ResponsePack(errCode, result)
}

func GetArbitratorGroupByHeight(param Params) map[string]interface{} {
	height, ok := param.Uint("height")
	if !ok {
		return ResponsePack(InvalidParams, "height parameter should be a positive integer")
	}

	hash, err := Chain.GetBlockHash(height)
	if err != nil {
		return ResponsePack(UnknownBlock, "not found block hash at given height")
	}

	block, _ := Chain.GetBlockByHash(hash)
	if block == nil {
		return ResponsePack(InternalError, "not found block at given height")
	}
	crcArbiters := Arbiters.GetCRCArbiters()
	sort.Slice(crcArbiters, func(i, j int) bool {
		return bytes.Compare(crcArbiters[i].NodePublicKey, crcArbiters[j].NodePublicKey) < 0
	})

	var arbitrators []string
	for _, a := range crcArbiters {
		if !a.IsNormal {
			arbitrators = append(arbitrators, "")
		} else {
			arbitrators = append(arbitrators, common.BytesToHexString(a.NodePublicKey))
		}
	}

	result := ArbitratorGroupInfo{
		OnDutyArbitratorIndex: Arbiters.GetDutyIndexByHeight(height),
		Arbitrators:           arbitrators,
	}

	return ResponsePack(Success, result)
}

// GetAssetByHash always return ELA asset
// Deprecated: It may be removed in the next version
func GetAssetByHash(param Params) map[string]interface{} {
	asset := payload.RegisterAsset{
		Asset: payload.Asset{
			Name:      "ELA",
			Precision: config.ELAPrecision,
			AssetType: 0x00,
		},
		Amount:     0 * 100000000,
		Controller: common.Uint168{},
	}

	return ResponsePack(Success, asset)
}

func GetBalanceByAddr(param Params) map[string]interface{} {
	address, ok := param.String("addr")
	if !ok {
		return ResponsePack(InvalidParams, "")
	}

	programHash, err := common.Uint168FromAddress(address)
	if err != nil {
		return ResponsePack(InvalidParams, "invalid address, "+err.Error())
	}
	utxos, err := Store.GetFFLDB().GetUTXO(programHash)
	if err != nil {
		return ResponsePack(InvalidParams, "list unspent failed, "+err.Error())
	}
	var balance common.Fixed64 = 0
	for _, u := range utxos {
		balance = balance + u.Value
	}
	return ResponsePack(Success, balance.String())
}

func GetBalance(param Params) map[string]interface{} {
	str, ok := param.String("addr")
	if !ok {
		return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "")
	}

	programHash, err := common.Uint168FromAddress(str)
	if err != nil {
		return ResponsePackEx(InvalidParams, "invalid address, "+err.Error())
	}
	utxos, err := Store.GetFFLDB().GetUTXO(programHash)
	if err != nil {
		return ResponsePackEx(InvalidParams, "get utxo error "+err.Error())
	}
	var balance common.Fixed64 = 0
	for _, u := range utxos {
		balance = balance + u.Value
	}
	return ResponsePackEx(ELEPHANT_SUCCESS, balance.String())
}

func GetReceivedByAddress(param Params) map[string]interface{} {
	address, ok := param.String("address")
	if !ok {
		return ResponsePack(InvalidParams, "need a parameter named address")
	}
	programHash, err := common.Uint168FromAddress(address)
	if err != nil {
		return ResponsePack(InvalidParams, "invalid address, "+err.Error())
	}
	utxos, err := Store.GetFFLDB().GetUTXO(programHash)
	if err != nil {
		return ResponsePack(InvalidParams, "list unspent failed, "+err.Error())
	}
	var balance common.Fixed64 = 0
	for _, u := range utxos {
		balance = balance + u.Value
	}

	return ResponsePack(Success, balance.String())
}

func GetUTXOsByAmount(param Params) map[string]interface{} {
	if rtn := checkRPCServiceLevel(config.WalletPermitted); rtn != nil {
		return rtn
	}

	bestHeight := Chain.GetHeight()

	result := make([]UTXOInfo, 0)
	address, ok := param.String("address")
	if !ok {
		return ResponsePack(InvalidParams, "need a parameter named address!")
	}
	amountStr, ok := param.String("amount")
	if !ok {
		return ResponsePack(InvalidParams, "need a parameter named amount!")
	}
	amount, err := common.StringToFixed64(amountStr)
	if err != nil {
		return ResponsePack(InvalidParams, "invalid amount!")
	}
	programHash, err := common.Uint168FromAddress(address)
	if err != nil {
		return ResponsePack(InvalidParams, "invalid address, "+err.Error())
	}
	utxos, err := Store.GetFFLDB().GetUTXO(programHash)
	if err != nil {
		return ResponsePack(InvalidParams, "list unspent failed, "+err.Error())
	}
	utxoType := "mixed"
	if t, ok := param.String("utxotype"); ok {
		switch t {
		case "mixed", "vote", "normal", "unused":
			utxoType = t
		default:
			return ResponsePack(InvalidParams, "invalid utxotype")
		}
	}

	if utxoType == "unused" {
		var unusedUTXOs []*UTXO
		usedUTXOs := TxMemPool.GetUsedUTXOs()
		for _, u := range utxos {
			outPoint := OutPoint{TxID: u.TxID, Index: u.Index}
			referKey := outPoint.ReferKey()
			if _, ok := usedUTXOs[referKey]; !ok {
				unusedUTXOs = append(unusedUTXOs, u)
			}
		}
		utxos = unusedUTXOs
	}

	totalAmount := common.Fixed64(0)
	for _, utxo := range utxos {
		if totalAmount >= *amount {
			break
		}
		tx, height, err := Store.GetTransaction(utxo.TxID)
		if err != nil {
			return ResponsePack(InternalError, "unknown transaction "+
				utxo.TxID.String()+" from persisted utxo")
		}
		if utxoType == "vote" && (tx.Version < TxVersion09 ||
			tx.Version >= TxVersion09 && tx.Outputs[utxo.Index].Type != OTVote) {
			continue
		}
		if utxoType == "normal" && tx.Version >= TxVersion09 &&
			tx.Outputs[utxo.Index].Type == OTVote {
			continue
		}
		if tx.TxType == CoinBase && bestHeight-height < config.DefaultParams.CoinbaseMaturity {
			continue
		}
		totalAmount += utxo.Value
		result = append(result, UTXOInfo{
			TxType:        byte(tx.TxType),
			TxID:          ToReversedString(utxo.TxID),
			AssetID:       ToReversedString(config.ELAAssetID),
			VOut:          utxo.Index,
			Amount:        utxo.Value.String(),
			Address:       address,
			OutputLock:    tx.Outputs[utxo.Index].OutputLock,
			Confirmations: bestHeight - height + 1,
		})
	}

	if totalAmount < *amount {
		return ResponsePack(InternalError, "not enough utxo")
	}

	return ResponsePack(Success, result)
}

func GetAmountByInputs(param Params) map[string]interface{} {
	if rtn := checkRPCServiceLevel(config.WalletPermitted); rtn != nil {
		return rtn
	}

	inputStr, ok := param.String("inputs")
	if !ok {
		return ResponsePack(InvalidParams, "need a parameter named inputs!")
	}

	inputBytes, _ := common.HexStringToBytes(inputStr)
	r := bytes.NewReader(inputBytes)
	count, err := common.ReadVarUint(r, 0)
	if err != nil {
		return ResponsePack(InvalidParams, "invalid inputs")
	}

	amount := common.Fixed64(0)
	for i := uint64(0); i < count; i++ {
		input := new(Input)
		if err := input.Deserialize(r); err != nil {
			return ResponsePack(InvalidParams, "invalid inputs")
		}
		tx, _, err := Store.GetTransaction(input.Previous.TxID)
		if err != nil {
			return ResponsePack(InternalError, "unknown transaction "+
				input.Previous.TxID.String()+" from persisted utxo")
		}
		amount += tx.Outputs[input.Previous.Index].Value
	}

	return ResponsePack(Success, amount.String())
}

func ListUnspent(param Params) map[string]interface{} {
	if rtn := checkRPCServiceLevel(config.WalletPermitted); rtn != nil {
		return rtn
	}

	bestHeight := Chain.GetHeight()

	var result []UTXOInfo
	addresses, ok := param.ArrayString("addresses")
	if !ok {
		return ResponsePack(InvalidParams, "need addresses in an array!")
	}
	utxoType := "mixed"
	if t, ok := param.String("utxotype"); ok {
		switch t {
		case "mixed", "vote", "normal":
			utxoType = t
		default:
			return ResponsePack(InvalidParams, "invalid utxotype")
		}
	}
	for _, address := range addresses {
		programHash, err := common.Uint168FromAddress(address)
		if err != nil {
			return ResponsePack(InvalidParams, "invalid address, "+err.Error())
		}
		utxos, err := Store.GetFFLDB().GetUTXO(programHash)
		if err != nil {
			return ResponsePack(InvalidParams, "list unspent failed, "+err.Error())
		}
		for _, utxo := range utxos {
			tx, height, err := Store.GetTransaction(utxo.TxID)
			if err != nil {
				return ResponsePack(InternalError,
					"unknown transaction "+utxo.TxID.String()+" from persisted utxo")
			}
			if utxoType == "vote" && (tx.Version < TxVersion09 ||
				tx.Version >= TxVersion09 && tx.Outputs[utxo.Index].Type != OTVote) {
				continue
			}
			if utxoType == "normal" && tx.Version >= TxVersion09 && tx.Outputs[utxo.Index].Type == OTVote {
				continue
			}
			if utxo.Value == 0 {
				continue
			}
			result = append(result, UTXOInfo{
				TxType:        byte(tx.TxType),
				TxID:          ToReversedString(utxo.TxID),
				AssetID:       ToReversedString(config.ELAAssetID),
				VOut:          utxo.Index,
				Amount:        utxo.Value.String(),
				Address:       address,
				OutputLock:    tx.Outputs[utxo.Index].OutputLock,
				Confirmations: bestHeight - height + 1,
			})
		}
	}
	return ResponsePack(Success, result)
}

func CreateRawTransaction(param Params) map[string]interface{} {
	if rtn := checkRPCServiceLevel(config.WalletPermitted); rtn != nil {
		return rtn
	}

	inputsParam, ok := param.String("inputs")
	if !ok {
		return ResponsePack(InvalidParams, "need a parameter named inputs")
	}
	outputsParam, ok := param.String("outputs")
	if !ok {
		return ResponsePack(InvalidParams, "need a parameter named outputs")
	}
	locktime, ok := param.Uint("locktime")
	if !ok {
		return ResponsePack(InvalidParams, "need a parameter named locktime")
	}

	inputs := make([]string, 0)
	gjson.Parse(inputsParam).ForEach(func(key, value gjson.Result) bool {
		inputs = append(inputs, value.String())
		return true
	})

	outputs := make([]string, 0)
	gjson.Parse(outputsParam).ForEach(func(key, value gjson.Result) bool {
		outputs = append(outputs, value.String())
		return true
	})

	txInputs := make([]*Input, 0)
	for _, v := range inputs {
		txIDStr := gjson.Get(v, "txid").String()
		txIDBytes, err := common.HexStringToBytes(txIDStr)
		if err != nil {
			return ResponsePack(InvalidParams, "invalid txid when convert to bytes")
		}
		txID, err := common.Uint256FromBytes(common.BytesReverse(txIDBytes))
		if err != nil {
			return ResponsePack(InvalidParams, "invalid txid in inputs param")
		}
		input := &Input{
			Previous: OutPoint{
				TxID:  *txID,
				Index: uint16(gjson.Get(v, "vout").Int()),
			},
		}
		txInputs = append(txInputs, input)
	}

	txOutputs := make([]*Output, 0)
	for _, v := range outputs {
		amount := gjson.Get(v, "amount").String()
		value, err := common.StringToFixed64(amount)
		if err != nil {
			return ResponsePack(InvalidParams, "invalid amount in inputs param")
		}
		address := gjson.Get(v, "address").String()
		programHash, err := common.Uint168FromAddress(address)
		if err != nil {
			return ResponsePack(InvalidParams, "invalid address in outputs param")
		}
		output := &Output{
			AssetID:     *account.SystemAssetID,
			Value:       *value,
			OutputLock:  0,
			ProgramHash: *programHash,
			Type:        OTNone,
			Payload:     &outputpayload.DefaultOutput{},
		}
		txOutputs = append(txOutputs, output)
	}

	txn := &Transaction{
		Version:    TxVersion09,
		TxType:     TransferAsset,
		Payload:    &payload.TransferAsset{},
		Attributes: []*Attribute{},
		Inputs:     txInputs,
		Outputs:    txOutputs,
		Programs:   []*pg.Program{},
		LockTime:   locktime,
	}

	buf := new(bytes.Buffer)
	err := txn.Serialize(buf)
	if err != nil {
		return ResponsePack(InternalError, "txn serialize failed")
	}

	return ResponsePack(Success, common.BytesToHexString(buf.Bytes()))
}

func SignRawTransactionWithKey(param Params) map[string]interface{} {
	if rtn := checkRPCServiceLevel(config.WalletPermitted); rtn != nil {
		return rtn
	}
	dataParam, ok := param.String("data")
	if !ok {
		return ResponsePack(InvalidParams, "need a parameter named data")
	}
	codesParam, ok := param.String("codes")
	if !ok {
		return ResponsePack(InvalidParams, "need a parameter named codes")
	}
	privkeysParam, ok := param.String("privkeys")
	if !ok {
		return ResponsePack(InvalidParams, "need a parameter named privkeys")
	}

	privkeys := make([]string, 0)
	gjson.Parse(privkeysParam).ForEach(func(key, value gjson.Result) bool {
		privkeys = append(privkeys, value.String())
		return true
	})

	accounts := make(map[common.Uint160]*account.Account, 0)
	for _, privkeyStr := range privkeys {
		privkey, err := common.HexStringToBytes(privkeyStr)
		if err != nil {
			return ResponsePack(InvalidParams, err.Error())
		}
		acc, err := account.NewAccountWithPrivateKey(privkey)
		if err != nil {
			return ResponsePack(InvalidTransaction, err.Error())
		}
		accounts[acc.ProgramHash.ToCodeHash()] = acc
	}

	txBytes, err := common.HexStringToBytes(dataParam)
	if err != nil {
		return ResponsePack(InvalidParams, "hex string to bytes error")
	}
	var txn Transaction
	if err := txn.Deserialize(bytes.NewReader(txBytes)); err != nil {
		return ResponsePack(InvalidTransaction, err.Error())
	}

	codes := make([]string, 0)
	gjson.Parse(codesParam).ForEach(func(key, value gjson.Result) bool {
		codes = append(codes, value.String())
		return true
	})

	programs := make([]*pg.Program, 0)
	if len(txn.Programs) > 0 {
		programs = txn.Programs
	} else {
		for _, codeStr := range codes {
			code, err := common.HexStringToBytes(codeStr)
			if err != nil {
				return ResponsePack(InvalidParams, "invalid params codes")
			}
			program := &pg.Program{
				Code:      code,
				Parameter: nil,
			}
			programs = append(programs, program)
		}
	}

	signData := new(bytes.Buffer)
	if err := txn.SerializeUnsigned(signData); err != nil {
		return ResponsePack(InvalidTransaction, err.Error())
	}

	references, err := Chain.UTXOCache.GetTxReference(&txn)
	if err != nil {
		return ResponsePack(InvalidTransaction, err.Error())
	}

	programHashes, err := blockchain.GetTxProgramHashes(&txn, references)
	if err != nil {
		return ResponsePack(InternalError, err.Error())
	}

	if len(programs) != len(programHashes) {
		return ResponsePack(InternalError, "the number of program hashes is different with number of programs")
	}

	// sort the program hashes of owner and programs of the transaction
	common.SortProgramHashByCodeHash(programHashes)
	blockchain.SortPrograms(programs)

	for i, programHash := range programHashes {
		program := programs[i]
		codeHash := common.ToCodeHash(program.Code)
		ownerHash := programHash.ToCodeHash()
		if !codeHash.IsEqual(ownerHash) {
			return ResponsePack(InternalError, "the program hashes is different with corresponding program code")
		}

		prefixType := contract.GetPrefixType(programHash)
		if prefixType == contract.PrefixStandard {
			signedProgram, err := account.SignStandardTransaction(&txn, program, accounts)
			if err != nil {
				return ResponsePack(InternalError, err.Error())
			}
			programs[i] = signedProgram
		} else if prefixType == contract.PrefixMultiSig {
			signedProgram, err := account.SignMultiSignTransaction(&txn, program, accounts)
			if err != nil {
				return ResponsePack(InternalError, err.Error())
			}
			programs[i] = signedProgram
		} else {
			return ResponsePack(InternalError, "invalid program hash type")
		}
	}
	txn.Programs = programs

	result := new(bytes.Buffer)
	if err := txn.Serialize(result); err != nil {
		return ResponsePack(InternalError, err.Error())
	}

	return ResponsePack(Success, common.BytesToHexString(result.Bytes()))
}

func GetUnspends(param Params) map[string]interface{} {
	address, ok := param.String("addr")
	if !ok {
		return ResponsePack(InvalidParams, "")
	}

	type UTXOUnspentInfo struct {
		TxID  string `json:"Txid"`
		Index uint16 `json:"Index"`
		Value string `json:"Value"`
	}
	type Result struct {
		AssetID   string            `json:"AssetId"`
		AssetName string            `json:"AssetName"`
		UTXO      []UTXOUnspentInfo `json:"UTXO"`
	}
	var results []Result

	programHash, err := common.Uint168FromAddress(address)
	if err != nil {
		return ResponsePack(InvalidParams, "invalid address, "+err.Error())
	}
	utxos, err := Store.GetFFLDB().GetUTXO(programHash)
	if err != nil {
		return ResponsePack(InvalidParams, "list unspent failed, "+err.Error())
	}
	for _, u := range utxos {
		var unspendsInfo []UTXOUnspentInfo
		unspendsInfo = append(unspendsInfo, UTXOUnspentInfo{
			ToReversedString(u.TxID),
			u.Index,
			u.Value.String()})

		results = append(results, Result{
			ToReversedString(config.ELAAssetID),
			"ELA",
			unspendsInfo})
	}
	return ResponsePack(Success, results)
}

// Deprecated: May be removed in the next version
func GetUnspendOutput(param Params) map[string]interface{} {
	addr, ok := param.String("addr")
	if !ok {
		return ResponsePack(InvalidParams, "")
	}
	programHash, err := common.Uint168FromAddress(addr)
	if err != nil {
		return ResponsePack(InvalidParams, "")
	}

	type UTXOUnspentInfo struct {
		TxID  string `json:"Txid"`
		Index uint16 `json:"Index"`
		Value string `json:"Value"`
	}
	utxos, err := Store.GetFFLDB().GetUTXO(programHash)
	if err != nil {
		return ResponsePack(InvalidParams, "list unspent failed, "+err.Error())
	}
	var UTXOoutputs []UTXOUnspentInfo
	for _, utxo := range utxos {
		UTXOoutputs = append(UTXOoutputs, UTXOUnspentInfo{
			TxID:  ToReversedString(utxo.TxID),
			Index: utxo.Index,
			Value: utxo.Value.String()})
	}
	return ResponsePack(Success, UTXOoutputs)
}

func GetTx(param Params) map[string]interface{} {
	str, ok := param.String("hash")
	if !ok {
		return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "")
	}

	bys, err := FromReversedString(str)
	if err != nil {
		return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "")
	}

	var hash common.Uint256
	err = hash.Deserialize(bytes.NewReader(bys))
	if err != nil {
		return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "")
	}
	var header *Header
	txn, height, err := Store.GetTransaction(hash)
	if err != nil {
		txn = TxMemPool.GetTransaction(hash)
		if txn == nil {
			return ResponsePackEx(ELEPHANT_SUCCESS,
				"Unknown Transaction")
		}
	} else {
		bHash, err := Chain.GetBlockHash(height)
		if err != nil {
			return ResponsePackEx(ELEPHANT_INTERNAL_ERROR, "")
		}
		header, err = Chain.GetHeader(bHash)
		if err != nil {
			return ResponsePackEx(ELEPHANT_INTERNAL_ERROR, "")
		}
	}

	return ResponsePackEx(ELEPHANT_SUCCESS, GetTransactionContextInfo(header, txn))
}

func GetCleanTx(param Params) map[string]interface{} {
	str, ok := param.String("hash")
	if !ok {
		return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "")
	}

	bys, err := FromReversedString(str)
	if err != nil {
		return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "")
	}

	var hash common.Uint256
	err = hash.Deserialize(bytes.NewReader(bys))
	if err != nil {
		return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "")
	}
	var _status uint64 = 0
	var header *Header
	txn, height, err := Store.GetTransaction(hash)
	if err != nil {
		txn = TxMemPool.GetTransaction(hash)
		if txn == nil {
			return ResponsePackEx(ELEPHANT_SUCCESS,
				"Unknown Transaction")
		}
		_status = 1
	} else {
		bHash, err := Chain.GetBlockHash(height)
		if err != nil {
			return ResponsePackEx(ELEPHANT_INTERNAL_ERROR, "")
		}
		header, err = Chain.GetHeader(bHash)
		if err != nil {
			return ResponsePackEx(ELEPHANT_INTERNAL_ERROR, "")
		}
	}

	var _height, _timestamp uint32
	if header == nil {
		_height = 0
		_timestamp = 0
	} else {
		_height = header.Height
		_timestamp = header.Timestamp
	}

	txhs := make([]types.TransactionHistory, 0)
	var signedAddress string
	var node_fee common.Fixed64
	var node_output_index uint64 = 999999
	var memo []byte
	var tx_type = txn.TxType
	for _, attr := range txn.Attributes {
		if attr.Usage == Memo {
			memo = attr.Data
		}
		if attr.Usage == Description {
			am := make(map[string]interface{})
			err := json.Unmarshal(attr.Data, &am)
			if err == nil {
				pm, ok := am["Postmark"]
				if ok {
					dpm, ok := pm.(map[string]interface{})
					if ok {
						var orgMsg string
						for i, input := range txn.Inputs {
							hash := input.Previous.TxID
							orgMsg += common.BytesToHexString(common.BytesReverse(hash[:])) + "-" + strconv.Itoa(int(input.Previous.Index))
							if i != len(txn.Inputs)-1 {
								orgMsg += ";"
							}
						}
						orgMsg += "&"
						for i, output := range txn.Outputs {
							address, _ := output.ProgramHash.ToAddress()
							orgMsg += address + "-" + fmt.Sprintf("%d", output.Value)
							if i != len(txn.Outputs)-1 {
								orgMsg += ";"
							}
						}
						orgMsg += "&"
						orgMsg += fmt.Sprintf("%d", txn.Fee)
						log.Debugf("origin debug %s ", orgMsg)
						pub, ok_pub := dpm["pub"].(string)
						sig, ok_sig := dpm["signature"].(string)
						b_msg := []byte(orgMsg)
						b_pub, ok_b_pub := hex.DecodeString(pub)
						b_sig, ok_b_sig := hex.DecodeString(sig)
						if ok_pub && ok_sig && ok_b_pub == nil && ok_b_sig == nil {
							pubKey, err := crypto.DecodePoint(b_pub)
							if err != nil {
								log.Infof("Error deserialise pubkey from postmark data %s", hex.EncodeToString(attr.Data))
								continue
							}
							err = crypto.Verify(*pubKey, b_msg, b_sig)
							if err != nil {
								log.Infof("Error verify postmark data %s", hex.EncodeToString(attr.Data))
								continue
							}
							signedAddress, err = common2.GetAddress(b_pub)
							if err != nil {
								log.Infof("Error Getting signed address from postmark %s", hex.EncodeToString(attr.Data))
								continue
							}
						} else {
							log.Infof("Invalid postmark data %s", hex.EncodeToString(attr.Data))
							continue
						}
					} else {
						log.Infof("Invalid postmark data %s", hex.EncodeToString(attr.Data))
						continue
					}
				}
			}
		}
	}

	if tx_type == CoinBase {
		var to []common.Uint168
		hold := make(map[common.Uint168]uint64)
		txhscoinbase := make([]types.TransactionHistory, 0)
		for _, vout := range txn.Outputs {
			if !common2.ContainsU168(vout.ProgramHash, to) {
				to = append(to, vout.ProgramHash)
				txh := types.TransactionHistory{}
				txh.Address = vout.ProgramHash
				txh.Inputs = []common.Uint168{blockchain2.MINING_ADDR}
				txh.TxType = tx_type
				txh.Txid = txn.Hash()
				txh.Height = uint64(_height)
				txh.CreateTime = uint64(_timestamp)
				txh.Type = []byte(blockchain2.INCOME)
				txh.Fee = 0
				txh.Memo = memo
				txh.NodeFee = 0
				txh.NodeOutputIndex = uint64(node_output_index)
				hold[vout.ProgramHash] = uint64(vout.Value)
				txhscoinbase = append(txhscoinbase, txh)
			} else {
				hold[vout.ProgramHash] += uint64(vout.Value)
			}
		}
		for i := 0; i < len(txhscoinbase); i++ {
			txhscoinbase[i].Outputs = []common.Uint168{txhscoinbase[i].Address}
			txhscoinbase[i].Value = hold[txhscoinbase[i].Address]
		}
		txhs = append(txhs, txhscoinbase...)
	} else {
		isCrossTx := false
		if tx_type == TransferCrossChainAsset {
			isCrossTx = true
		}

		version := txn.Version
		if version == 0x09 {
			vout := txn.Outputs
			for _, v := range vout {
				if v.Type == 0x01 && v.AssetID == *blockchain2.ELA_ASSET {
					tx_type = types.Vote
				}
			}
		}
		spend := make(map[common.Uint168]int64)
		var totalInput int64 = 0
		var from []common.Uint168
		var to []common.Uint168
		for _, input := range txn.Inputs {
			txid := input.Previous.TxID
			index := input.Previous.Index
			referTx, _, _ := Store.GetTransaction(txid)
			address := referTx.Outputs[index].ProgramHash
			totalInput += int64(referTx.Outputs[index].Value)
			v, ok := spend[address]
			if ok {
				spend[address] = v + int64(referTx.Outputs[index].Value)
			} else {
				spend[address] = int64(referTx.Outputs[index].Value)
			}
			if !common2.ContainsU168(address, from) {
				from = append(from, address)
			}
		}
		receive := make(map[common.Uint168]int64)
		var totalOutput int64 = 0
		for i, output := range txn.Outputs {
			address, _ := output.ProgramHash.ToAddress()
			var valueCross int64
			if isCrossTx == true && (output.ProgramHash == blockchain2.MINING_ADDR || strings.Index(address, "X") == 0 || address == "4oLvT2") {
				switch pl := txn.Payload.(type) {
				case *payload.TransferCrossChainAsset:
					valueCross = int64(pl.CrossChainAmounts[0])
				}
			}
			if valueCross != 0 {
				totalOutput += valueCross
			} else {
				totalOutput += int64(output.Value)
			}
			v, ok := receive[output.ProgramHash]
			if ok {
				receive[output.ProgramHash] = v + int64(output.Value)
			} else {
				receive[output.ProgramHash] = int64(output.Value)
			}
			if !common2.ContainsU168(output.ProgramHash, to) {
				to = append(to, output.ProgramHash)
			}
			if signedAddress != "" {
				outputAddress, _ := output.ProgramHash.ToAddress()
				if signedAddress == outputAddress {
					node_fee = output.Value
					node_output_index = uint64(i)
				}
			}
		}
		fee := totalInput - totalOutput
		for k, r := range receive {
			transferType := blockchain2.INCOME
			s, ok := spend[k]
			var value int64
			if ok {
				if s > r {
					value = s - r
					transferType = blockchain2.SPEND
				} else {
					value = r - s
				}
				delete(spend, k)
			} else {
				value = r
			}
			var realFee uint64 = uint64(fee)
			var rto = to
			if transferType == blockchain2.INCOME {
				realFee = 0
				rto = []common.Uint168{k}
			}

			if transferType == blockchain2.SPEND {
				from = []common.Uint168{k}
			}

			txh := types.TransactionHistory{}
			txh.Value = uint64(value)
			txh.Address = k
			txh.Inputs = from
			txh.TxType = tx_type
			txh.Txid = txn.Hash()
			txh.Height = uint64(_height)
			txh.CreateTime = uint64(_timestamp)
			txh.Type = []byte(transferType)
			txh.Fee = realFee
			txh.NodeFee = uint64(node_fee)
			txh.NodeOutputIndex = uint64(node_output_index)
			if len(rto) > 10 {
				txh.Outputs = rto[0:10]
			} else {
				txh.Outputs = rto
			}
			txh.Memo = memo
			txh.Status = _status
			txhs = append(txhs, txh)
		}

		for k, r := range spend {
			txh := types.TransactionHistory{}
			txh.Value = uint64(r)
			txh.Address = k
			txh.Inputs = []common.Uint168{k}
			txh.TxType = tx_type
			txh.Txid = txn.Hash()
			txh.Height = uint64(_height)
			txh.CreateTime = uint64(_timestamp)
			txh.Type = []byte(blockchain2.SPEND)
			txh.Fee = uint64(fee)
			txh.NodeFee = uint64(node_fee)
			txh.NodeOutputIndex = uint64(node_output_index)
			if len(to) > 10 {
				txh.Outputs = to[0:10]
			} else {
				txh.Outputs = to
			}
			txh.Memo = memo
			txh.Status = _status
			txhs = append(txhs, txh)
		}
	}

	ret := make([]*types.TransactionHistoryDisplay, 0)
	for _, txh := range txhs {
		value := new(bytes.Buffer)
		txh.Serialize(value)
		dis, _ := txh.Deserialize(value)
		dis.NodeOutputIndex = nil
		dis.NodeFee = nil
		ret = append(ret, dis)
	}
	return ResponsePackEx(ELEPHANT_SUCCESS, ret)
}

//Transaction
func GetTransactionByHash(param Params) map[string]interface{} {
	str, ok := param.String("hash")
	if !ok {
		return ResponsePack(InvalidParams, "")
	}

	bys, err := FromReversedString(str)
	if err != nil {
		return ResponsePack(InvalidParams, "")
	}

	var hash common.Uint256
	err = hash.Deserialize(bytes.NewReader(bys))
	if err != nil {
		return ResponsePack(InvalidTransaction, "")
	}
	txn, height, err := Store.GetTransaction(hash)
	if err != nil {
		return ResponsePack(UnknownTransaction, "")
	}
	if false {
		w := new(bytes.Buffer)
		txn.Serialize(w)
		return ResponsePack(Success, common.BytesToHexString(w.Bytes()))
	}
	bHash, err := Chain.GetBlockHash(height)
	if err != nil {
		return ResponsePack(UnknownBlock, "")
	}
	header, err := Chain.GetHeader(bHash)
	if err != nil {
		return ResponsePack(UnknownBlock, "")
	}

	return ResponsePack(Success, GetTransactionContextInfo(header, txn))
}

func GetExistWithdrawTransactions(param Params) map[string]interface{} {
	txList, ok := param.ArrayString("txs")
	if !ok {
		return ResponsePack(InvalidParams, "txs not found")
	}

	var resultTxHashes []string
	for _, txHash := range txList {
		txHashBytes, err := common.HexStringToBytes(txHash)
		if err != nil {
			return ResponsePack(InvalidParams, "")
		}
		hash, err := common.Uint256FromBytes(txHashBytes)
		if err != nil {
			return ResponsePack(InvalidParams, "")
		}
		inStore := Store.IsSidechainTxHashDuplicate(*hash)
		inTxPool := TxMemPool.IsDuplicateSidechainTx(*hash)
		if inTxPool || inStore {
			resultTxHashes = append(resultTxHashes, txHash)
		}
	}

	return ResponsePack(Success, resultTxHashes)
}

//single producer info
type RpcProducerInfo struct {
	OwnerPublicKey string `json:"ownerpublickey"`
	NodePublicKey  string `json:"nodepublickey"`
	Nickname       string `json:"nickname"`
	Url            string `json:"url"`
	Location       uint64 `json:"location"`
	Active         bool   `json:"active"`
	Votes          string `json:"votes"`
	State          string `json:"state"`
	RegisterHeight uint32 `json:"registerheight"`
	CancelHeight   uint32 `json:"cancelheight"`
	InactiveHeight uint32 `json:"inactiveheight"`
	IllegalHeight  uint32 `json:"illegalheight"`
	Index          uint64 `json:"index"`
}

//a group producer info  include TotalVotes and producer count
type RpcProducersInfo struct {
	ProducerInfoSlice []RpcProducerInfo `json:"producers"`
	TotalVotes        string            `json:"totalvotes"`
	TotalCounts       uint64            `json:"totalcounts"`
}

//single cr candidate info
type RpcCrCandidateInfo struct {
	Code           string `json:"code"`
	CID            string `json:"cid"`
	DID            string `json:"did"`
	NickName       string `json:"nickname"`
	Url            string `json:"url"`
	Location       uint64 `json:"location"`
	State          string `json:"state"`
	Votes          string `json:"votes"`
	RegisterHeight uint32 `json:"registerheight"`
	CancelHeight   uint32 `json:"cancelheight"`

	Index uint64 `json:"index"`
}

//a group cr candidate info include TotalVotes and candidate count
type RpcCrCandidatesInfo struct {
	CRCandidateInfoSlice []RpcCrCandidateInfo `json:"crcandidatesinfo"`
	TotalVotes           string               `json:"totalvotes"`
	TotalCounts          uint64               `json:"totalcounts"`
}

type RpcSecretaryGeneral struct {
	SecretaryGeneral string `json:"secretarygeneral"`
}

//single cr member info
type RpcCrMemberInfo struct {
	Code             string `json:"code"`
	CID              string `json:"cid"`
	DID              string `json:"did"`
	NickName         string `json:"nickname"`
	Url              string `json:"url"`
	Location         uint64 `json:"location"`
	ImpeachmentVotes string `json:"impeachmentvotes"`
	DepositAmount    string `json:"depositamout"`
	DepositAddress   string `json:"depositaddress"`
	Penalty          string `json:"penalty"`
	State            string `json:"state"`
	Index            uint64 `json:"index"`
}

//a group cr member info  include cr member count
type RpcCrMembersInfo struct {
	CRMemberInfoSlice []RpcCrMemberInfo `json:"crmembersinfo"`
	TotalCounts       uint64            `json:"totalcounts"`
}

type RpcProposalBaseState struct {
	Status             string            `json:"status"`
	ProposalHash       string            `json:"proposalhash"`
	TxHash             string            `json:"txhash"`
	CRVotes            map[string]string `json:"crvotes"`
	VotersRejectAmount string            `json:"votersrejectamount"`
	RegisterHeight     uint32            `json:"registerHeight"`
	TerminatedHeight   uint32            `json:"terminatedheight"`
	TrackingCount      uint8             `json:"trackingcount"`
	ProposalOwner      string            `json:"proposalowner"`
	Index              uint64            `json:"index"`
}

type RpcCRProposalBaseStateInfo struct {
	ProposalBaseStates []RpcProposalBaseState `json:"proposalbasestates"`
	TotalCounts        uint64                 `json:"totalcounts"`
}

type RpcCRCProposal struct {
	ProposalType       string       `json:"proposaltype"`
	OwnerPublicKey     string       `json:"ownerpublickey"`
	CRCouncilMemberDID string       `json:"crcouncilmemberdid"`
	DraftHash          string       `json:"drafthash"`
	Recipient          string       `json:"recipient"`
	Budgets            []BudgetInfo `json:"budgets"`
}

type RpcProposalState struct {
	Status             string            `json:"status"`
	Proposal           RpcCRCProposal    `json:"proposal"`
	ProposalHash       string            `json:"proposalhash"`
	TxHash             string            `json:"txhash"`
	CRVotes            map[string]string `json:"crvotes"`
	VotersRejectAmount string            `json:"votersrejectamount"`
	RegisterHeight     uint32            `json:"registerheight"`
	TerminatedHeight   uint32            `json:"terminatedheight"`
	TrackingCount      uint8             `json:"trackingcount"`
	ProposalOwner      string            `json:"proposalowner"`
	AvailableAmount    string            `json:"availableamount"`
}

type RpcCRProposalStateInfo struct {
	ProposalState RpcProposalState `json:"proposalstate"`
}

func ListProducers(param Params) map[string]interface{} {
	start, _ := param.Int("start")
	limit, ok := param.Int("limit")
	if !ok {
		limit = -1
	}
	s, ok := param.String("state")
	if ok {
		s = strings.ToLower(s)
	}
	var producers []*state.Producer
	switch s {
	case "all":
		producers = Chain.GetState().GetAllProducers()
	case "pending":
		producers = Chain.GetState().GetPendingProducers()
	case "active":
		producers = Chain.GetState().GetActiveProducers()
	case "inactive":
		producers = Chain.GetState().GetInactiveProducers()
	case "canceled":
		producers = Chain.GetState().GetCanceledProducers()
	case "illegal":
		producers = Chain.GetState().GetIllegalProducers()
	case "returned":
		producers = Chain.GetState().GetReturnedDepositProducers()
	default:
		producers = Chain.GetState().GetProducers()
	}

	sort.Slice(producers, func(i, j int) bool {
		if producers[i].Votes() == producers[j].Votes() {
			return bytes.Compare(producers[i].NodePublicKey(),
				producers[j].NodePublicKey()) < 0
		}
		return producers[i].Votes() > producers[j].Votes()
	})

	var producerInfoSlice []RpcProducerInfo
	var totalVotes common.Fixed64
	for i, p := range producers {
		totalVotes += p.Votes()
		producerInfo := RpcProducerInfo{
			OwnerPublicKey: hex.EncodeToString(p.Info().OwnerPublicKey),
			NodePublicKey:  hex.EncodeToString(p.Info().NodePublicKey),
			Nickname:       p.Info().NickName,
			Url:            p.Info().Url,
			Location:       p.Info().Location,
			Active:         p.State() == state.Active,
			Votes:          p.Votes().String(),
			State:          p.State().String(),
			RegisterHeight: p.RegisterHeight(),
			CancelHeight:   p.CancelHeight(),
			InactiveHeight: p.InactiveSince(),
			IllegalHeight:  p.IllegalHeight(),
			Index:          uint64(i),
		}
		producerInfoSlice = append(producerInfoSlice, producerInfo)
	}

	count := int64(len(producers))
	if limit < 0 {
		limit = count
	}
	var rsProducerInfoSlice []RpcProducerInfo
	if start < count {
		end := start
		if start+limit <= count {
			end = start + limit
		} else {
			end = count
		}
		rsProducerInfoSlice = append(rsProducerInfoSlice, producerInfoSlice[start:end]...)
	}

	result := &RpcProducersInfo{
		ProducerInfoSlice: rsProducerInfoSlice,
		TotalVotes:        totalVotes.String(),
		TotalCounts:       uint64(count),
	}

	return ResponsePack(Success, result)
}

func GetSecretaryGeneral(param Params) map[string]interface{} {
	result := &RpcSecretaryGeneral{
		SecretaryGeneral: ChainParams.SecretaryGeneral,
	}
	return ResponsePack(Success, result)
}

//list cr candidates according to ( state , start and limit)
func ListCRCandidates(param Params) map[string]interface{} {
	start, _ := param.Int("start")
	limit, ok := param.Int("limit")
	if !ok {
		limit = -1
	}
	s, ok := param.String("state")
	if ok {
		s = strings.ToLower(s)
	}
	var candidates []*crstate.Candidate
	crCommittee := Chain.GetCRCommittee()
	switch s {
	case "all":
		candidates = crCommittee.GetAllCandidates()
	case "pending":
		candidates = crCommittee.GetCandidates(crstate.Pending)
	case "active":
		candidates = crCommittee.GetCandidates(crstate.Active)
	case "canceled":
		candidates = crCommittee.GetCandidates(crstate.Canceled)
	case "returned":
		candidates = crCommittee.GetCandidates(crstate.Returned)
	default:
		candidates = crCommittee.GetCandidates(crstate.Pending)
		candidates = append(candidates, crCommittee.GetCandidates(crstate.Active)...)
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Votes() == candidates[j].Votes() {
			iCRInfo := candidates[i].Info()
			jCRInfo := candidates[j].Info()
			return iCRInfo.GetCodeHash().Compare(jCRInfo.GetCodeHash()) < 0
		}
		return candidates[i].Votes() > candidates[j].Votes()
	})

	var candidateInfoSlice []RpcCrCandidateInfo
	var totalVotes common.Fixed64
	for i, c := range candidates {
		totalVotes += c.Votes()
		cidAddress, _ := c.Info().CID.ToAddress()
		var didAddress string
		if !c.Info().DID.IsEqual(emptyHash) {
			didAddress, _ = c.Info().DID.ToAddress()
		}
		candidateInfo := RpcCrCandidateInfo{
			Code:           hex.EncodeToString(c.Info().Code),
			CID:            cidAddress,
			DID:            didAddress,
			NickName:       c.Info().NickName,
			Url:            c.Info().Url,
			Location:       c.Info().Location,
			State:          c.State().String(),
			Votes:          c.Votes().String(),
			RegisterHeight: c.RegisterHeight(),
			CancelHeight:   c.CancelHeight(),
			Index:          uint64(i),
		}
		candidateInfoSlice = append(candidateInfoSlice, candidateInfo)
	}

	count := int64(len(candidates))
	if limit < 0 {
		limit = count
	}
	var rSCandidateInfoSlice []RpcCrCandidateInfo
	if start < count {
		end := start
		if start+limit <= count {
			end = start + limit
		} else {
			end = count
		}
		rSCandidateInfoSlice = append(rSCandidateInfoSlice, candidateInfoSlice[start:end]...)
	}

	result := &RpcCrCandidatesInfo{
		CRCandidateInfoSlice: rSCandidateInfoSlice,
		TotalVotes:           totalVotes.String(),
		TotalCounts:          uint64(count),
	}

	return ResponsePack(Success, result)
}

//list current crs according to (state)
func ListCurrentCRs(param Params) map[string]interface{} {
	cm := Chain.GetCRCommittee()
	var crMembers []*crstate.CRMember
	if cm.IsInElectionPeriod() {
		crMembers = cm.GetAllMembers()
		sort.Slice(crMembers, func(i, j int) bool {
			return crMembers[i].Info.GetCodeHash().Compare(
				crMembers[j].Info.GetCodeHash()) < 0
		})
	}

	var rsCRMemberInfoSlice []RpcCrMemberInfo
	for i, cr := range crMembers {
		cidAddress, _ := cr.Info.CID.ToAddress()
		var didAddress string
		if !cr.Info.DID.IsEqual(emptyHash) {
			didAddress, _ = cr.Info.DID.ToAddress()
		}
		depositAddr, _ := cr.DepositHash.ToAddress()
		memberInfo := RpcCrMemberInfo{
			Code:             hex.EncodeToString(cr.Info.Code),
			CID:              cidAddress,
			DID:              didAddress,
			NickName:         cr.Info.NickName,
			Url:              cr.Info.Url,
			Location:         cr.Info.Location,
			ImpeachmentVotes: cr.ImpeachmentVotes.String(),
			DepositAmount:    cm.GetAvailableDepositAmount(cr.Info.CID).String(),
			DepositAddress:   depositAddr,
			Penalty:          cm.GetPenalty(cr.Info.CID).String(),
			Index:            uint64(i),
			State:            cr.MemberState.String(),
		}
		rsCRMemberInfoSlice = append(rsCRMemberInfoSlice, memberInfo)
	}

	count := int64(len(crMembers))

	result := &RpcCrMembersInfo{
		CRMemberInfoSlice: rsCRMemberInfoSlice,
		TotalCounts:       uint64(count),
	}

	return ResponsePack(Success, result)
}

func ListCRProposalBaseState(param Params) map[string]interface{} {
	start, _ := param.Int("start")
	limit, ok := param.Int("limit")
	if !ok {
		limit = -1
	}
	s, ok := param.String("state")
	if ok {
		s = strings.ToLower(s)
	}
	var proposalMap crstate.ProposalsMap
	crCommittee := Chain.GetCRCommittee()
	switch s {
	case "all":
		proposalMap = crCommittee.GetAllProposals()
	case "registered":
		proposalMap = crCommittee.GetProposals(crstate.Registered)
	case "cragreed":
		proposalMap = crCommittee.GetProposals(crstate.CRAgreed)
	case "voteragreed":
		proposalMap = crCommittee.GetProposals(crstate.VoterAgreed)
	case "finished":
		proposalMap = crCommittee.GetProposals(crstate.Finished)
	case "crcanceled":
		proposalMap = crCommittee.GetProposals(crstate.CRCanceled)
	case "votercanceled":
		proposalMap = crCommittee.GetProposals(crstate.VoterCanceled)
	case "aborted":
		proposalMap = crCommittee.GetProposals(crstate.Aborted)
	case "terminated":
		proposalMap = crCommittee.GetProposals(crstate.Terminated)
	default:
		return ResponsePack(InvalidParams, "invalidate state")
	}

	var crVotes map[string]string
	var RpcProposalBaseStates []RpcProposalBaseState

	var index uint64
	for _, proposal := range proposalMap {
		crVotes = make(map[string]string)
		for k, v := range proposal.CRVotes {
			did, _ := k.ToAddress()
			crVotes[did] = v.Name()
		}
		RpcProposalBaseState := RpcProposalBaseState{
			Status:             proposal.Status.String(),
			ProposalHash:       ToReversedString(proposal.Proposal.Hash),
			TxHash:             ToReversedString(proposal.TxHash),
			CRVotes:            crVotes,
			VotersRejectAmount: proposal.VotersRejectAmount.String(),
			RegisterHeight:     proposal.RegisterHeight,
			TrackingCount:      proposal.TrackingCount,
			TerminatedHeight:   proposal.TerminatedHeight,
			ProposalOwner:      hex.EncodeToString(proposal.ProposalOwner),
			Index:              index,
		}
		RpcProposalBaseStates = append(RpcProposalBaseStates, RpcProposalBaseState)
		index++
	}
	sort.Slice(RpcProposalBaseStates, func(i, j int) bool {
		return RpcProposalBaseStates[i].
			ProposalHash < RpcProposalBaseStates[j].ProposalHash
	})

	for k := range RpcProposalBaseStates {
		RpcProposalBaseStates[k].Index = uint64(k)
	}

	count := int64(len(RpcProposalBaseStates))
	if limit < 0 {
		limit = count
	}
	var rSRpcProposalBaseStates []RpcProposalBaseState
	if start < count {
		end := start
		if start+limit <= count {
			end = start + limit
		} else {
			end = count
		}
		rSRpcProposalBaseStates = append(rSRpcProposalBaseStates, RpcProposalBaseStates[start:end]...)
	}

	result := &RpcCRProposalBaseStateInfo{
		ProposalBaseStates: rSRpcProposalBaseStates,
		TotalCounts:        uint64(count),
	}

	return ResponsePack(Success, result)
}

func GetCRProposalState(param Params) map[string]interface{} {
	var proposalState *crstate.ProposalState
	crCommittee := Chain.GetCRCommittee()
	ProposalHashHexStr, ok := param.String("proposalhash")
	if ok {
		proposalHashBytes, err := FromReversedString(ProposalHashHexStr)
		if err != nil {
			return ResponsePack(InvalidParams, "invalidate proposalhash")
		}
		ProposalHash, err := common.Uint256FromBytes(proposalHashBytes)
		if err != nil {
			return ResponsePack(InvalidParams, "invalidate proposalhash")
		}
		proposalState = crCommittee.GetProposal(*ProposalHash)
		if proposalState == nil {
			return ResponsePack(InvalidParams, "proposalhash not exist")
		}

	} else {
		DraftHashStr, ok := param.String("drafthash")
		if !ok {
			return ResponsePack(InvalidParams, "params at least one of proposalhash and DraftHash")
		}
		DraftHashStrBytes, err := FromReversedString(DraftHashStr)
		if err != nil {
			return ResponsePack(InvalidParams, "invalidate drafthash")
		}
		DraftHash, err := common.Uint256FromBytes(DraftHashStrBytes)
		if err != nil {
			return ResponsePack(InvalidParams, "invalidate drafthash")
		}
		proposalState = crCommittee.GetProposalByDraftHash(*DraftHash)
		if proposalState == nil {
			return ResponsePack(InvalidParams, "DraftHash not exist")
		}
	}

	var rpcProposal RpcCRCProposal
	proposalHash := proposalState.Proposal.Hash

	did, _ := proposalState.Proposal.CRCouncilMemberDID.ToAddress()
	rpcProposal.CRCouncilMemberDID = did
	rpcProposal.DraftHash = ToReversedString(proposalState.Proposal.DraftHash)
	rpcProposal.ProposalType = proposalState.Proposal.ProposalType.Name()
	rpcProposal.OwnerPublicKey = common.BytesToHexString(proposalState.Proposal.OwnerPublicKey)
	rpcProposal.Budgets = make([]BudgetInfo, 0)
	for _, b := range proposalState.Proposal.Budgets {
		budgetStatus := proposalState.BudgetsStatus[b.Stage]
		rpcProposal.Budgets = append(rpcProposal.Budgets, BudgetInfo{
			Type:   b.Type.Name(),
			Stage:  b.Stage,
			Amount: b.Amount.String(),
			Status: budgetStatus.Name(),
		})
	}

	var err error
	rpcProposal.Recipient, err = proposalState.Proposal.Recipient.ToAddress()
	if err != nil {
		return ResponsePack(InternalError, "invalidate Recipient")
	}
	crVotes := make(map[string]string)
	for k, v := range proposalState.CRVotes {
		did, _ := k.ToAddress()
		crVotes[did] = v.Name()
	}
	RpcProposalState := RpcProposalState{
		Status:             proposalState.Status.String(),
		Proposal:           rpcProposal,
		ProposalHash:       ToReversedString(proposalHash),
		TxHash:             ToReversedString(proposalState.TxHash),
		CRVotes:            crVotes,
		VotersRejectAmount: proposalState.VotersRejectAmount.String(),
		RegisterHeight:     proposalState.RegisterHeight,
		TrackingCount:      proposalState.TrackingCount,
		TerminatedHeight:   proposalState.TerminatedHeight,
		ProposalOwner:      hex.EncodeToString(proposalState.ProposalOwner),
		AvailableAmount:    crCommittee.AvailableWithdrawalAmount(proposalHash).String(),
	}
	result := &RpcCRProposalStateInfo{ProposalState: RpcProposalState}
	return ResponsePack(Success, result)
}

func ProducerStatus(param Params) map[string]interface{} {
	publicKey, ok := param.String("publickey")
	if !ok {
		return ResponsePack(InvalidParams, "public key not found")
	}
	publicKeyBytes, err := common.HexStringToBytes(publicKey)
	if err != nil {
		return ResponsePack(InvalidParams, "invalid public key")
	}
	if _, err = contract.PublicKeyToStandardProgramHash(publicKeyBytes); err != nil {
		return ResponsePack(InvalidParams, "invalid public key bytes")
	}
	producer := Chain.GetState().GetProducer(publicKeyBytes)
	if producer == nil {
		return ResponsePack(InvalidParams, "unknown producer public key")
	}
	return ResponsePack(Success, producer.State().String())
}

func VoteStatus(param Params) map[string]interface{} {
	address, ok := param.String("address")
	if !ok {
		return ResponsePack(InvalidParams, "address not found")
	}

	programHash, err := common.Uint168FromAddress(address)
	if err != nil {
		return ResponsePack(InvalidParams, "Invalid address: "+address)
	}
	utxos, err := Store.GetFFLDB().GetUTXO(programHash)
	if err != nil {
		return ResponsePack(InvalidParams, "list unspent failed, "+err.Error())
	}
	var total common.Fixed64
	var voting common.Fixed64
	for _, utxo := range utxos {
		tx, _, err := Store.GetTransaction(utxo.TxID)
		if err != nil {
			return ResponsePack(InternalError, "unknown transaction "+utxo.TxID.String()+" from persisted utxo")
		}
		if tx.Outputs[utxo.Index].Type == OTVote {
			voting += utxo.Value
		}
		total += utxo.Value
	}

	pending := false
	for _, t := range TxMemPool.GetTxsInPool() {
		for _, i := range t.Inputs {
			tx, _, err := Store.GetTransaction(i.Previous.TxID)
			if err != nil {
				return ResponsePack(InternalError, "unknown transaction "+i.Previous.TxID.String()+" from persisted utxo")
			}
			if tx.Outputs[i.Previous.Index].ProgramHash.IsEqual(*programHash) {
				pending = true
			}
		}
		for _, o := range t.Outputs {
			if o.Type == OTVote && o.ProgramHash.IsEqual(*programHash) {
				pending = true
			}
		}
		if pending {
			break
		}
	}

	type voteInfo struct {
		Total   string `json:"total"`
		Voting  string `json:"voting"`
		Pending bool   `json:"pending"`
	}
	return ResponsePack(Success, &voteInfo{
		Total:   total.String(),
		Voting:  voting.String(),
		Pending: pending,
	})
}

func GetDepositCoin(param Params) map[string]interface{} {
	pk, ok := param.String("ownerpublickey")
	if !ok {
		return ResponsePack(InvalidParams, "need a param called ownerpublickey")
	}
	pkBytes, err := hex.DecodeString(pk)
	if err != nil {
		return ResponsePack(InvalidParams, "invalid publickey")
	}
	programHash, err := contract.PublicKeyToDepositProgramHash(pkBytes)
	if err != nil {
		return ResponsePack(InvalidParams, "invalid publickey to programHash")
	}
	utxos, err := Store.GetFFLDB().GetUTXO(programHash)
	if err != nil {
		return ResponsePack(InvalidParams, "list unspent failed, "+err.Error())
	}
	var balance common.Fixed64 = 0
	for _, utxo := range utxos {
		balance = balance + utxo.Value
	}
	var deducted common.Fixed64 = 0
	//todo get deducted coin

	type depositCoin struct {
		Available string `json:"available"`
		Deducted  string `json:"deducted"`
	}
	return ResponsePack(Success, &depositCoin{
		Available: balance.String(),
		Deducted:  deducted.String(),
	})
}

func GetCRDepositCoin(param Params) map[string]interface{} {
	crCommittee := Chain.GetCRCommittee()
	var availableDepositAmount, penaltyAmount, depositAmount, totalAmount common.Fixed64
	pubkey, hasPubkey := param.String("publickey")
	if hasPubkey {
		available, penalty, deposit, total, err := crCommittee.GetDepositAmountByPublicKey(pubkey)
		if err != nil {
			return ResponsePack(InvalidParams, err.Error())
		}
		availableDepositAmount = available
		penaltyAmount = penalty
		depositAmount = deposit
		totalAmount = total
	}
	id, hasID := param.String("id")
	if hasID {
		programHash, err := common.Uint168FromAddress(id)
		if err != nil {
			return ResponsePack(InvalidParams, "invalid id to programHash")
		}
		available, penalty, deposit, total, err := crCommittee.GetDepositAmountByID(*programHash)
		if err != nil {
			return ResponsePack(InvalidParams, err.Error())
		}
		availableDepositAmount = available
		penaltyAmount = penalty
		depositAmount = deposit
		totalAmount = total
	}

	if !hasPubkey && !hasID {
		return ResponsePack(InvalidParams, "need a param called "+
			"publickey or id")
	}

	type depositCoin struct {
		Available string `json:"available"`
		Deducted  string `json:"deducted"`
		Deposit   string `json:"deposit"`
		Assets    string `json:"assets"`
	}
	return ResponsePack(Success, &depositCoin{
		Available: availableDepositAmount.String(),
		Deducted:  penaltyAmount.String(),
		Deposit:   depositAmount.String(),
		Assets:    totalAmount.String(),
	})
}

func EstimateSmartFee(param Params) map[string]interface{} {
	if rtn := checkRPCServiceLevel(config.TransactionPermitted); rtn != nil {
		return rtn
	}

	confirm, ok := param.Int("confirmations")
	if !ok {
		return ResponsePack(InvalidParams, "need a param called confirmations")
	}
	if confirm > 25 {
		return ResponsePack(InvalidParams, "support only 25 confirmations at most")
	}
	var FeeRate = 10000 //basic fee rate 10000 sela per KB
	var count = 0

	// TODO just return fixed transaction fee for now, we didn't have that much
	// transactions in a block yet.

	return ResponsePack(Success, GetFeeRate(count, int(confirm))*FeeRate)
}

func GetFeeRate(count int, confirm int) int {
	gap := count - confirm
	if gap < 0 {
		gap = -1
	}
	return gap + 2
}

func DecodeRawTransaction(param Params) map[string]interface{} {
	if rtn := checkRPCServiceLevel(config.WalletPermitted); rtn != nil {
		return rtn
	}

	dataParam, ok := param.String("data")
	if !ok {
		return ResponsePack(InvalidParams, "need a parameter named data")
	}
	txBytes, err := common.HexStringToBytes(dataParam)
	if err != nil {
		return ResponsePack(InvalidParams, "invalid raw tx data, "+err.Error())
	}
	var txn Transaction
	if err := txn.Deserialize(bytes.NewReader(txBytes)); err != nil {
		return ResponsePack(InvalidParams, "invalid raw tx data, "+err.Error())
	}

	return ResponsePack(Success, GetTransactionInfo(&txn))
}

func getPayloadInfo(p Payload, payloadVersion byte) PayloadInfo {
	switch object := p.(type) {
	case *payload.CoinBase:
		obj := new(CoinbaseInfo)
		obj.CoinbaseData = string(object.Content)
		return obj
	case *payload.RegisterAsset:
		obj := new(RegisterAssetInfo)
		obj.Asset = object.Asset
		obj.Amount = object.Amount.String()
		obj.Controller = common.BytesToHexString(common.BytesReverse(object.Controller.Bytes()))
		return obj
	case *payload.SideChainPow:
		obj := new(SideChainPowInfo)
		obj.BlockHeight = object.BlockHeight
		obj.SideBlockHash = object.SideBlockHash.String()
		obj.SideGenesisHash = object.SideGenesisHash.String()
		obj.Signature = common.BytesToHexString(object.Signature)
		return obj
	case *payload.WithdrawFromSideChain:
		obj := new(WithdrawFromSideChainInfo)
		obj.BlockHeight = object.BlockHeight
		obj.GenesisBlockAddress = object.GenesisBlockAddress
		for _, hash := range object.SideChainTransactionHashes {
			obj.SideChainTransactionHashes = append(obj.SideChainTransactionHashes, hash.String())
		}
		return obj
	case *payload.TransferCrossChainAsset:
		obj := new(TransferCrossChainAssetInfo)
		obj.CrossChainAddresses = object.CrossChainAddresses
		obj.OutputIndexes = object.OutputIndexes
		obj.CrossChainAmounts = object.CrossChainAmounts
		return obj
	case *payload.TransferAsset:
	case *payload.Record:
	case *payload.ProducerInfo:
		obj := new(ProducerInfo)
		obj.OwnerPublicKey = common.BytesToHexString(object.OwnerPublicKey)
		obj.NodePublicKey = common.BytesToHexString(object.NodePublicKey)
		obj.NickName = object.NickName
		obj.Url = object.Url
		obj.Location = object.Location
		obj.NetAddress = object.NetAddress
		obj.Signature = common.BytesToHexString(object.Signature)
		return obj
	case *payload.ProcessProducer:
		obj := new(CancelProducerInfo)
		obj.OwnerPublicKey = common.BytesToHexString(object.OwnerPublicKey)
		obj.Signature = common.BytesToHexString(object.Signature)
		return obj
	case *payload.InactiveArbitrators:
		var arbitrators []string
		for _, a := range object.Arbitrators {
			arbitrators = append(arbitrators, common.BytesToHexString(a))
		}
		obj := new(InactiveArbitratorsInfo)
		obj.Sponsor = common.BytesToHexString(object.Sponsor)
		obj.Arbitrators = arbitrators
		return obj
	case *payload.RevertToDPOS:
		obj := new(RevertToDPOSInfo)
		obj.WorkHeightInterval = object.WorkHeightInterval
		obj.RevertToPOWBlockHeight = object.RevertToPOWBlockHeight
		return obj
	case *payload.RevertToPOW:
		obj := new(RevertToPOWInfo)
		obj.Type = object.Type.String()
		obj.WorkingHeight = object.WorkingHeight
		return obj
	case *payload.ActivateProducer:
		obj := new(ActivateProducerInfo)
		obj.NodePublicKey = common.BytesToHexString(object.NodePublicKey)
		obj.Signature = common.BytesToHexString(object.Signature)
		return obj
	case *payload.UpdateVersion:
		obj := new(UpdateVersionInfo)
		obj.StartHeight = object.StartHeight
		obj.EndHeight = object.EndHeight
		return obj
	case *payload.CRInfo:
		obj := new(CRInfo)
		obj.Code = common.BytesToHexString(object.Code)
		cid, _ := object.CID.ToAddress()
		obj.CID = cid
		did, _ := object.DID.ToAddress()
		if object.DID.IsEqual(emptyHash) {
			obj.DID = ""
		} else {
			obj.DID = did
		}
		obj.NickName = object.NickName
		obj.Url = object.Url
		obj.Location = object.Location
		obj.Signature = common.BytesToHexString(object.Signature)
		return obj
	case *payload.UnregisterCR:
		obj := new(UnregisterCRInfo)
		cid, _ := object.CID.ToAddress()
		obj.CID = cid
		obj.Signature = common.BytesToHexString(object.Signature)
		return obj
	case *payload.CRCProposal:

		switch object.ProposalType {
		case payload.Normal, payload.ELIP:
			var budgets []BudgetBaseInfo
			for _, b := range object.Budgets {
				budgets = append(budgets, BudgetBaseInfo{
					Type:   b.Type.Name(),
					Stage:  b.Stage,
					Amount: b.Amount.String(),
				})
			}
			obj := new(CRCProposalInfo)
			obj.ProposalType = object.ProposalType.Name()
			obj.CategoryData = object.CategoryData
			obj.OwnerPublicKey = common.BytesToHexString(object.OwnerPublicKey)
			obj.DraftHash = common.ToReversedString(object.DraftHash)
			obj.Budgets = budgets
			addr, _ := object.Recipient.ToAddress()
			obj.Recipient = addr
			obj.Signature = common.BytesToHexString(object.Signature)
			crmdid, _ := object.CRCouncilMemberDID.ToAddress()
			obj.CRCouncilMemberDID = crmdid
			obj.CRCouncilMemberSignature = common.BytesToHexString(object.CRCouncilMemberSignature)
			obj.Hash = common.ToReversedString(object.Hash(payloadVersion))
			return obj

		case payload.ChangeProposalOwner:
			obj := new(CRCChangeProposalOwnerInfo)
			obj.ProposalType = object.ProposalType.Name()
			obj.CategoryData = object.CategoryData
			obj.OwnerPublicKey = common.BytesToHexString(object.OwnerPublicKey)
			obj.DraftHash = common.ToReversedString(object.DraftHash)
			obj.TargetProposalHash = common.ToReversedString(object.TargetProposalHash)
			addr, _ := object.NewRecipient.ToAddress()
			obj.NewRecipient = addr
			obj.NewOwnerPublicKey = common.BytesToHexString(object.NewOwnerPublicKey)
			obj.Signature = common.BytesToHexString(object.Signature)
			obj.NewOwnerSignature = common.BytesToHexString(object.NewOwnerSignature)
			crmdid, _ := object.CRCouncilMemberDID.ToAddress()
			obj.CRCouncilMemberDID = crmdid
			obj.CRCouncilMemberSignature = common.BytesToHexString(object.CRCouncilMemberSignature)
			obj.Hash = common.ToReversedString(object.Hash(payloadVersion))
			return obj

		case payload.CloseProposal:
			obj := new(CRCCloseProposalInfo)
			obj.ProposalType = object.ProposalType.Name()
			obj.CategoryData = object.CategoryData
			obj.OwnerPublicKey = common.BytesToHexString(object.OwnerPublicKey)
			obj.DraftHash = common.ToReversedString(object.DraftHash)
			obj.TargetProposalHash = common.ToReversedString(object.TargetProposalHash)
			obj.Signature = common.BytesToHexString(object.Signature)
			crmdid, _ := object.CRCouncilMemberDID.ToAddress()
			obj.CRCouncilMemberDID = crmdid
			obj.CRCouncilMemberSignature = common.BytesToHexString(object.CRCouncilMemberSignature)
			obj.Hash = common.ToReversedString(object.Hash(payloadVersion))
			return obj

		case payload.ReserveCustomID:
			obj := new(CRCReservedCustomIDProposalInfo)
			obj.ProposalType = object.ProposalType.Name()
			obj.CategoryData = object.CategoryData
			obj.OwnerPublicKey = common.BytesToHexString(object.OwnerPublicKey)
			obj.DraftHash = common.ToReversedString(object.DraftHash)
			obj.ReservedCustomIDList = object.ReservedCustomIDList
			obj.Signature = common.BytesToHexString(object.Signature)
			crmdid, _ := object.CRCouncilMemberDID.ToAddress()
			obj.CRCouncilMemberDID = crmdid
			obj.CRCouncilMemberSignature = common.BytesToHexString(object.CRCouncilMemberSignature)
			obj.Hash = common.ToReversedString(object.Hash(payloadVersion))
			return obj

		case payload.ReceiveCustomID:
			obj := new(CRCReceivedCustomIDProposalInfo)
			obj.ProposalType = object.ProposalType.Name()
			obj.CategoryData = object.CategoryData
			obj.OwnerPublicKey = common.BytesToHexString(object.OwnerPublicKey)
			obj.DraftHash = common.ToReversedString(object.DraftHash)
			obj.ReceiveCustomIDList = object.ReceivedCustomIDList
			obj.ReceiverDID, _ = object.ReceiverDID.ToAddress()
			obj.Signature = common.BytesToHexString(object.Signature)
			crmdid, _ := object.CRCouncilMemberDID.ToAddress()
			obj.CRCouncilMemberDID = crmdid
			obj.CRCouncilMemberSignature = common.BytesToHexString(object.CRCouncilMemberSignature)
			obj.Hash = common.ToReversedString(object.Hash(payloadVersion))
			return obj

		case payload.ChangeCustomIDFee:
			obj := new(CRCChangeCustomIDFeeInfo)
			obj.ProposalType = object.ProposalType.Name()
			obj.CategoryData = object.CategoryData
			obj.OwnerPublicKey = common.BytesToHexString(object.OwnerPublicKey)
			obj.DraftHash = common.ToReversedString(object.DraftHash)
			obj.FeeRate = int64(object.RateOfCustomIDFee)
			obj.Signature = common.BytesToHexString(object.Signature)
			crmdid, _ := object.CRCouncilMemberDID.ToAddress()
			obj.CRCouncilMemberDID = crmdid
			obj.CRCouncilMemberSignature = common.BytesToHexString(object.CRCouncilMemberSignature)
			obj.Hash = common.ToReversedString(object.Hash(payloadVersion))
			return obj

		case payload.SecretaryGeneral:
			obj := new(CRCSecretaryGeneralProposalInfo)
			obj.ProposalType = object.ProposalType.Name()
			obj.CategoryData = object.CategoryData
			obj.OwnerPublicKey = common.BytesToHexString(object.OwnerPublicKey)
			obj.DraftHash = common.ToReversedString(object.DraftHash)
			obj.SecretaryGeneralPublicKey = common.BytesToHexString(object.SecretaryGeneralPublicKey)
			sgDID, _ := object.SecretaryGeneralDID.ToAddress()
			obj.SecretaryGeneralDID = sgDID
			obj.Signature = common.BytesToHexString(object.Signature)
			obj.SecretaryGeneraSignature = common.BytesToHexString(object.SecretaryGeneraSignature)
			crmdid, _ := object.CRCouncilMemberDID.ToAddress()
			obj.CRCouncilMemberDID = crmdid
			obj.CRCouncilMemberSignature = common.BytesToHexString(object.CRCouncilMemberSignature)
			obj.Hash = common.ToReversedString(object.Hash(payloadVersion))
			return obj
		}

	case *payload.CustomIDProposalResult:
		obj := new(CRCCustomIDProposalResultInfo)
		for _, r := range object.ProposalResults {
			result := ProposalResultInfo{
				ProposalHash: common.ToReversedString(r.ProposalHash),
				ProposalType: r.ProposalType.Name(),
				Result:       r.Result,
			}
			obj.ProposalResults = append(obj.ProposalResults, result)
		}

		return obj

	case *payload.CRCProposalReview:
		obj := new(CRCProposalReviewInfo)
		obj.ProposalHash = common.ToReversedString(object.ProposalHash)
		obj.VoteResult = object.VoteResult.Name()
		obj.OpinionHash = common.ToReversedString(object.OpinionHash)
		did, _ := object.DID.ToAddress()
		obj.DID = did
		obj.Sign = common.BytesToHexString(object.Signature)
		return obj
	case *payload.CRCProposalTracking:
		obj := new(CRCProposalTrackingInfo)
		obj.ProposalTrackingType = object.ProposalTrackingType.Name()
		obj.ProposalHash = common.ToReversedString(object.ProposalHash)
		obj.MessageHash = common.ToReversedString(object.MessageHash)
		obj.Stage = object.Stage
		obj.OwnerPublicKey = common.BytesToHexString(object.OwnerPublicKey)
		obj.NewOwnerPublicKey = common.BytesToHexString(object.NewOwnerPublicKey)
		obj.OwnerSignature = common.BytesToHexString(object.OwnerSignature)
		obj.NewOwnerPublicKey = common.BytesToHexString(object.NewOwnerPublicKey)
		obj.SecretaryGeneralOpinionHash = common.ToReversedString(object.SecretaryGeneralOpinionHash)
		obj.SecretaryGeneralSignature = common.BytesToHexString(object.SecretaryGeneralSignature)
		obj.NewOwnerSignature = common.BytesToHexString(object.NewOwnerSignature)
		return obj
	case *payload.CRCProposalWithdraw:
		obj := new(CRCProposalWithdrawInfo)
		obj.ProposalHash = common.ToReversedString(object.ProposalHash)
		obj.OwnerPublicKey = common.BytesToHexString(object.OwnerPublicKey)
		if payloadVersion == payload.CRCProposalWithdrawVersion01 {
			recipient, err := object.Recipient.ToAddress()
			if err == nil {
				obj.Recipient = recipient
			}
			obj.Amount = object.Amount.String()
		}
		obj.Signature = common.BytesToHexString(object.Signature)
		return obj
	case *payload.CRCouncilMemberClaimNode:
		obj := new(CRCouncilMemberClaimNodeInfo)
		obj.NodePublicKey = common.BytesToHexString(object.NodePublicKey)
		obj.CRCouncilMemberDID, _ = object.CRCouncilCommitteeDID.ToAddress()
		obj.CRCouncilMemberSignature = common.BytesToHexString(object.CRCouncilCommitteeSignature)
		return obj
	case *payload.NextTurnDPOSInfo:
		obj := new(NextTurnDPOSPayloadInfo)
		crPublicKeysString := make([]string, 0)
		dposPublicKeysString := make([]string, 0)
		for _, v := range object.CRPublicKeys {
			crPublicKeysString = append(crPublicKeysString, common.BytesToHexString(v))
		}
		for _, v := range object.DPOSPublicKeys {
			dposPublicKeysString = append(dposPublicKeysString, common.BytesToHexString(v))
		}
		obj.WorkingHeight = object.WorkingHeight
		obj.CRPublickeys = crPublicKeysString
		obj.DPOSPublicKeys = dposPublicKeysString
		return obj
	case *payload.CRCProposalRealWithdraw:
		obj := new(CRCProposalRealWithdrawInfo)
		obj.WithdrawTransactionHashes = make([]string, 0)
		for _, hash := range object.WithdrawTransactionHashes {
			obj.WithdrawTransactionHashes =
				append(obj.WithdrawTransactionHashes, common.ToReversedString(hash))
		}
		return obj
	case *payload.DPOSIllegalProposals:
		obj := new(DPOSIllegalProposalsInfo)
		obj.Hash = common.ToReversedString(object.Hash())
		obj.Evidence = ProposalEvidenceInfo{
			Proposal: DPOSProposalInfo{
				Sponsor:    common.BytesToHexString(object.Evidence.Proposal.Sponsor),
				BlockHash:  common.ToReversedString(object.Evidence.Proposal.BlockHash),
				ViewOffset: object.Evidence.Proposal.ViewOffset,
				Sign:       common.BytesToHexString(object.Evidence.Proposal.Sign),
				Hash:       common.ToReversedString(object.Evidence.Proposal.Hash()),
			},
			BlockHeight: object.Evidence.BlockHeight,
		}
		obj.CompareEvidence = ProposalEvidenceInfo{
			Proposal: DPOSProposalInfo{
				Sponsor:    common.BytesToHexString(object.CompareEvidence.Proposal.Sponsor),
				BlockHash:  common.ToReversedString(object.CompareEvidence.Proposal.BlockHash),
				ViewOffset: object.CompareEvidence.Proposal.ViewOffset,
				Sign:       common.BytesToHexString(object.CompareEvidence.Proposal.Sign),
				Hash:       common.ToReversedString(object.CompareEvidence.Proposal.Hash()),
			},
			BlockHeight: object.CompareEvidence.BlockHeight,
		}
		return obj
	case *payload.DPOSIllegalVotes:
		obj := new(DPOSIllegalVotesInfo)
		obj.Hash = common.ToReversedString(object.Hash())
		obj.Evidence = VoteEvidenceInfo{
			ProposalEvidenceInfo: ProposalEvidenceInfo{
				Proposal: DPOSProposalInfo{
					Sponsor:    common.BytesToHexString(object.Evidence.Proposal.Sponsor),
					BlockHash:  common.ToReversedString(object.Evidence.Proposal.BlockHash),
					ViewOffset: object.Evidence.Proposal.ViewOffset,
					Sign:       common.BytesToHexString(object.Evidence.Proposal.Sign),
					Hash:       common.ToReversedString(object.Evidence.Proposal.Hash()),
				},
				BlockHeight: object.Evidence.BlockHeight,
			},
			Vote: DPOSProposalVoteInfo{
				ProposalHash: common.ToReversedString(object.Evidence.Vote.ProposalHash),
				Signer:       common.BytesToHexString(object.Evidence.Vote.Signer),
				Accept:       object.Evidence.Vote.Accept,
				Sign:         common.BytesToHexString(object.Evidence.Vote.Sign),
				Hash:         common.ToReversedString(object.Evidence.Vote.Hash()),
			},
		}
		obj.CompareEvidence = VoteEvidenceInfo{
			ProposalEvidenceInfo: ProposalEvidenceInfo{
				Proposal: DPOSProposalInfo{
					Sponsor:    common.BytesToHexString(object.CompareEvidence.Proposal.Sponsor),
					BlockHash:  common.ToReversedString(object.CompareEvidence.Proposal.BlockHash),
					ViewOffset: object.CompareEvidence.Proposal.ViewOffset,
					Sign:       common.BytesToHexString(object.CompareEvidence.Proposal.Sign),
					Hash:       common.ToReversedString(object.CompareEvidence.Proposal.Hash()),
				},
				BlockHeight: object.CompareEvidence.BlockHeight,
			},
			Vote: DPOSProposalVoteInfo{
				ProposalHash: common.ToReversedString(object.CompareEvidence.Vote.ProposalHash),
				Signer:       common.BytesToHexString(object.CompareEvidence.Vote.Signer),
				Accept:       object.CompareEvidence.Vote.Accept,
				Sign:         common.BytesToHexString(object.CompareEvidence.Vote.Sign),
				Hash:         common.ToReversedString(object.CompareEvidence.Vote.Hash()),
			},
		}
		return obj
	case *payload.DPOSIllegalBlocks:
		obj := new(DPOSIllegalBlocksInfo)
		obj.Hash = common.ToReversedString(object.Hash())
		obj.CoinType = uint32(object.CoinType)
		obj.BlockHeight = object.BlockHeight
		eviSigners := make([]string, 0)
		for _, s := range object.Evidence.Signers {
			eviSigners = append(eviSigners, common.BytesToHexString(s))
		}
		obj.Evidence = BlockEvidenceInfo{
			Header:       common.BytesToHexString(object.Evidence.Header),
			BlockConfirm: common.BytesToHexString(object.Evidence.BlockConfirm),
			Signers:      eviSigners,
			Hash:         common.ToReversedString(object.Evidence.BlockHash()),
		}
		compEviSigners := make([]string, 0)
		for _, s := range object.CompareEvidence.Signers {
			compEviSigners = append(compEviSigners, common.BytesToHexString(s))
		}
		obj.CompareEvidence = BlockEvidenceInfo{
			Header:       common.BytesToHexString(object.CompareEvidence.Header),
			BlockConfirm: common.BytesToHexString(object.CompareEvidence.BlockConfirm),
			Signers:      compEviSigners,
			Hash:         common.ToReversedString(object.CompareEvidence.BlockHash()),
		}
		return obj
	}
	return nil
}

func getOutputPayloadInfo(op OutputPayload) OutputPayloadInfo {
	switch object := op.(type) {
	case *outputpayload.DefaultOutput:
		obj := new(DefaultOutputInfo)
		return obj
	case *outputpayload.VoteOutput:
		obj := new(VoteOutputInfo)
		obj.Version = object.Version
		for _, content := range object.Contents {
			var contentInfo VoteContentInfo
			contentInfo.VoteType = content.VoteType
			switch contentInfo.VoteType {
			case outputpayload.Delegate:
				for _, cv := range content.CandidateVotes {
					contentInfo.CandidatesInfo = append(contentInfo.CandidatesInfo,
						CandidateVotes{
							Candidate: common.BytesToHexString(cv.Candidate),
							Votes:     cv.Votes.String(),
						})
				}
			case outputpayload.CRC:
				for _, cv := range content.CandidateVotes {
					c, _ := common.Uint168FromBytes(cv.Candidate)
					addr, _ := c.ToAddress()
					contentInfo.CandidatesInfo = append(contentInfo.CandidatesInfo,
						CandidateVotes{
							Candidate: addr,
							Votes:     cv.Votes.String(),
						})
				}
			case outputpayload.CRCProposal:
				for _, cv := range content.CandidateVotes {
					c, _ := common.Uint256FromBytes(cv.Candidate)
					contentInfo.CandidatesInfo = append(contentInfo.CandidatesInfo,
						CandidateVotes{
							Candidate: ToReversedString(*c),
							Votes:     cv.Votes.String(),
						})
				}
			case outputpayload.CRCImpeachment:
				for _, cv := range content.CandidateVotes {
					c, _ := common.Uint168FromBytes(cv.Candidate)
					addr, _ := c.ToAddress()
					contentInfo.CandidatesInfo = append(contentInfo.CandidatesInfo,
						CandidateVotes{
							Candidate: addr,
							Votes:     cv.Votes.String(),
						})
				}
			}
			obj.Contents = append(obj.Contents, contentInfo)
		}
		return obj
	}

	return nil
}

func VerifyAndSendTx(tx *Transaction) error {
	// if transaction is verified unsuccessfully then will not put it into transaction pool
	if err := TxMemPool.AppendToTxPool(tx); err != nil {
		log.Info("[httpjsonrpc] VerifyTransaction failed when AppendToTxnPool. Errcode:", err)
		return err
	}

	// Relay tx inventory to other peers.
	txHash := tx.Hash()
	iv := msg.NewInvVect(msg.InvTypeTx, &txHash)
	Server.RelayInventory(iv, tx)

	return nil
}

func ResponsePack(errCode ServerErrCode, result interface{}) map[string]interface{} {
	if errCode != 0 && (result == "" || result == nil) {
		result = ErrMap[errCode]
	}
	return map[string]interface{}{"Result": result, "Error": errCode}
}

func ResponsePackEx(errCode ServerErrCode, result interface{}) map[string]interface{} {
	return map[string]interface{}{"result": result, "status": errCode}
}

func GetHistory(param Params) map[string]interface{} {
	addr, ok := param.String("addr")
	if !ok {
		return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "address can not be blank")
	}
	_, err := common.Uint168FromAddress(addr)
	if err != nil {
		return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "invalid address")
	}
	order, exist := param.String("order")
	if exist {
		if order != "asc" && order != "desc" {
			return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "invalid order")
		}
	}
	ok = param.HasKey("pageNum")
	ok1 := param.HasKey("pageSize")
	ver, _ := param.String("version")
	if !ok && !ok1 {
		txhs := blockchain2.DefaultChainStoreEx.GetTxHistory(addr, order, ver)
		var len int
		switch txhs.(type) {
		case types.TransactionHistorySorter:
			len = txhs.(types.TransactionHistorySorter).Len()
		case types.TransactionHistorySorterDesc:
			len = txhs.(types.TransactionHistorySorterDesc).Len()
		}
		thr := types.ThResult{
			History:  txhs,
			TotalNum: len,
		}
		return ResponsePackEx(ELEPHANT_SUCCESS, thr)
	} else if ok && ok1 {
		pageNum, cool := param.Uint("pageNum")
		if !cool {
			return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "")
		}
		pageSize, cool := param.Uint("pageSize")
		if !cool {
			return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "")
		}
		txhs, total := blockchain2.DefaultChainStoreEx.GetTxHistoryByPage(addr, order, ver, pageNum, pageSize)
		thr := types.ThResult{
			History:  txhs,
			TotalNum: total,
		}
		return ResponsePackEx(ELEPHANT_SUCCESS, thr)
	}
	return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "")
}

func GetPublicKey(param Params) map[string]interface{} {
	addr, ok := param.String("addr")
	if !ok {
		return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "")
	}
	_, err := common.Uint168FromAddress(addr)
	if err != nil {
		return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "")
	}
	publicKey := blockchain2.DefaultChainStoreEx.GetPublicKey(addr)
	if publicKey == "" {
		return ResponsePackEx(ELEPHANT_SUCCESS, "Can not find pubkey of this address, please using this address send a transaction first")
	} else {
		return ResponsePackEx(ELEPHANT_SUCCESS, publicKey)
	}
}

func CreateTx(param Params) map[string]interface{} {
	log.Debugf("CreateTx request param %v", param)
	inputs, ok := param["inputs"].([]interface{})
	if !ok {
		return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "Can not find inputs")
	}
	var utxoList []types.Utxos
	for _, v := range inputs {
		input, ok := v.(string)
		if !ok {
			return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "Not valid input value")
		}
		programHash, err := common.Uint168FromAddress(input)
		if err != nil {
			return ResponsePack(InvalidParams, "invalid address, "+err.Error())
		}
		utxos, err := Store.GetFFLDB().GetUTXO(programHash)
		if err != nil {
			return ResponsePack(InvalidParams, "list unspent failed, "+err.Error())
		}
		utxoList = append(utxoList, utxos)
	}
	outputs, ok := param["outputs"].([]interface{})
	if !ok {
		return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "Can not find outputs")
	}
	var smAmt int64
	for _, v := range outputs {
		output := v.(map[string]interface{})
		_, ok := output["addr"].(string)
		if !ok {
			return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "Can not find addr in output")
		}
		var amt float64
		var err error
		switch output["amt"].(type) {
		case float64:
			amt = output["amt"].(float64)
		case string:
			amt, err = strconv.ParseFloat(output["amt"].(string), 64)
			if err != nil {
				return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "Can not find amt in output")
			}
		default:
			return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "Can not find amt in output")
		}
		smAmt += int64(amt)
	}
	paraListMap := make(map[string]interface{})
	txList := make([]map[string]interface{}, 0)
	var index = -1
	var multiTxNum = 0
	var bundleUtxoSize = 100
	if common2.Conf.BundleUtxoSize > 100 {
		bundleUtxoSize = common2.Conf.BundleUtxoSize
	}
	var spendMoney int64 = 0
	var hasEnoughFee bool = false
	for i, utxos := range utxoList {
		if i >= 1 {
			return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "Only support single spend address")
		}
		addr := inputs[i].(string)
		sort.Sort(utxos)
		for j, utxo := range utxos {
			index = j
			spendMoney += int64(utxo.Value)
			if (j+1)%bundleUtxoSize == 0 {
				multiTxNum = (j + 1) / bundleUtxoSize
			} else {
				multiTxNum = (j+1)/bundleUtxoSize + 1
			}
			if spendMoney >= smAmt+int64(config.Parameters.PowConfiguration.MinTxFee*multiTxNum) {
				hasEnoughFee = true
				break
			}
		}

		if !hasEnoughFee {
			return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "Not Enough UTXO")
		}

		var hasGiveLeftMoney = false
		leftMoney := spendMoney - int64(config.Parameters.PowConfiguration.MinTxFee*multiTxNum) - smAmt
		for h := 0; h < multiTxNum; h++ {
			txListMap := make(map[string]interface{})
			var currTxSum int64 = 0
			utxoInputsArray := make([]map[string]interface{}, 0)
			for z := h * bundleUtxoSize; z < (h+1)*bundleUtxoSize; z++ {
				if z > index {
					break
				}
				utxoInputsDetail := make(map[string]interface{})
				b, _ := FromReversedString(utxos[z].TxID.String())
				utxoInputsDetail["txid"] = hex.EncodeToString(b)
				utxoInputsDetail["index"] = utxos[z].Index
				utxoInputsDetail["address"] = addr
				currTxSum += utxos[z].Value.IntValue()
				utxoInputsArray = append(utxoInputsArray, utxoInputsDetail)
			}

			if currTxSum < int64(config.Parameters.PowConfiguration.MinTxFee) {
				continue
			}

			utxoOutputsArray := make([]map[string]interface{}, 0)
			if len(outputs) == 1 {
				output := outputs[0].(map[string]interface{})
				utxoOutputsDetail := make(map[string]interface{})
				utxoOutputsDetail["address"] = output["addr"]
				if !hasGiveLeftMoney && currTxSum >= leftMoney+int64(config.Parameters.PowConfiguration.MinTxFee) {
					utxoOutputsDetail["amount"] = currTxSum - int64(config.Parameters.PowConfiguration.MinTxFee) - leftMoney
					utxoOutputsArray = append(utxoOutputsArray, utxoOutputsDetail)
					if leftMoney > 0 {
						utxoOutputsDetailLeft := make(map[string]interface{})
						utxoOutputsDetailLeft["address"] = inputs[0]
						utxoOutputsDetailLeft["amount"] = leftMoney
						utxoOutputsArray = append(utxoOutputsArray, utxoOutputsDetailLeft)
					}
					hasGiveLeftMoney = true
				} else {
					utxoOutputsDetail["amount"] = currTxSum - int64(config.Parameters.PowConfiguration.MinTxFee)
					utxoOutputsArray = append(utxoOutputsArray, utxoOutputsDetail)
				}
			} else {
				return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "Only support single output")
			}

			if config.Parameters.PowConfiguration.MinTxFee > 100 && common2.Conf.EarnReward {
				utxoOutputsDetail := make(map[string]interface{})
				utxoOutputsDetail["address"] = config.Parameters.PowConfiguration.PayToAddr
				utxoOutputsDetail["amount"] = int64(config.Parameters.PowConfiguration.MinTxFee - 100)
				utxoOutputsArray = append(utxoOutputsArray, utxoOutputsDetail)
			}
			txListMap["UTXOInputs"] = utxoInputsArray
			txListMap["Outputs"] = utxoOutputsArray
			if common2.Conf.EarnReward {
				txListMap["Fee"] = 100
			} else {
				txListMap["Fee"] = config.Parameters.PowConfiguration.MinTxFee
			}
			txListMap["Total_Node_Fee"] = config.Parameters.PowConfiguration.MinTxFee
			var orgMsg string
			for i, input := range utxoInputsArray {
				orgMsg += input["txid"].(string) + "-" + strconv.Itoa(int(input["index"].(uint16)))
				if i != len(utxoInputsArray)-1 {
					orgMsg += ";"
				}
			}
			orgMsg += "&"
			for i, output := range utxoOutputsArray {

				orgMsg += output["address"].(string) + "-" + strconv.Itoa(int(output["amount"].(int64)))
				if i != len(utxoOutputsArray)-1 {
					orgMsg += ";"
				}
			}
			orgMsg += "&"
			orgMsg += strconv.Itoa(txListMap["Fee"].(int))

			log.Debugf("Curr Origin msg %s", orgMsg)
			signature, err := crypto.Sign(NodePrivKey, []byte(orgMsg))
			if err != nil {
				return ResponsePackEx(ELEPHANT_INTERNAL_ERROR, err.Error())
			}
			proof := make(map[string]interface{})
			txListMap["Postmark"] = proof
			proof["signature"] = hex.EncodeToString(signature)
			proof["pub"] = hex.EncodeToString(NodePubKey)
			txList = append(txList, txListMap)
		}
		if !hasGiveLeftMoney {
			return ResponsePackEx(ELEPHANT_INTERNAL_ERROR, "Not giving left money , logic error")
		}
	}
	paraListMap["Transactions"] = txList
	if !hasEnoughFee {
		return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "Not Enough UTXO")
	}
	return ResponsePackEx(ELEPHANT_SUCCESS, paraListMap)
}

func CreateVoteTx(param Params) map[string]interface{} {
	log.Debugf("CreateVoteTx request param %v", param)
	inputs, ok := param["inputs"].([]interface{})
	if !ok {
		return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "Can not find inputs")
	}
	var utxoList []types.Utxos
	var total int64
	var multiTxNum = 0
	var bundleUtxoSize = 100
	if common2.Conf.BundleUtxoSize > 100 {
		bundleUtxoSize = common2.Conf.BundleUtxoSize
	}
	for _, v := range inputs {
		input, ok := v.(string)
		if !ok {
			return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "Not valid input value")
		}
		programHash, err := common.Uint168FromAddress(input)
		if err != nil {
			return ResponsePack(InvalidParams, "invalid address, "+err.Error())
		}
		utxos, err := Store.GetFFLDB().GetUTXO(programHash)
		if err != nil {
			return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "Internal error")
		}

		utxoList = append(utxoList, utxos)
	}

	utxos := utxoList[0]
	sort.Sort(utxos)
	for m, utxo := range utxos {
		total += int64(utxo.Value)
		if m == len(utxos)-1 {
			if (m+1)%bundleUtxoSize == 0 {
				multiTxNum = (m + 1) / bundleUtxoSize
			} else {
				multiTxNum = (m+1)/bundleUtxoSize + 1
			}
		}
	}

	outputs, ok := param["outputs"].([]interface{})
	if !ok {
		return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "Can not find outputs")
	}
	var smAmt int64
	if len(outputs) != 1 {
		return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "Only support single output")
	}
	var sendAmt int64
	for _, v := range outputs {
		output := v.(map[string]interface{})
		_, ok := output["addr"].(string)
		if !ok {
			return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "Can not find addr in output")
		}
		var amt float64
		var err error
		switch output["amt"].(type) {
		case float64:
			amt = output["amt"].(float64)
		case string:
			amt, err = strconv.ParseFloat(output["amt"].(string), 64)
			if err != nil {
				return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "Can not find amt in output")
			}
		default:
			return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "Can not find amt in output")
		}
		sendAmt = int64(amt)
		smAmt += int64(amt)
	}
	var left = total - smAmt - int64(config.Parameters.PowConfiguration.MinTxFee)*int64(multiTxNum)
	if left < 0 {
		return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "Not Enough UTXO")
	}
	smAmt = total - int64(config.Parameters.PowConfiguration.MinTxFee)*int64(multiTxNum)
	outputs = append(outputs, map[string]interface{}{"addr": (inputs[0]).(string), "amt": left})

	paraListMap := make(map[string]interface{})
	txList := make([]map[string]interface{}, 0)
	var index = -1
	var spendMoney int64 = 0
	var hasEnoughFee bool = false
	for i, utxos := range utxoList {
		if i >= 1 {
			return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "Only support single spend address")
		}
		addr := inputs[i].(string)
		for j, utxo := range utxos {
			index = j
			spendMoney += int64(utxo.Value)
			if spendMoney >= smAmt+int64(config.Parameters.PowConfiguration.MinTxFee*multiTxNum) {
				hasEnoughFee = true
				break
			}
		}

		if !hasEnoughFee {
			return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "Not Enough UTXO")
		}

		var normalTransferAmtOver = false
		var normalTransferLeft = sendAmt
		leftMoney := spendMoney - int64(config.Parameters.PowConfiguration.MinTxFee*multiTxNum) - smAmt
		if leftMoney != 0 {
			return ResponsePackEx(ELEPHANT_INTERNAL_ERROR, "Vote Tx leftMoney not 0")
		}
		for h := 0; h < multiTxNum; h++ {
			txListMap := make(map[string]interface{})
			var currTxSum int64 = 0
			utxoInputsArray := make([]map[string]interface{}, 0)
			for z := h * bundleUtxoSize; z < (h+1)*bundleUtxoSize; z++ {
				if z > index {
					break
				}
				utxoInputsDetail := make(map[string]interface{})
				b, _ := FromReversedString(utxos[z].TxID.String())
				utxoInputsDetail["txid"] = hex.EncodeToString(b)
				utxoInputsDetail["index"] = utxos[z].Index
				utxoInputsDetail["address"] = addr
				currTxSum += utxos[z].Value.IntValue()
				utxoInputsArray = append(utxoInputsArray, utxoInputsDetail)
			}

			if currTxSum < int64(config.Parameters.PowConfiguration.MinTxFee) {
				continue
			}

			utxoOutputsArray := make([]map[string]interface{}, 0)
			if len(outputs) == 2 {
				output := outputs[0].(map[string]interface{})
				utxoOutputsDetail := make(map[string]interface{})
				utxoOutputsDetail["address"] = output["addr"]

				utxoOutputsDetailReward := make(map[string]interface{})
				if config.Parameters.PowConfiguration.MinTxFee > 100 && common2.Conf.EarnReward {
					utxoOutputsDetailReward["address"] = config.Parameters.PowConfiguration.PayToAddr
					utxoOutputsDetailReward["amount"] = int64(config.Parameters.PowConfiguration.MinTxFee - 100)
				}

				output1 := outputs[1].(map[string]interface{})
				utxoOutputsDetail1 := make(map[string]interface{})
				utxoOutputsDetail1["address"] = output1["addr"]

				if normalTransferAmtOver {
					// first send address
					utxoOutputsDetail["amount"] = int64(0)

					// owner address
					utxoOutputsDetail1["amount"] = currTxSum - int64(config.Parameters.PowConfiguration.MinTxFee)
				} else {
					if currTxSum >= normalTransferLeft+int64(config.Parameters.PowConfiguration.MinTxFee) {
						// first send address
						utxoOutputsDetail["amount"] = normalTransferLeft
						// owner address
						utxoOutputsDetail1["amount"] = currTxSum - normalTransferLeft - int64(config.Parameters.PowConfiguration.MinTxFee)
						normalTransferLeft = 0
						normalTransferAmtOver = true
					} else {
						// first send address
						utxoOutputsDetail["amount"] = currTxSum - int64(config.Parameters.PowConfiguration.MinTxFee)
						normalTransferLeft -= currTxSum - int64(config.Parameters.PowConfiguration.MinTxFee)

						// owner address
						utxoOutputsDetail1["amount"] = int64(0)
					}
				}
				utxoOutputsArray = append(utxoOutputsArray, utxoOutputsDetail)
				if config.Parameters.PowConfiguration.MinTxFee > 100 && common2.Conf.EarnReward {
					utxoOutputsArray = append(utxoOutputsArray, utxoOutputsDetailReward)
				}
				utxoOutputsArray = append(utxoOutputsArray, utxoOutputsDetail1)
			} else {
				return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "Only support single output")
			}

			txListMap["UTXOInputs"] = utxoInputsArray
			txListMap["Outputs"] = utxoOutputsArray
			if common2.Conf.EarnReward {
				txListMap["Fee"] = 100
			} else {
				txListMap["Fee"] = config.Parameters.PowConfiguration.MinTxFee
			}
			txListMap["Total_Node_Fee"] = config.Parameters.PowConfiguration.MinTxFee
			var orgMsg string
			for i, input := range utxoInputsArray {
				orgMsg += input["txid"].(string) + "-" + strconv.Itoa(int(input["index"].(uint16)))
				if i != len(utxoInputsArray)-1 {
					orgMsg += ";"
				}
			}
			orgMsg += "&"
			for i, output := range utxoOutputsArray {
				orgMsg += output["address"].(string) + "-" + strconv.Itoa(int(output["amount"].(int64)))
				if i != len(utxoOutputsArray)-1 {
					orgMsg += ";"
				}
			}
			orgMsg += "&"
			orgMsg += strconv.Itoa(txListMap["Fee"].(int))

			log.Debugf("Curr Origin msg %s", orgMsg)

			signature, err := crypto.Sign(NodePrivKey, []byte(orgMsg))
			if err != nil {
				return ResponsePackEx(ELEPHANT_INTERNAL_ERROR, err.Error())
			}
			proof := make(map[string]interface{})
			txListMap["Postmark"] = proof
			proof["signature"] = hex.EncodeToString(signature)
			proof["pub"] = hex.EncodeToString(NodePubKey)
			txList = append(txList, txListMap)
		}

		if !normalTransferAmtOver {
			return ResponsePackEx(ELEPHANT_INTERNAL_ERROR, "basic normal transfer not complete , logic error")
		}
	}
	paraListMap["Transactions"] = txList
	if !hasEnoughFee {
		return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "Not Enough UTXO")
	}
	return ResponsePackEx(ELEPHANT_SUCCESS, paraListMap)
}

func ProducerStatistic(param Params) map[string]interface{} {
	blockchain2.DefaultChainStoreEx.LockDposData()
	defer blockchain2.DefaultChainStoreEx.UnlockDposData()
	pub, ok := param["producer"].(string)
	if !ok || pub == "" || len(pub) != 66 {
		return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, " invalid public key ")
	}
	height, ok := param["height"].(string)
	var iHeight int
	if !ok {
		iHeight = 99999999
	}
	iHeight, err := strconv.Atoi(height)
	if err != nil {
		iHeight = 99999999
	}
	type ret struct {
		Producer_public_key string `json:",omitempty"`
		Vote_type           string `json:",omitempty"`
		Txid                string `json:",omitempty"`
		Value               string `json:",omitempty"`
		Outputlock          int    `json:",omitempty"`
		Address             string `json:",omitempty"`
		Block_time          int64  `json:",omitempty"`
		Height              int64  `json:",omitempty"`
	}

	rst, err := blockchain2.DBA.ToStruct("select Producer_public_key,Vote_type,Txid,Value,Address,Block_time,Height from chain_vote_info where producer_public_key = '"+pub+"' and (outputlock = 0 or outputlock >= height) and (cancel_height > "+strconv.Itoa(iHeight)+" or cancel_height is null) and height <=  "+strconv.Itoa(iHeight), ret{})
	if err != nil {
		return ResponsePackEx(ELEPHANT_INTERNAL_ERROR, " internal error : "+err.Error())
	}
	if err != nil {
		return ResponsePackEx(ELEPHANT_INTERNAL_ERROR, " internal error : "+err.Error())
	}
	return ResponsePackEx(ELEPHANT_SUCCESS, rst)
}

func VoterStatistic(param Params) map[string]interface{} {
	blockchain2.DefaultChainStoreEx.LockDposData()
	defer blockchain2.DefaultChainStoreEx.UnlockDposData()
	addr, ok := param["addr"].(string)
	if !ok || addr == "" || len(addr) != 34 {
		return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, " invalid address ")
	}
	pageNum, _ := param["pageNum"].(string)
	var sql string
	var from int64
	var size int64
	if pageNum != "" {
		pageSize, _ := param["pageSize"].(string)
		if pageSize != "" {
			var err error
			size, err = strconv.ParseInt(pageSize, 10, 64)
			if err != nil {
				return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, err.Error())
			}
			if size > 3 {
				return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "page size can not bigger then 3")
			}
		} else {
			size = 10
		}
		num, err := strconv.ParseInt(pageNum, 10, 64)
		if err != nil {
			return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, err.Error())
		}
		if num <= 0 {
			num = 1
		}
		from = (num - 1) * size
	} else {
		return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, " must use pagination ")
	}
	sql = "select * from chain_vote_info where address = '" + addr + "' order by _id desc "
	info, err := blockchain2.DBA.ToStruct(sql, types.Vote_info{})
	if err != nil {
		return ResponsePackEx(ELEPHANT_INTERNAL_ERROR, " internal error : "+err.Error())
	}
	headersContainer := make(map[string]*types.Vote_statistic_header)
	for i := 0; i < len(info); i++ {
		data := info[i].(*types.Vote_info)
		h, ok := headersContainer[data.Txid+strconv.Itoa(data.N)]
		if ok {
			h.Node_num += 1
			h.Nodes = append(h.Nodes, data.Producer_public_key)
		} else {
			h = new(types.Vote_statistic_header)
			h.Value = data.Value
			h.Node_num = 1
			h.Txid = data.Txid
			h.Height = data.Height
			h.Nodes = []string{data.Producer_public_key}
			h.Block_time = data.Block_time
			h.Is_valid = data.Is_valid
			headersContainer[data.Txid+strconv.Itoa(data.N)] = h
		}
	}
	var voteStatisticSorter types.Vote_statisticSorter
	for _, v := range headersContainer {
		voteStatisticSorter = append(voteStatisticSorter, types.Vote_statistic{
			*v,
			[]types.Vote_info{},
		})
	}
	sort.Sort(voteStatisticSorter)
	if !(from == 0 && size == 0) && int(from+1+size) <= len(voteStatisticSorter) {
		voteStatisticSorter = voteStatisticSorter[from : from+size]
	} else if !(from == 0 && size == 0) && int(from+1) <= len(voteStatisticSorter) && int(from+1+size) > len(voteStatisticSorter) {
		voteStatisticSorter = voteStatisticSorter[from:]
	}
	var voteStatistic types.Vote_statisticSorter
	ranklisthoder := make(map[int64][]interface{})
	//height+producer_public_key : index
	ranklisthoderByProducer := make(map[string]int)
	for _, _v := range voteStatisticSorter {
		v := _v.Vote_Header
		rst, ok := ranklisthoder[v.Height]
		if !ok {
			rst, err = blockchain2.DBA.ToStruct(`select m.* from (select ifnull(a.producer_public_key,b.ownerpublickey) as producer_public_key , ifnull(a.value,0) as value , b.* from 
chain_producer_info b left join 
(select A.producer_public_key , cast(ROUND(sum(value),8) as text) as value from chain_vote_info A where (A.cancel_height > `+strconv.Itoa(int(v.Height))+` or
cancel_height is null) and height <= `+strconv.Itoa(int(v.Height))+` group by producer_public_key) a on a.producer_public_key = b.ownerpublickey 
order by value * 100000000  desc) m`, types.Vote_info{})
			if err != nil {
				return ResponsePackEx(ELEPHANT_INTERNAL_ERROR, " internal error : "+err.Error())
			}
			totalVote, err := blockchain2.DBA.ToFloat(`	select sum(a.value)  from (select A.producer_public_key , sum(value) as value from chain_vote_info A where (A.cancel_height > ` + strconv.Itoa(int(v.Height)) + ` or
	 cancel_height is null) and height <= ` + strconv.Itoa(int(v.Height)) + ` group by producer_public_key order by value desc limit 96) a`)
			if err != nil {
				return ResponsePackEx(ELEPHANT_INTERNAL_ERROR, " internal error : "+err.Error())
			}
			for i, r := range rst {
				vi := r.(*types.Vote_info)
				public, _ := hex.DecodeString(vi.Ownerpublickey)
				addr, err := common2.GetAddress(public)
				if err != nil {
					log.Warn("Invalid Ownerpublickey " + vi.Ownerpublickey)
					continue
				}
				vi.Address = addr
				vi.Rank = int64(i + 1)
				val, err := blockchain2.DefaultChainStoreEx.GetDposRewardByHeight(addr, uint32(v.Height))
				if err != nil {
					vi.Reward = "0"
				} else {
					vi.Reward = val.String()
				}
				var vote float64
				if vi.Value == "" {
					vote = 0
				} else {
					vote, err = strconv.ParseFloat(vi.Value, 64)
					if err != nil {
						return ResponsePackEx(ELEPHANT_INTERNAL_ERROR, " internal error : "+err.Error())
					}
				}
				if vi.Rank <= 24 {
					vi.EstRewardPerYear = strconv.FormatFloat(float64(175834088*0.25/(100000000*36)*365*720+175834088*0.75/(totalVote*100000000)*vote*365*720), 'f', 8, 64)
				} else if vi.Rank <= 96 {
					vi.EstRewardPerYear = strconv.FormatFloat(float64(175834088*0.75/(totalVote*100000000)*vote*365*720), 'f', 8, 64)
				} else {
					vi.EstRewardPerYear = "0"
				}
			}
			for m := 0; m < len(rst); m++ {
				ranklisthoderByProducer[strconv.Itoa(int(v.Height))+rst[m].(*types.Vote_info).Producer_public_key] = m
			}
		}
		var voteInfos []types.Vote_info
		for _, pub := range v.Nodes {
			voteInfos = append(voteInfos, *rst[ranklisthoderByProducer[strconv.Itoa(int(v.Height))+pub]].(*types.Vote_info))
		}
		voteStatistic = append(voteStatistic, types.Vote_statistic{
			v,
			voteInfos,
		})
	}
	return ResponsePackEx(ELEPHANT_SUCCESS, voteStatistic)
}

//
func ProducerRankByHeight(param Params) map[string]interface{} {
	blockchain2.DefaultChainStoreEx.LockDposData()
	defer blockchain2.DefaultChainStoreEx.UnlockDposData()
	height, ok := param["height"].(string)
	if !ok {
		return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "invalid height")
	}
	h, err := strconv.Atoi(height)
	if err != nil || h < 0 {
		return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "invalid height")
	}
	state, ok := param["state"].(string)
	if state != "" && state != "active" && state != "inactive" && state != "pending" &&
		state != "canceled" && state != "illegal" && state != "returned" {
		return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "state can be one of the folowing values active,inactive,pending,canceled,illegal,returned")
	}
	var rst []interface{}
	if state == "" {
		rst, err = blockchain2.DBA.ToStruct(`select m.* from (select ifnull(a.producer_public_key,b.ownerpublickey) as producer_public_key , ifnull(a.value,0) as value , b.* from
chain_producer_info b left join 
(select A.producer_public_key , cast(ROUND(sum(value),8) as text) as value from chain_vote_info A where (A.cancel_height > `+height+` or
cancel_height is null) and height <= `+height+` group by producer_public_key) a on a.producer_public_key = b.ownerpublickey
order by value * 100000000 desc) m `, types.Vote_info{})
	} else {
		rst, err = blockchain2.DBA.ToStruct(`select m.* from (select ifnull(a.producer_public_key,b.ownerpublickey) as producer_public_key , ifnull(a.value,0) as value , b.* from
chain_producer_info b left join 
(select A.producer_public_key , cast(ROUND(sum(value),8) as text) as value from chain_vote_info A where (A.cancel_height > `+height+` or
cancel_height is null) and height <= `+height+` group by producer_public_key) a on a.producer_public_key = b.ownerpublickey where b.state = '`+strings.ToUpper(state[:1])+state[1:]+`'
order by value * 100000000  desc) m `, types.Vote_info{})
	}
	if err != nil {
		return ResponsePackEx(ELEPHANT_INTERNAL_ERROR, " internal error : "+err.Error())
	}

	totalVote, err := blockchain2.DBA.ToFloat(`	select sum(a.value)  from (select A.producer_public_key , sum(value) as value from chain_vote_info A where A.cancel_height > ` + height + ` or
	 cancel_height is null group by producer_public_key order by value desc limit 96) a`)
	if err != nil {
		return ResponsePackEx(ELEPHANT_INTERNAL_ERROR, " internal error : "+err.Error())
	}
	for i, r := range rst {
		vi := r.(*types.Vote_info)
		public, _ := hex.DecodeString(vi.Ownerpublickey)
		addr, err := common2.GetAddress(public)
		if err != nil {
			log.Warn("Invalid Ownerpublickey " + vi.Ownerpublickey)
			continue
		}
		vi.Address = addr
		val, err := blockchain2.DefaultChainStoreEx.GetDposReward(addr)
		if err != nil {
			log.Warn("Invalid Ownerpublickey " + vi.Ownerpublickey)
			continue
		}
		vi.Reward = val.String()
		vi.Rank = int64(i + 1)
		var vote float64
		if vi.Value == "" {
			vote = 0
		} else {
			vote, err = strconv.ParseFloat(vi.Value, 64)
			if err != nil {
				return ResponsePackEx(ELEPHANT_INTERNAL_ERROR, " internal error : "+err.Error())
			}
		}
		if vi.Rank <= 24 {
			vi.EstRewardPerYear = strconv.FormatFloat(float64(175834088*0.25/(100000000*36)*365*720+175834088*0.75/(totalVote*100000000)*vote*365*720), 'f', 8, 64)
		} else if vi.Rank <= 96 {
			vi.EstRewardPerYear = strconv.FormatFloat(float64(175834088*0.75/(totalVote*100000000)*vote*365*720), 'f', 8, 64)
		} else {
			vi.EstRewardPerYear = "0"
		}
	}

	return ResponsePackEx(ELEPHANT_SUCCESS, rst)
}

func TotalVoteByHeight(param Params) map[string]interface{} {
	blockchain2.DefaultChainStoreEx.LockDposData()
	defer blockchain2.DefaultChainStoreEx.UnlockDposData()
	height, ok := param["height"].(string)
	if !ok {
		return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "invalid height")
	}
	h, err := strconv.Atoi(height)
	if err != nil || h < 0 {
		return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "invalid height")
	}
	rst, err := blockchain2.DBA.ToFloat(`select ifnull(ROUND(sum(value),8),0) as value from chain_producer_info b left join chain_vote_info a on a.producer_public_key = b.ownerpublickey  where (cancel_height > ` + height + ` or cancel_height is null) and height <= ` + height + ``)
	if err != nil {
		return ResponsePackEx(ELEPHANT_INTERNAL_ERROR, " internal error : "+err.Error())
	}

	return ResponsePackEx(ELEPHANT_SUCCESS, rst)
}

func GetProducerByTxs(param Params) map[string]interface{} {
	blockchain2.DefaultChainStoreEx.LockDposData()
	defer blockchain2.DefaultChainStoreEx.UnlockDposData()
	txids, ok := param["txid"].([]interface{})
	if !ok {
		return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "Can not find txid")
	}
	type ret struct {
		Producer interface{}
		Txid     string
	}
	var rst []ret
	for _, v := range txids {
		txid := v.(string)
		tmp := types.Producer_info{}
		producer, err := blockchain2.DBA.ToStruct("select b.* from chain_producer_info b left join chain_vote_info a on a.producer_public_key = b.ownerpublickey where a.txid = '"+txid+"'", tmp)
		if err != nil {
			return ResponsePackEx(ELEPHANT_INTERNAL_ERROR, " internal error : "+err.Error())
		}
		if len(producer) > 0 && producer[0] != nil {
			rst = append(rst, ret{
				Producer: producer,
				Txid:     txid,
			})
		}
	}
	return ResponsePackEx(ELEPHANT_SUCCESS, rst)
}

func NodeRewardAddr(param Params) map[string]interface{} {
	return ResponsePackEx(ELEPHANT_SUCCESS, config.Parameters.PowConfiguration.PayToAddr)
}

func CrDidStatistic(param Params) map[string]interface{} {
	blockchain2.DefaultChainStoreEx.LockDposData()
	defer blockchain2.DefaultChainStoreEx.UnlockDposData()
	did, ok := param["did"].(string)
	if !ok || did == "" || len(did) != 34 {
		return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, " invalid cr did ")
	}
	height, ok := param["height"].(string)
	var iHeight int
	if !ok {
		iHeight = 99999999
	}
	iHeight, err := strconv.Atoi(height)
	if err != nil {
		iHeight = 99999999
	}
	type ret struct {
		Did        string `json:",omitempty"`
		Vote_type  string `json:",omitempty"`
		Txid       string `json:",omitempty"`
		Value      string `json:",omitempty"`
		Outputlock int    `json:",omitempty"`
		Address    string `json:",omitempty"`
		Block_time int64  `json:",omitempty"`
		Height     int64  `json:",omitempty"`
	}

	rst, err := blockchain2.DBA.ToStruct("select Did,Vote_type,Txid,Value,Address,Block_time,Height from chain_vote_cr_info where did = '"+did+"' and (outputlock = 0 or outputlock >= height) and (cancel_height > "+strconv.Itoa(iHeight)+" or cancel_height is null) and height <=  "+strconv.Itoa(iHeight), ret{})
	if err != nil {
		return ResponsePackEx(ELEPHANT_INTERNAL_ERROR, " internal error : "+err.Error())
	}
	if err != nil {
		return ResponsePackEx(ELEPHANT_INTERNAL_ERROR, " internal error : "+err.Error())
	}
	return ResponsePackEx(ELEPHANT_SUCCESS, rst)
}

func str2Float64(sf string) float64 {
	r, err := strconv.ParseFloat(sf, 64)
	if err != nil {
		log.Errorf("Error parsing str to float64 %s", sf)
		return 0
	}
	return r
}

const satoshi = 100000000.0

func CrVoterStatistic(param Params) map[string]interface{} {
	blockchain2.DefaultChainStoreEx.LockDposData()
	defer blockchain2.DefaultChainStoreEx.UnlockDposData()
	addr, ok := param["addr"].(string)
	if !ok || addr == "" || len(addr) != 34 {
		return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, " invalid address ")
	}
	pageNum, _ := param["pageNum"].(string)
	var sql string
	var from int64
	var size int64
	if pageNum != "" {
		pageSize, _ := param["pageSize"].(string)
		if pageSize != "" {
			var err error
			size, err = strconv.ParseInt(pageSize, 10, 64)
			if err != nil {
				return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, err.Error())
			}
		} else {
			size = 10
		}
		num, err := strconv.ParseInt(pageNum, 10, 64)
		if err != nil {
			return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, err.Error())
		}
		if num <= 0 {
			num = 1
		}
		from = (num - 1) * size
	} else {
		return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, " must use pagination ")
	}
	sql = "select * from chain_vote_cr_info where address = '" + addr + "' order by _id desc "
	info, err := blockchain2.DBA.ToStruct(sql, types.Vote_cr_info{})
	if err != nil {
		return ResponsePackEx(ELEPHANT_INTERNAL_ERROR, " internal error : "+err.Error())
	}
	headersContainer := make(map[string]*types.Vote_cr_statistic_header)
	for i := 0; i < len(info); i++ {
		data := info[i].(*types.Vote_cr_info)
		h, ok := headersContainer[data.Txid+strconv.Itoa(data.N)]
		if ok {
			h.Candidate_num += 1
			h.Value = strconv.FormatFloat(math.Round((str2Float64(h.Value)*satoshi+str2Float64(data.Value)*satoshi))/float64(satoshi), 'f', 8, 64)
			h.Candidates = append(h.Candidates, types.Candidates{data.Did, data.Value})
		} else {
			h = new(types.Vote_cr_statistic_header)
			h.Value = data.Value
			h.Candidate_num = 1
			h.Txid = data.Txid
			h.Height = data.Height
			h.Candidates = []types.Candidates{{data.Did, data.Value}}
			h.Block_time = data.Block_time
			h.Is_valid = data.Is_valid
			headersContainer[data.Txid+strconv.Itoa(data.N)] = h
		}
	}
	var voteStatisticSorter types.Vote_cr_statisticSorter
	for _, v := range headersContainer {
		v.Total_num = len(headersContainer)
		voteStatisticSorter = append(voteStatisticSorter, types.Vote_cr_statistic{
			*v,
			[]types.Vote_cr_info{},
		})
	}

	sort.Sort(voteStatisticSorter)
	if !(from == 0 && size == 0) && int(from+1+size) <= len(voteStatisticSorter) {
		voteStatisticSorter = voteStatisticSorter[from : from+size]
	} else if !(from == 0 && size == 0) && int(from+1) <= len(voteStatisticSorter) && int(from+1+size) > len(voteStatisticSorter) {
		voteStatisticSorter = voteStatisticSorter[from:]
	}
	var voteStatistic types.Vote_cr_statisticSorter
	ranklisthoder := make(map[int64][]interface{})
	//height+producer_public_key : index
	ranklisthoderByProducer := make(map[string]int)
	for _, _v := range voteStatisticSorter {
		v := _v.Vote_Header
		rst, ok := ranklisthoder[v.Height]
		if !ok {
			rst, err = blockchain2.DBA.ToStruct(`select m.* from (select ifnull(a.did,b.did) as did , ifnull(a.value,0) as value , b.* from 
chain_cr_candidate_info b left join 
(select A.did , cast(ROUND(sum(value),8) as text) as value from chain_vote_cr_info A where (A.cancel_height > `+strconv.Itoa(int(v.Height))+` or
cancel_height is null) and height <= `+strconv.Itoa(int(v.Height))+` group by did) a on a.did = b.did
order by value * 100000000  desc) m`, types.Vote_cr_info{})
			if err != nil {
				return ResponsePackEx(ELEPHANT_INTERNAL_ERROR, " internal error : "+err.Error())
			}
			for i, r := range rst {
				vi := r.(*types.Vote_cr_info)
				vi.Rank = int64(i + 1)
			}
			for m := 0; m < len(rst); m++ {
				ranklisthoderByProducer[strconv.Itoa(int(v.Height))+rst[m].(*types.Vote_cr_info).Did] = m
			}
		}
		var voteInfos []types.Vote_cr_info
		for _, cand := range v.Candidates {
			voteInfos = append(voteInfos, *rst[ranklisthoderByProducer[strconv.Itoa(int(v.Height))+cand.Did]].(*types.Vote_cr_info))
		}
		voteStatistic = append(voteStatistic, types.Vote_cr_statistic{
			v,
			voteInfos,
		})

	}
	return ResponsePackEx(ELEPHANT_SUCCESS, voteStatistic)
}

func CrCandidateRankByHeight(param Params) map[string]interface{} {
	blockchain2.DefaultChainStoreEx.LockDposData()
	defer blockchain2.DefaultChainStoreEx.UnlockDposData()
	height, ok := param["height"].(string)
	if !ok {
		return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "invalid height")
	}
	h, err := strconv.Atoi(height)
	if err != nil || h < 0 {
		return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "invalid height")
	}
	state, ok := param["state"].(string)
	if state != "all" && state != "active" && state != "pending" &&
		state != "canceled" && state != "returned" {
		return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "state can be one of the folowing values all,active,pending,canceled,returned")
	}
	var rst []interface{}
	if state == "all" {
		rst, err = blockchain2.DBA.ToStruct(`select m.* from (select ifnull(a.did,b.did) as did , ifnull(a.value,0) as value , b.* from
chain_cr_candidate_info b left join 
(select A.did , cast(ROUND(sum(value),8) as text) as value from chain_vote_cr_info A where (A.cancel_height > `+height+` or
cancel_height is null) and height <= `+height+` group by did) a on a.did = b.did
order by votes * 100000000 desc) m `, types.Vote_cr_info{})
	} else {
		rst, err = blockchain2.DBA.ToStruct(`select m.* from (select ifnull(a.did,b.did) as did , ifnull(a.value,0) as value , b.* from
chain_cr_candidate_info b left join 
(select A.did , cast(ROUND(sum(value),8) as text) as value from chain_vote_cr_info A where (A.cancel_height > `+height+` or
cancel_height is null) and height <= `+height+` group by did) a on a.did = b.did where b.state = '`+strings.ToUpper(state[:1])+state[1:]+`'
order by votes * 100000000  desc) m `, types.Vote_cr_info{})
	}
	if err != nil {
		return ResponsePackEx(ELEPHANT_INTERNAL_ERROR, " internal error : "+err.Error())
	}

	for i, r := range rst {
		vi := r.(*types.Vote_cr_info)
		vi.Rank = int64(i + 1)
		vi.Value = vi.Votes
	}

	return ResponsePackEx(ELEPHANT_SUCCESS, rst)
}

func TotalCandidateVoteByHeight(param Params) map[string]interface{} {
	blockchain2.DefaultChainStoreEx.LockDposData()
	defer blockchain2.DefaultChainStoreEx.UnlockDposData()
	height, ok := param["height"].(string)
	if !ok {
		return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "invalid height")
	}
	h, err := strconv.Atoi(height)
	if err != nil || h < 0 {
		return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "invalid height")
	}
	rst, err := blockchain2.DBA.ToFloat(`select ifnull(ROUND(sum(value),8),0) as value from chain_cr_candidate_info b left join chain_vote_cr_info a on a.did = b.did  where (cancel_height > ` + height + ` or cancel_height is null) and height <= ` + height + ``)
	if err != nil {
		return ResponsePackEx(ELEPHANT_INTERNAL_ERROR, " internal error : "+err.Error())
	}

	return ResponsePackEx(ELEPHANT_SUCCESS, rst)
}

func GetCandidateByTxs(param Params) map[string]interface{} {
	blockchain2.DefaultChainStoreEx.LockDposData()
	defer blockchain2.DefaultChainStoreEx.UnlockDposData()
	txids, ok := param["txid"].([]interface{})
	if !ok {
		return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "Can not find txid")
	}
	type ret struct {
		Producer interface{}
		Txid     string
	}
	var rst []ret
	for _, v := range txids {
		txid := v.(string)
		tmp := types.Candidate_info{}
		producer, err := blockchain2.DBA.ToStruct("select b.* from chain_cr_candidate_info b left join chain_vote_cr_info a on a.did = b.did where a.txid = '"+txid+"'", tmp)
		if err != nil {
			return ResponsePackEx(ELEPHANT_INTERNAL_ERROR, " internal error : "+err.Error())
		}
		if len(producer) > 0 && producer[0] != nil {
			rst = append(rst, ret{
				Producer: producer,
				Txid:     txid,
			})
		}
	}
	return ResponsePackEx(ELEPHANT_SUCCESS, rst)
}

func ApiCrVoterListByHeight(param Params) map[string]interface{} {
	blockchain2.DefaultChainStoreEx.LockDposData()
	defer blockchain2.DefaultChainStoreEx.UnlockDposData()
	height, ok := param["height"].(string)
	if !ok {
		return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "invalid height")
	}
	h, err := strconv.Atoi(height)
	if err != nil || h < 0 {
		return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "invalid height")
	}
	stct := types.Cr_voter{}
	rst, err := blockchain2.DBA.ToStruct(`select address from chain_vote_cr_info where (cancel_height > `+height+` or cancel_height is null) and height <= `+height+` group by address `, stct)
	if err != nil {
		return ResponsePackEx(ELEPHANT_INTERNAL_ERROR, " internal error : "+err.Error())
	}

	return ResponsePackEx(ELEPHANT_SUCCESS, rst)
}

func checkRPCServiceLevel(level config.RPCServiceLevel) map[string]interface{} {
	if level < config.RPCServiceLevelFromString(ChainParams.RPCServiceLevel) {
		return ResponsePack(InvalidMethod,
			"requesting method if out of service level")
	}
	return nil
}

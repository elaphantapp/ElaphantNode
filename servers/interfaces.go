package servers

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	blockchain2 "github.com/elastos/Elastos.ELA.Elephant.Node/blockchain"
	common2 "github.com/elastos/Elastos.ELA.Elephant.Node/common"
	"github.com/elastos/Elastos.ELA.Elephant.Node/core/types"
	aux "github.com/elastos/Elastos.ELA/auxpow"
	"github.com/elastos/Elastos.ELA/blockchain"
	"github.com/elastos/Elastos.ELA/common"
	"github.com/elastos/Elastos.ELA/common/config"
	"github.com/elastos/Elastos.ELA/common/log"
	"github.com/elastos/Elastos.ELA/core/contract"
	. "github.com/elastos/Elastos.ELA/core/types"
	"github.com/elastos/Elastos.ELA/core/types/outputpayload"
	"github.com/elastos/Elastos.ELA/core/types/payload"
	"github.com/elastos/Elastos.ELA/crypto"
	"github.com/elastos/Elastos.ELA/dpos"
	"github.com/elastos/Elastos.ELA/dpos/state"
	"github.com/elastos/Elastos.ELA/elanet"
	"github.com/elastos/Elastos.ELA/elanet/pact"
	. "github.com/elastos/Elastos.ELA/errors"
	"github.com/elastos/Elastos.ELA/mempool"
	"github.com/elastos/Elastos.ELA/p2p/msg"
	"github.com/elastos/Elastos.ELA/pow"
	"sort"
	"strconv"
	"strings"
)

var (
	Compile     string
	NodePrivKey []byte
	NodePubKey  []byte
	Config      *config.Configuration
	Chain       *blockchain.BlockChain
	Store       blockchain.IChainStore
	TxMemPool   *mempool.TxPool
	Pow         *pow.Service
	Server      elanet.Server
	Arbiter     *dpos.Arbitrator
	Arbiters    state.Arbitrators
)

func ToReversedString(hash common.Uint256) string {
	return common.BytesToHexString(common.BytesReverse(hash[:]))
}

func FromReversedString(reversed string) ([]byte, error) {
	bytes, err := common.HexStringToBytes(reversed)
	return common.BytesReverse(bytes), err
}

func GetTransactionInfo(header *Header, tx *Transaction) *TransactionInfo {
	inputs := make([]InputInfo, len(tx.Inputs))
	for i, v := range tx.Inputs {
		inputs[i].TxID = ToReversedString(v.Previous.TxID)
		inputs[i].VOut = v.Previous.Index
		inputs[i].Sequence = v.Sequence
	}

	outputs := make([]OutputInfo, len(tx.Outputs))
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

	return &TransactionInfo{
		TxID:           txHashStr,
		Hash:           txHashStr,
		Size:           size,
		VSize:          size,
		Version:        tx.Version,
		LockTime:       tx.LockTime,
		Inputs:         inputs,
		Outputs:        outputs,
		BlockHash:      blockHash,
		Confirmations:  confirmations,
		Time:           time,
		BlockTime:      blockTime,
		TxType:         tx.TxType,
		PayloadVersion: tx.PayloadVersion,
		Payload:        getPayloadInfo(tx.Payload),
		Attributes:     attributes,
		Programs:       programs,
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
		hash, err := Store.GetBlockHash(height)
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
		return ResponsePack(Success, GetTransactionInfo(header, tx))
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
	level, ok := param.Int("level")
	if !ok || level < 0 {
		return ResponsePack(InvalidParams, "level must be an integer in 0-6")
	}

	log.SetPrintLevel(uint8(level))
	return ResponsePack(Success, fmt.Sprint("log level has been set to ", level))
}

func CreateAuxBlock(param Params) map[string]interface{} {
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
		Height:            Store.GetHeight(),
		CoinBaseValue:     block.Transactions[0].Outputs[1].Value,
		Bits:              fmt.Sprintf("%x", block.Header.Bits),
		Hash:              block.Hash().String(),
		PreviousBlockHash: Chain.CurrentBlockHash().String(),
	}
	return ResponsePack(Success, &SendToAux)
}

func SubmitAuxBlock(param Params) map[string]interface{} {
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
		producer := Arbiters.GetCRCProducer(p.PID[:])
		if producer == nil {
			if producer = blockchain.DefaultLedger.Blockchain.GetState().
				GetProducer(p.PID[:]); producer == nil {
				continue
			}
		}
		result = append(result, peerInfo{
			OwnerPublicKey: common.BytesToHexString(producer.OwnerPublicKey()),
			NodePublicKey:  common.BytesToHexString(producer.NodePublicKey()),
			IP:             p.Addr,
			ConnState:      p.State.String(),
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

		CurrentTurnStartHeight: int(Store.GetHeight()) - dutyIndex,
		NextTurnStartHeight: int(Store.GetHeight()) +
			Arbiters.GetArbitersCount() - dutyIndex,
	}
	for _, v := range Arbiters.GetArbitrators() {
		result.Arbiters = append(result.Arbiters, common.BytesToHexString(v))
	}
	for _, v := range Arbiters.GetCandidates() {
		result.Candidates = append(result.Candidates, common.BytesToHexString(v))
	}
	for _, v := range Arbiters.GetNextArbitrators() {
		result.NextArbiters = append(result.NextArbiters,
			common.BytesToHexString(v))
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
		Blocks:        Store.GetHeight(),
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

	//TODO  and description for this rpc-interface
	return ResponsePack(Success, "createauxblock==submitauxblock")
}

func GetMiningInfo(param Params) map[string]interface{} {
	block, err := Store.GetBlock(Store.GetCurrentBlockHash())
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
		Blocks:         Store.GetHeight() + 1,
		CurrentBlockTx: uint32(len(block.Transactions)),
		Difficulty:     Chain.CalcCurrentDifficulty(block.Bits),
		NetWorkHashPS:  Chain.GetNetworkHashPS().String(),
		PooledTx:       uint32(len(TxMemPool.GetTxsInPool())),
		Chain:          Config.ActiveNet,
	}

	return ResponsePack(Success, miningInfo)
}

func ToggleMining(param Params) map[string]interface{} {
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
	txs := make([]*TransactionInfo, 0)
	for _, tx := range TxMemPool.GetTxsInPool() {
		txs = append(txs, GetTransactionInfo(nil, tx))
	}
	return ResponsePack(Success, txs)
}

func GetBlockInfo(block *Block, verbose bool) BlockInfo {
	var txs []interface{}
	if verbose {
		for _, tx := range block.Transactions {
			txs = append(txs, GetTransactionInfo(&block.Header, tx))
		}
	} else {
		for _, tx := range block.Transactions {
			txs = append(txs, ToReversedString(tx.Hash()))
		}
	}
	var versionBytes [4]byte
	binary.BigEndian.PutUint32(versionBytes[:], block.Header.Version)

	var chainWork [4]byte
	binary.BigEndian.PutUint32(chainWork[:], Store.GetHeight()-block.Header.Height)

	nextBlockHash, _ := Store.GetBlockHash(block.Header.Height + 1)

	auxPow := new(bytes.Buffer)
	block.Header.AuxPow.Serialize(auxPow)

	return BlockInfo{
		Hash:              ToReversedString(block.Hash()),
		Confirmations:     Store.GetHeight() - block.Header.Height + 1,
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

func getBlock(hash common.Uint256, verbose uint32) (interface{}, ErrCode) {
	block, err := Store.GetBlock(hash)
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

func getConfirm(hash common.Uint256, verbose uint32) (interface{}, ErrCode) {
	confirm, err := Store.GetConfirm(hash)
	if err != nil {
		return "", UnknownBlock
	}
	if verbose == 0 {
		w := new(bytes.Buffer)
		confirm.Serialize(w)
		return common.BytesToHexString(w.Bytes()), Success
	}

	return GetConfirmInfo(confirm), Success
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

	hash, err := Store.GetBlockHash(height)
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
		return ResponsePack(err.(ErrCode), err.Error())
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
	return ResponsePack(Success, Store.GetHeight())
}

func CurrHeight(param Params) map[string]interface{} {
	return ResponsePackEx(ELEPHANT_SUCCESS, Store.GetHeight())
}

func GetNodeFee(param Params) map[string]interface{} {
	return ResponsePackEx(ELEPHANT_SUCCESS, config.Parameters.PowConfiguration.MinTxFee)
}

func GetBestBlockHash(param Params) map[string]interface{} {
	hash, err := Store.GetBlockHash(Store.GetHeight())
	if err != nil {
		return ResponsePack(InvalidParams, "")
	}
	return ResponsePack(Success, ToReversedString(hash))
}

func GetBlockCount(param Params) map[string]interface{} {
	return ResponsePack(Success, Store.GetHeight()+1)
}

func GetBlockHash(param Params) map[string]interface{} {
	height, ok := param.Uint("height")
	if !ok {
		return ResponsePack(InvalidParams, "height parameter should be a positive integer")
	}

	hash, err := Store.GetBlockHash(height)
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

	hash, err := Store.GetBlockHash(height)
	if err != nil {
		return ResponsePack(UnknownBlock, "")

	}
	block, err := Store.GetBlock(hash)
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

	hash, err := Store.GetBlockHash(height)
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

	hash, err := Store.GetBlockHash(height)
	if err != nil {
		return ResponsePack(UnknownBlock, "not found block hash at given height")
	}

	block, _ := Store.GetBlock(hash)
	if block == nil {
		return ResponsePack(InternalError, "not found block at given height")
	}

	var arbitrators []string
	for _, data := range Arbiters.GetArbitrators() {
		arbitrators = append(arbitrators, common.BytesToHexString(data))
	}

	result := ArbitratorGroupInfo{
		OnDutyArbitratorIndex: Arbiters.GetDutyIndexByHeight(height),
		Arbitrators:           arbitrators,
	}

	return ResponsePack(Success, result)
}

//Asset
func GetAssetByHash(param Params) map[string]interface{} {
	str, ok := param.String("hash")
	if !ok {
		return ResponsePack(InvalidParams, "")
	}
	hashBytes, err := FromReversedString(str)
	if err != nil {
		return ResponsePack(InvalidParams, "")
	}
	var hash common.Uint256
	err = hash.Deserialize(bytes.NewReader(hashBytes))
	if err != nil {
		return ResponsePack(InvalidAsset, "")
	}
	asset, err := Store.GetAsset(hash)
	if err != nil {
		return ResponsePack(UnknownAsset, "")
	}
	if false {
		w := new(bytes.Buffer)
		asset.Serialize(w)
		return ResponsePack(Success, common.BytesToHexString(w.Bytes()))
	}
	return ResponsePack(Success, asset)
}

func GetBalanceByAddr(param Params) map[string]interface{} {
	str, ok := param.String("addr")
	if !ok {
		return ResponsePack(InvalidParams, "")
	}

	programHash, err := common.Uint168FromAddress(str)
	if err != nil {
		return ResponsePack(InvalidParams, "")
	}
	unspends, err := Store.GetUnspentsFromProgramHash(*programHash)
	var balance common.Fixed64 = 0
	for _, u := range unspends {
		for _, v := range u {
			balance = balance + v.Value
		}
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
		return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "")
	}
	unspends, err := Store.GetUnspentsFromProgramHash(*programHash)
	var balance common.Fixed64 = 0
	for _, u := range unspends {
		for _, v := range u {
			balance = balance + v.Value
		}
	}
	return ResponsePackEx(ELEPHANT_SUCCESS, balance.String())
}

func GetBalanceByAsset(param Params) map[string]interface{} {
	addr, ok := param.String("addr")
	if !ok {
		return ResponsePack(InvalidParams, "")
	}

	programHash, err := common.Uint168FromAddress(addr)
	if err != nil {
		return ResponsePack(InvalidParams, "")
	}

	assetIDStr, ok := param.String("assetid")
	if !ok {
		return ResponsePack(InvalidParams, "")
	}
	assetIDBytes, err := FromReversedString(assetIDStr)
	if err != nil {
		return ResponsePack(InvalidParams, "")
	}
	assetID, err := common.Uint256FromBytes(assetIDBytes)
	if err != nil {
		return ResponsePack(InvalidParams, "")
	}

	unspents, err := Store.GetUnspentsFromProgramHash(*programHash)
	var balance common.Fixed64 = 0
	for k, u := range unspents {
		for _, v := range u {
			if assetID.IsEqual(k) {
				balance = balance + v.Value
			}
		}
	}
	return ResponsePack(Success, balance.String())
}

func GetReceivedByAddress(param Params) map[string]interface{} {
	address, ok := param.String("address")
	if !ok {
		return ResponsePack(InvalidParams, "need a parameter named address")
	}
	programHash, err := common.Uint168FromAddress(address)
	if err != nil {
		return ResponsePack(InvalidParams, "Invalid address: "+address)
	}
	UTXOsWithAssetID, err := Store.GetUnspentsFromProgramHash(*programHash)
	if err != nil {
		return ResponsePack(InvalidParams, err)
	}
	UTXOs := UTXOsWithAssetID[config.ELAAssetID]
	var totalValue common.Fixed64
	for _, unspent := range UTXOs {
		totalValue += unspent.Value
	}

	return ResponsePack(Success, totalValue.String())
}

func GetUTXOsByAmount(param Params) map[string]interface{} {
	bestHeight := Store.GetHeight()

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
		return ResponsePack(InvalidParams, "invalid address: "+address)
	}
	unspents, err := Store.GetUnspentsFromProgramHash(*programHash)
	if err != nil {
		return ResponsePack(InvalidParams, "cannot get asset with program")
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
	totalAmount := common.Fixed64(0)
	for _, unspent := range unspents[config.ELAAssetID] {
		if totalAmount >= *amount {
			break
		}
		tx, height, err := Store.GetTransaction(unspent.TxID)
		if err != nil {
			return ResponsePack(InternalError, "unknown transaction "+
				unspent.TxID.String()+" from persisted utxo")
		}
		if utxoType == "vote" && (tx.Version < TxVersion09 ||
			tx.Version >= TxVersion09 && tx.Outputs[unspent.Index].Type != OTVote) {
			continue
		}
		if utxoType == "normal" && tx.Version >= TxVersion09 &&
			tx.Outputs[unspent.Index].Type == OTVote {
			continue
		}
		if tx.TxType == CoinBase && bestHeight-height < config.DefaultParams.CoinbaseMaturity {
			continue
		}
		totalAmount += unspent.Value
		result = append(result, UTXOInfo{
			TxType:        byte(tx.TxType),
			TxID:          ToReversedString(unspent.TxID),
			AssetID:       ToReversedString(config.ELAAssetID),
			VOut:          unspent.Index,
			Amount:        unspent.Value.String(),
			Address:       address,
			OutputLock:    tx.Outputs[unspent.Index].OutputLock,
			Confirmations: bestHeight - height + 1,
		})
	}

	if totalAmount < *amount {
		return ResponsePack(InternalError, "not enough utxo")
	}

	return ResponsePack(Success, result)
}

func GetAmountByInputs(param Params) map[string]interface{} {
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
	bestHeight := Store.GetHeight()

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
			return ResponsePack(InvalidParams, "Invalid address: "+address)
		}
		unspents, err := Store.GetUnspentsFromProgramHash(*programHash)
		if err != nil {
			return ResponsePack(InvalidParams, "cannot get asset with program")
		}

		for _, unspent := range unspents[config.ELAAssetID] {
			tx, height, err := Store.GetTransaction(unspent.TxID)
			if err != nil {
				return ResponsePack(InternalError,
					"unknown transaction "+unspent.TxID.String()+" from persisted utxo")
			}
			if utxoType == "vote" && (tx.Version < TxVersion09 ||
				tx.Version >= TxVersion09 && tx.Outputs[unspent.Index].Type != OTVote) {
				continue
			}
			if utxoType == "normal" && tx.Version >= TxVersion09 && tx.Outputs[unspent.Index].Type == OTVote {
				continue
			}
			if unspent.Value == 0 {
				continue
			}
			result = append(result, UTXOInfo{
				TxType:        byte(tx.TxType),
				TxID:          ToReversedString(unspent.TxID),
				AssetID:       ToReversedString(config.ELAAssetID),
				VOut:          unspent.Index,
				Amount:        unspent.Value.String(),
				Address:       address,
				OutputLock:    tx.Outputs[unspent.Index].OutputLock,
				Confirmations: bestHeight - height + 1,
			})
		}
	}
	return ResponsePack(Success, result)
}

func GetUnspends(param Params) map[string]interface{} {
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
		Index uint32 `json:"Index"`
		Value string `json:"Value"`
	}
	type Result struct {
		AssetID   string            `json:"AssetId"`
		AssetName string            `json:"AssetName"`
		Utxo      []UTXOUnspentInfo `json:"Utxo"`
	}
	var results []Result
	unspends, err := Store.GetUnspentsFromProgramHash(*programHash)

	for k, u := range unspends {
		asset, err := Store.GetAsset(k)
		if err != nil {
			return ResponsePack(InternalError, "")
		}
		var unspendsInfo []UTXOUnspentInfo
		for _, v := range u {
			unspendsInfo = append(unspendsInfo, UTXOUnspentInfo{ToReversedString(v.TxID), v.Index, v.Value.String()})
		}
		results = append(results, Result{ToReversedString(k), asset.Name, unspendsInfo})
	}
	return ResponsePack(Success, results)
}

func GetUnspendOutput(param Params) map[string]interface{} {
	addr, ok := param.String("addr")
	if !ok {
		return ResponsePack(InvalidParams, "")
	}
	programHash, err := common.Uint168FromAddress(addr)
	if err != nil {
		return ResponsePack(InvalidParams, "")
	}
	assetID, ok := param.String("assetid")
	if !ok {
		return ResponsePack(InvalidParams, "")
	}
	bys, err := FromReversedString(assetID)
	if err != nil {
		return ResponsePack(InvalidParams, "")
	}

	var assetHash common.Uint256
	if err := assetHash.Deserialize(bytes.NewReader(bys)); err != nil {
		return ResponsePack(InvalidParams, "")
	}
	type UTXOUnspentInfo struct {
		TxID  string `json:"Txid"`
		Index uint32 `json:"Index"`
		Value string `json:"Value"`
	}
	infos, err := Store.GetUnspentFromProgramHash(*programHash, assetHash)
	if err != nil {
		return ResponsePack(InvalidParams, "")

	}
	var UTXOoutputs []UTXOUnspentInfo
	for _, v := range infos {
		UTXOoutputs = append(UTXOoutputs, UTXOUnspentInfo{TxID: ToReversedString(v.TxID), Index: v.Index, Value: v.Value.String()})
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
		bHash, err := Store.GetBlockHash(height)
		if err != nil {
			return ResponsePackEx(ELEPHANT_INTERNAL_ERROR, "")
		}
		header, err = Chain.GetHeader(bHash)
		if err != nil {
			return ResponsePackEx(ELEPHANT_INTERNAL_ERROR, "")
		}
	}

	return ResponsePackEx(ELEPHANT_SUCCESS, GetTransactionInfo(header, txn))
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
	bHash, err := Store.GetBlockHash(height)
	if err != nil {
		return ResponsePack(UnknownBlock, "")
	}
	header, err := Chain.GetHeader(bHash)
	if err != nil {
		return ResponsePack(UnknownBlock, "")
	}

	return ResponsePack(Success, GetTransactionInfo(header, txn))
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

type Producer struct {
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

type Producers struct {
	Producers   []Producer `json:"producers"`
	TotalVotes  string     `json:"totalvotes"`
	TotalCounts uint64     `json:"totalcounts"`
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

	var ps []Producer
	var totalVotes common.Fixed64
	for i, p := range producers {
		totalVotes += p.Votes()
		producer := Producer{
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
		ps = append(ps, producer)
	}

	count := int64(len(producers))
	if limit < 0 {
		limit = count
	}
	var resultPs []Producer
	if start < count {
		end := start
		if start+limit <= count {
			end = start + limit
		} else {
			end = count
		}
		resultPs = append(resultPs, ps[start:end]...)
	}

	result := &Producers{
		Producers:   resultPs,
		TotalVotes:  totalVotes.String(),
		TotalCounts: uint64(count),
	}

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
	unspents, err := Store.GetUnspentsFromProgramHash(*programHash)
	if err != nil {
		return ResponsePack(InvalidParams, "cannot get asset with program")
	}

	var total common.Fixed64
	var voting common.Fixed64
	for _, unspent := range unspents[config.ELAAssetID] {
		tx, _, err := Store.GetTransaction(unspent.TxID)
		if err != nil {
			return ResponsePack(InternalError, "unknown transaction "+unspent.TxID.String()+" from persisted utxo")
		}
		if tx.Outputs[unspent.Index].Type == OTVote {
			voting += unspent.Value
		}
		total += unspent.Value
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
	unspends, err := Store.GetUnspentsFromProgramHash(*programHash)
	var balance common.Fixed64 = 0
	for _, u := range unspends {
		for _, v := range u {
			balance = balance + v.Value
		}
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

func EstimateSmartFee(param Params) map[string]interface{} {
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

func getPayloadInfo(p Payload) PayloadInfo {
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
	case *payload.ActivateProducer:
		obj := new(ActivateProducerInfo)
		obj.NodePublicKey = common.BytesToHexString(object.NodePublicKey)
		obj.Signature = common.BytesToHexString(object.Signature)
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
			for _, candidate := range content.Candidates {
				contentInfo.CandidatesInfo = append(contentInfo.CandidatesInfo, common.BytesToHexString(candidate))
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

func ResponsePack(errCode ErrCode, result interface{}) map[string]interface{} {
	if errCode != 0 && (result == "" || result == nil) {
		result = ErrMap[errCode]
	}
	return map[string]interface{}{"Result": result, "Error": errCode}
}

func ResponsePackEx(errCode ErrCode, result interface{}) map[string]interface{} {
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
	inputs, ok := param["inputs"].([]interface{})
	if !ok {
		return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "Can not find inputs")
	}
	var utxoList [][]*blockchain.UTXO
	for _, v := range inputs {
		input, ok := v.(string)
		if !ok {
			return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "Not valid input value")
		}
		programhash, err := common.Uint168FromAddress(input)
		if err != nil {
			return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "Invalid address")
		}
		assetIDBytes, _ := FromReversedString("a3d0eaa466df74983b5d7c543de6904f4c9418ead5ffd6d25814234a96db37b0")
		assetID, err := common.Uint256FromBytes(assetIDBytes)
		if err != nil {
			return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "")
		}
		utxos, err := blockchain2.DefaultChainStoreEx.GetUnspentFromProgramHash(*programhash, *assetID)
		if err != nil {
			return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "Internal error")
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
		for j, utxo := range utxos {
			index = j
			spendMoney += int64(utxo.Value)
			if (j+1)%bundleUtxoSize == 0 {
				multiTxNum = (j + 1) / bundleUtxoSize
			} else {
				multiTxNum = (j+1)/bundleUtxoSize + 1
			}
			if spendMoney >= smAmt+int64(config.Parameters.PowConfiguration.MinTxFee*multiTxNum) {
				break
			}
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
				orgMsg += input["txid"].(string) + "-" + strconv.Itoa(int(input["index"].(uint32)))
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
			hasEnoughFee = true
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
	inputs, ok := param["inputs"].([]interface{})
	if !ok {
		return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "Can not find inputs")
	}
	var utxoList [][]*blockchain.UTXO
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
		programhash, err := common.Uint168FromAddress(input)
		if err != nil {
			return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "Invalid address")
		}
		assetIDBytes, _ := FromReversedString("a3d0eaa466df74983b5d7c543de6904f4c9418ead5ffd6d25814234a96db37b0")
		assetID, err := common.Uint256FromBytes(assetIDBytes)
		if err != nil {
			return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "")
		}
		utxos, err := blockchain2.DefaultChainStoreEx.GetUnspentFromProgramHash(*programhash, *assetID)
		if err != nil {
			return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "Internal error")
		}
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
		utxoList = append(utxoList, utxos)
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
				break
			}
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
				orgMsg += input["txid"].(string) + "-" + strconv.Itoa(int(input["index"].(uint32)))
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
			hasEnoughFee = true
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

func GetCmcPrice(param Params) map[string]interface{} {
	limit, ok := param["limit"].(string)
	l := 0
	var err error
	if !ok {
		l = 2000
	} else {
		l, err = strconv.Atoi(limit)
		if err != nil {
			return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "")
		}
		if l > 2000 || l <= 0 {
			l = 2000
		}
	}
	cmcs := blockchain2.DefaultChainStoreEx.GetCmcPrice()
	if len(cmcs.C) < l {
		return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, " Cmc Price is not ready yet")
	}
	cmcs = types.Cmcs{
		C: cmcs.C[:int64(l)],
	}
	return ResponsePackEx(ELEPHANT_SUCCESS, cmcs.C)
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

func GetSpendUtxos(param Params) map[string]interface{} {
	inputs, ok := param["UTXOInputs"].([]interface{})
	if !ok {
		return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "Can not find UTXOInputs")
	}
	var total common.Fixed64
	for _, input := range inputs {
		i, ok := input.(map[string]interface{})
		if !ok {
			return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "Invalid request param")
		}
		index, ok := i["index"].(float64)
		if !ok {
			return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "Invalid request param")
		}
		txid, ok := i["txid"].(string)
		if !ok {
			return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "Invalid request param")
		}
		reverseTxid, err := common2.ReverseHexString(txid)
		if err != nil {
			return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "Invalid request param")
		}
		nativeTxid, err := common.Uint256FromHexString(reverseTxid)
		if err != nil {
			return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "Invalid request param")
		}
		utxo, err := Store.GetUnspent(*nativeTxid, uint16(index))
		if err != nil {
			return ResponsePackEx(ELEPHANT_ERR_BAD_REQUEST, "Invalid utxo , txid "+txid+", index "+strconv.Itoa(int(index)))
		}
		total += utxo.Value
	}
	return ResponsePackEx(ELEPHANT_SUCCESS, total)
}

func NodeRewardAddr(param Params) map[string]interface{} {
	return ResponsePackEx(ELEPHANT_SUCCESS, config.Parameters.PowConfiguration.PayToAddr)
}

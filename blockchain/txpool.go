package blockchain

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	common2 "github.com/elastos/Elastos.ELA.Elephant.Node/common"
	"github.com/elastos/Elastos.ELA.Elephant.Node/core/types"
	"github.com/elastos/Elastos.ELA/common"
	"github.com/elastos/Elastos.ELA/common/log"
	. "github.com/elastos/Elastos.ELA/core/types"
	"github.com/elastos/Elastos.ELA/core/types/outputpayload"
	"github.com/elastos/Elastos.ELA/core/types/payload"
	"github.com/elastos/Elastos.ELA/crypto"
	"io"
	"strconv"
	"strings"
)

var DefaultMemPool MemPool

type MemPool struct {
	i    int
	c    IChainStoreExtend
	is_p map[common.Uint256]bool
	p    map[string][]byte
}

func (m *MemPool) AppendToMemPool(tx *Transaction) error {
	if _, ok := m.is_p[tx.Hash()]; ok {
		return nil
	}
	txhs := make([]types.TransactionHistory, 0)
	var signedAddress string
	var node_fee common.Fixed64
	var node_output_index uint64 = 999999
	var memo []byte
	for _, attr := range tx.Attributes {
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
						for i, input := range tx.Inputs {
							hash := input.Previous.TxID
							orgMsg += common.BytesToHexString(common.BytesReverse(hash[:])) + "-" + strconv.Itoa(int(input.Previous.Index))
							if i != len(tx.Inputs)-1 {
								orgMsg += ";"
							}
						}
						orgMsg += "&"
						for i, output := range tx.Outputs {
							address, _ := output.ProgramHash.ToAddress()
							orgMsg += address + "-" + fmt.Sprintf("%d", output.Value)
							if i != len(tx.Outputs)-1 {
								orgMsg += ";"
							}
						}
						orgMsg += "&"
						orgMsg += fmt.Sprintf("%d", tx.Fee)
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

	isCrossTx := false
	if tx.TxType == TransferCrossChainAsset {
		isCrossTx = true
	}
	if m.isVoteTx(tx) {
		tx.TxType = types.Vote
	}
	spend := make(map[common.Uint168]int64)
	var totalInput int64 = 0
	var from []common.Uint168
	var to []common.Uint168
	for _, input := range tx.Inputs {
		txid := input.Previous.TxID
		index := input.Previous.Index
		referTx, _, err := m.c.GetTransaction(txid)
		if err != nil {
			return err
		}
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
	vote := outputpayload.VoteOutput{}
	for i, output := range tx.Outputs {
		outputPayload := output.Payload
		if tx.TxType != types.Vote && outputPayload != nil && outputPayload.Validate() == nil {
			var buf bytes.Buffer
			err := outputPayload.Deserialize(&buf)
			if err == nil || err == io.EOF {
				err = vote.Serialize(&buf)
				if err == nil || err == io.EOF {
					tx.TxType = types.Vote
				}
			}
		}

		address, _ := output.ProgramHash.ToAddress()
		var valueCross int64
		if isCrossTx == true && (output.ProgramHash == MINING_ADDR || strings.Index(address, "X") == 0 || address == "4oLvT2") {
			switch pl := tx.Payload.(type) {
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
		transferType := INCOME
		s, ok := spend[k]
		var value int64
		if ok {
			if s > r {
				value = s - r
				transferType = SPEND
			} else {
				value = r - s
			}
			delete(spend, k)
		} else {
			value = r
		}
		var realFee uint64 = uint64(fee)
		var rto = to
		if transferType == INCOME {
			realFee = 0
			rto = []common.Uint168{k}
		}

		if transferType == SPEND {
			from = []common.Uint168{k}
		}

		txh := types.TransactionHistory{}
		txh.Value = uint64(value)
		txh.Address = k
		txh.Inputs = from
		txh.TxType = tx.TxType
		txh.Txid = tx.Hash()
		txh.Height = 0
		txh.CreateTime = 0
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
		txh.Status = 1
		txhs = append(txhs, txh)
	}

	for k, r := range spend {
		txh := types.TransactionHistory{}
		txh.Value = uint64(r)
		txh.Address = k
		txh.Inputs = []common.Uint168{k}
		txh.TxType = tx.TxType
		txh.Txid = tx.Hash()
		txh.Height = 0
		txh.CreateTime = 0
		txh.Type = []byte(SPEND)
		txh.Fee = uint64(fee)
		txh.NodeFee = uint64(node_fee)
		txh.NodeOutputIndex = uint64(node_output_index)
		if len(to) > 10 {
			txh.Outputs = to[0:10]
		} else {
			txh.Outputs = to
		}
		txh.Memo = memo
		txh.Status = 1
		txhs = append(txhs, txh)
	}
	for _, p := range txhs {
		m.is_p[p.Txid] = true
		m.i += m.i
		err := m.store(p.Txid, p)
		if err != nil {
			return err
		}
	}

	return nil
}

func (m *MemPool) isVoteTx(tx *Transaction) bool {
	version := tx.Version
	if version == 0x09 {
		vout := tx.Outputs
		for _, v := range vout {
			if v.Type == 0x01 && v.AssetID == *ELA_ASSET {
				return true
			}
		}
	}
	return false
}

func (m *MemPool) store(txid common.Uint256, history types.TransactionHistory) error {
	addr, _ := history.Address.ToAddress()
	value := new(bytes.Buffer)
	history.Serialize(value)
	m.p[addr+txid.String()+strconv.Itoa(m.i)] = value.Bytes()
	return nil
}

func (m *MemPool) GetMemPoolTx(address *common.Uint168) (ret []types.TransactionHistoryDisplay) {

	for k, v := range m.p {
		addr, err := address.ToAddress()
		if err != nil {
			log.Warnf("Warn invalid address %s", addr)
			return
		}
		if strings.Contains(k, addr) {
			buf := new(bytes.Buffer)
			buf.Write(v)
			txh := types.TransactionHistory{}
			txhd, _ := txh.Deserialize(buf)
			ret = append(ret, *txhd)
		}
	}

	return
}

func (m *MemPool) DeleteMemPoolTx(txid common.Uint256) {

	for k := range m.p {
		if strings.Contains(k, txid.String()) {
			delete(m.p, k)
			delete(m.is_p, txid)
		}
	}

}

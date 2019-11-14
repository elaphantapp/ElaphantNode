package blockchain

import (
	"bytes"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/elastos/Elastos.ELA.Elephant.Node/common"
	"github.com/elastos/Elastos.ELA.Elephant.Node/core/types"
	. "github.com/elastos/Elastos.ELA/blockchain"
	common2 "github.com/elastos/Elastos.ELA/common"
	"github.com/elastos/Elastos.ELA/common/log"
	. "github.com/elastos/Elastos.ELA/core/types"
	"github.com/elastos/Elastos.ELA/core/types/outputpayload"
	"github.com/elastos/Elastos.ELA/core/types/payload"
	"github.com/elastos/Elastos.ELA/crypto"
	_ "github.com/mattn/go-sqlite3"
	"github.com/robfig/cron"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	INCOME                      string = "income"
	SPEND                       string = "spend"
	ELA                         uint64 = 100000000
	DPOS_CHECK_POINT                   = 290000
	CHECK_POINT_ROLLBACK_HEIGHT        = 100
)

var (
	MINING_ADDR  = common2.Uint168{}
	ELA_ASSET, _ = common2.Uint256FromHexString("b037db964a231458d2d6ffd5ea18944c4f90e63d547c5d3b9874df66a4ead0a3")
	DBA          *common.Dba
)

type ChainStoreExtend struct {
	IChainStore
	IStore
	chain    *BlockChain
	taskChEx chan interface{}
	quitEx   chan chan bool
	mu       sync.RWMutex
	*cron.Cron
	rp         chan bool
	checkPoint bool
}

func (c *ChainStoreExtend) AddTask(task interface{}) {
	c.taskChEx <- task
}

func NewChainStoreEx(chain *BlockChain, chainstore IChainStore, filePath string) (*ChainStoreExtend, error) {
	st, err := NewLevelDB(filePath)
	if err != nil {
		return nil, err
	}
	DBA, err = common.NewInstance(filePath)
	if err != nil {
		log.Fatal(err)
		return nil, err
	}
	err = common.InitDb(DBA)
	if err != nil {
		log.Fatal(err)
		return nil, err
	}
	c := &ChainStoreExtend{
		IChainStore: chainstore,
		IStore:      st,
		chain:       chain,
		taskChEx:    make(chan interface{}, 100),
		quitEx:      make(chan chan bool, 1),
		Cron:        cron.New(),
		mu:          sync.RWMutex{},
		rp:          make(chan bool, 1),
		checkPoint:  true,
	}
	DefaultChainStoreEx = c
	DefaultMemPool = MemPool{
			c:    DefaultChainStoreEx,
			is_p: make(map[common2.Uint256]bool),
			p:    make(map[string][]byte),
	}
	go c.loop()
	go c.initTask()
	return c, nil
}

func (c *ChainStoreExtend) Close() {

}

func (c *ChainStoreExtend) processVote(block *Block, voteTxHolder *map[string]TxType) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if block.Height >= DPOS_CHECK_POINT {
		db, err := DBA.Begin()
		if err != nil {
			return err
		}
		err = doProcessVote(block, voteTxHolder, db)
		if err != nil {
			db.Rollback()
			return err
		} else {
			err = db.Commit()
			if err != nil {
				return err
			}
		}
	}
	if len(c.rp) > 0 {
		c.renewProducer()
		<-c.rp
	}
	return nil
}

func doProcessVote(block *Block, voteTxHolder *map[string]TxType, db *sql.Tx) error {
	for i := 0; i < len(block.Transactions); i++ {
		tx := block.Transactions[i]
		version := tx.Version
		txid, err := common.ReverseHexString(tx.Hash().String())
		if err != nil {
			return err
		}
		if version == 0x09 {
			vout := tx.Outputs
			stmt, err := db.Prepare("insert into chain_vote_info (producer_public_key,vote_type,txid,n,`value`,outputlock,address,block_time,height) values(?,?,?,?,?,?,?,?,?)")
			if err != nil {
				return err
			}
			for n, v := range vout {
				if v.Type == 0x01 && v.AssetID == *ELA_ASSET {
					payload, ok := v.Payload.(*outputpayload.VoteOutput)
					if !ok || payload == nil {
						continue
					}
					contents := payload.Contents
					if !ok {
						continue
					}
					value := v.Value.String()
					address, err := v.ProgramHash.ToAddress()
					if err != nil {
						return err
					}
					outputlock := v.OutputLock
					for _, cv := range contents {
						votetype := cv.VoteType
						votetypeStr := ""
						if votetype == 0x00 {
							votetypeStr = "Delegate"
						} else if votetype == 0x01 {
							votetypeStr = "CRC"
						}
						candidates := cv.Candidates
						for _, pub := range candidates {
							_, err := stmt.Exec(common2.BytesToHexString(pub), votetypeStr, txid, n, value, outputlock, address, block.Header.Timestamp, block.Header.Height)
							if err != nil {
								return err
							}
							(*voteTxHolder)[txid] = types.Vote
						}
					}
				}
			}
			stmt.Close()
		}
		// remove canceled vote
		vin := tx.Inputs
		prepStat, err := db.Prepare("select * from chain_vote_info where txid = ? and n = ?")
		if err != nil {
			return err
		}
		stmt, err := db.Prepare("update chain_vote_info set is_valid = 'NO',cancel_height=? where txid = ? and n = ? ")
		if err != nil {
			return err
		}
		for _, v := range vin {
			txhash, _ := common.ReverseHexString(v.Previous.TxID.String())
			vout := v.Previous.Index
			r, err := prepStat.Query(txhash, vout)
			if err != nil {
				return err
			}
			if r.Next() {
				_, err = stmt.Exec(block.Header.Height, txhash, vout)
				if err != nil {
					return err
				}
			}
		}
		stmt.Close()
	}
	return nil
}

func (c *ChainStoreExtend) assembleRollbackBlock(rollbackStart uint32, blk *Block, blocks *[]*Block) error {
	for i := rollbackStart; i < blk.Height; i++ {
		blockHash, err := c.GetBlockHash(i)
		if err != nil {
			return err
		}
		b, err := c.GetBlock(blockHash)
		if err != nil {
			return err
		}
		*blocks = append(*blocks, b)
	}
	return nil
}

func (c *ChainStoreExtend) persistTxHistory(blk *Block) error {
	var blocks []*Block
	var rollbackStart uint32 = 0
	if c.checkPoint {
		bestHeight, err := c.GetBestHeightExt()
		if err == nil && bestHeight > CHECK_POINT_ROLLBACK_HEIGHT {
			rollbackStart = bestHeight - CHECK_POINT_ROLLBACK_HEIGHT
		}
		c.assembleRollbackBlock(rollbackStart, blk, &blocks)
		c.checkPoint = false
	} else if blk.Height > DPOS_CHECK_POINT {
		rollbackStart = blk.Height - 5
		c.assembleRollbackBlock(rollbackStart, blk, &blocks)
	}

	blocks = append(blocks, blk)

	for _, block := range blocks {
		_, err := c.GetStoredHeightExt(block.Height)
		if err == nil {
			continue
		}
		//process vote
		voteTxHolder := make(map[string]TxType)
		err = c.processVote(block, &voteTxHolder)
		if err != nil {
			return err
		}
		txs := block.Transactions
		txhs := make([]types.TransactionHistory, 0)
		pubks := make(map[common2.Uint168][]byte)
		dposReward := make(map[common2.Uint168]common2.Fixed64)

		for i := 0; i < len(txs); i++ {
			tx := txs[i]
			txid, err := common.ReverseHexString(tx.Hash().String())
			if err != nil {
				return err
			}
			var memo []byte
			var signedAddress string
			var node_fee common2.Fixed64
			var node_output_index uint64 = 999999

			for _, attr := range tx.Attributes {
				if attr.Usage == Memo {
					memo = attr.Data
				}
				if attr.Usage == Description {
					am := make(map[string]interface{})
					err = json.Unmarshal(attr.Data, &am)
					if err == nil {
						pm, ok := am["Postmark"]
						if ok {
							dpm, ok := pm.(map[string]interface{})
							if ok {
								var orgMsg string
								for i, input := range tx.Inputs {
									hash := input.Previous.TxID
									orgMsg += common2.BytesToHexString(common2.BytesReverse(hash[:])) + "-" + strconv.Itoa(int(input.Previous.Index))
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
									signedAddress, err = common.GetAddress(b_pub)
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

			if tx.TxType == CoinBase {
				var to []common2.Uint168
				hold := make(map[common2.Uint168]uint64)
				txhscoinbase := make([]types.TransactionHistory, 0)
				for i, vout := range tx.Outputs {
					if !common.ContainsU168(vout.ProgramHash, to) {
						to = append(to, vout.ProgramHash)
						txh := types.TransactionHistory{}
						txh.Address = vout.ProgramHash
						txh.Inputs = []common2.Uint168{MINING_ADDR}
						txh.TxType = tx.TxType
						txh.Txid = tx.Hash()
						txh.Height = uint64(block.Height)
						txh.CreateTime = uint64(block.Header.Timestamp)
						txh.Type = []byte(INCOME)
						txh.Fee = 0
						txh.Memo = memo
						txh.NodeFee = 0
						txh.NodeOutputIndex = uint64(node_output_index)
						hold[vout.ProgramHash] = uint64(vout.Value)
						txhscoinbase = append(txhscoinbase, txh)
					} else {
						hold[vout.ProgramHash] += uint64(vout.Value)
					}
					// dpos reward
					if i > 1 {
						dposReward[vout.ProgramHash] = vout.Value
					}
				}
				for i := 0; i < len(txhscoinbase); i++ {
					txhscoinbase[i].Outputs = []common2.Uint168{txhscoinbase[i].Address}
					txhscoinbase[i].Value = hold[txhscoinbase[i].Address]
				}
				txhs = append(txhs, txhscoinbase...)
			} else {
				for _, program := range tx.Programs {
					code := program.Code
					programHash, err := common.GetProgramHash(code[1 : len(code)-1])
					if err != nil {
						continue
					}
					pubks[*programHash] = code[1 : len(code)-1]
				}

				isCrossTx := false
				if tx.TxType == TransferCrossChainAsset {
					isCrossTx = true
				}
				if voteTxHolder[txid] == types.Vote {
					tx.TxType = types.Vote
				}
				spend := make(map[common2.Uint168]int64)
				var totalInput int64 = 0
				var from []common2.Uint168
				var to []common2.Uint168
				for _, input := range tx.Inputs {
					txid := input.Previous.TxID
					index := input.Previous.Index
					referTx, _, err := c.GetTransaction(txid)
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
					if !common.ContainsU168(address, from) {
						from = append(from, address)
					}
				}
				receive := make(map[common2.Uint168]int64)
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
					if !common.ContainsU168(output.ProgramHash, to) {
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
						rto = []common2.Uint168{k}
					}

					if transferType == SPEND {
						from = []common2.Uint168{k}
					}

					txh := types.TransactionHistory{}
					txh.Value = uint64(value)
					txh.Address = k
					txh.Inputs = from
					txh.TxType = tx.TxType
					txh.Txid = tx.Hash()
					txh.Height = uint64(block.Height)
					txh.CreateTime = uint64(block.Header.Timestamp)
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
					txhs = append(txhs, txh)
				}

				for k, r := range spend {
					txh := types.TransactionHistory{}
					txh.Value = uint64(r)
					txh.Address = k
					txh.Inputs = []common2.Uint168{k}
					txh.TxType = tx.TxType
					txh.Txid = tx.Hash()
					txh.Height = uint64(block.Height)
					txh.CreateTime = uint64(block.Header.Timestamp)
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
					txhs = append(txhs, txh)
				}
			}
		}
		c.persistTransactionHistory(txhs)
		c.persistPbks(pubks)
		c.persistDposReward(dposReward, block.Height)
		c.persistBestHeight(block.Height)
		c.persistStoredHeight(block.Height)
	}
	return nil
}

func (c *ChainStoreExtend) CloseEx() {
	closed := make(chan bool)
	c.quitEx <- closed
	<-closed
	c.Stop()
	log.Info("Extend chainStore shutting down")
}

func (c *ChainStoreExtend) loop() {
	for {
		select {
		case t := <-c.taskChEx:
			now := time.Now()
			switch kind := t.(type) {
			case *Block:
				err := c.persistTxHistory(kind)
				if err != nil {
					log.Errorf("Error persist transaction history %s", err.Error())
				}
				tcall := float64(time.Now().Sub(now)) / float64(time.Second)
				log.Debugf("handle SaveHistory time cost: %g num transactions:%d", tcall, len(kind.Transactions))
			}
		case closed := <-c.quitEx:
			closed <- true
			return
		}
	}
}

func (c *ChainStoreExtend) GetTxHistory(addr string, order string, vers string) interface{} {
	key := new(bytes.Buffer)
	key.WriteByte(byte(DataTxHistoryPrefix))
	var txhs interface{}
	if order == "desc" {
		txhs = make(types.TransactionHistorySorterDesc, 0)
	} else {
		txhs = make(types.TransactionHistorySorter, 0)
	}
	address, err := common2.Uint168FromAddress(addr)
	if err != nil {
		return txhs
	}
	common2.WriteVarBytes(key, address[:])
	iter := c.NewIterator(key.Bytes())
	defer iter.Release()

	for iter.Next() {
		val := new(bytes.Buffer)
		val.Write(iter.Value())
		txh := types.TransactionHistory{}
		txhd, _ := txh.Deserialize(val)
		if txhd.Type == "income" {
			if len(txhd.Inputs) > 0 {
				txhd.Inputs = []string{txhd.Inputs[0]}
			} else {
				txhd.Inputs = []string{}
			}
			txhd.Outputs = []string{txhd.Address}
		} else {
			txhd.Inputs = []string{txhd.Address}
			if vers != "2" {
				txhd.Outputs = []string{txhd.Outputs[0]}
			} else {
				if len(txhd.Outputs) > 10 {
					txhd.Outputs = txhd.Outputs[0:10]
				}
			}
		}
		if vers == "2" {
			if txhd.Type == "spend" {
				txhd.Fee = txhd.Fee + uint64(*txhd.NodeFee)
			}
		} else {
			txhd.NodeFee = nil
			txhd.NodeOutputIndex = nil
			txhd.Status = ""
		}
		if order == "desc" {
			txhs = append(txhs.(types.TransactionHistorySorterDesc), *txhd)
		} else {
			txhs = append(txhs.(types.TransactionHistorySorter), *txhd)
		}
	}

	poolTx := DefaultMemPool.GetMemPoolTx(address)

	for _, txh := range poolTx {
		if order == "desc" {
			txhs = append(txhs.(types.TransactionHistorySorterDesc), txh)
		} else {
			txhs = append(txhs.(types.TransactionHistorySorter), txh)
		}
	}

	if order == "desc" {
		sort.Sort(txhs.(types.TransactionHistorySorterDesc))
	} else {
		sort.Sort(txhs.(types.TransactionHistorySorter))
	}
	return txhs
}

func (c *ChainStoreExtend) GetTxHistoryByPage(addr, order, vers string, pageNum, pageSize uint32) (interface{}, int) {
	txhs := c.GetTxHistory(addr, order, vers)
	from := (pageNum - 1) * pageSize
	if order == "desc" {
		return txhs.(types.TransactionHistorySorterDesc).Filter(from, pageSize), len(txhs.(types.TransactionHistorySorterDesc))
	} else {
		return txhs.(types.TransactionHistorySorter).Filter(from, pageSize), len(txhs.(types.TransactionHistorySorter))
	}
}

func (c *ChainStoreExtend) GetPublicKey(addr string) string {
	key := new(bytes.Buffer)
	key.WriteByte(byte(DataPkPrefix))
	k, _ := common2.Uint168FromAddress(addr)
	k.Serialize(key)
	buf, err := c.Get(key.Bytes())
	if err != nil {
		log.Warn("No public key find")
		return ""
	}
	return hex.EncodeToString(buf[1:])
}

func (c *ChainStoreExtend) GetDposReward(addr string) (*common2.Fixed64, error) {
	key := new(bytes.Buffer)
	key.WriteByte(byte(DataDposRewardPrefix))
	k, _ := common2.Uint168FromAddress(addr)
	k.Serialize(key)
	iter := c.NewIterator(key.Bytes())
	var r common2.Fixed64
	for iter.Next() {
		v, err := common2.Fixed64FromBytes(iter.Value())
		if err != nil {
			return nil, err
		}
		r += *v
	}
	return &r, nil
}

func (c *ChainStoreExtend) GetDposRewardByHeight(addr string, height uint32) (*common2.Fixed64, error) {
	key := new(bytes.Buffer)
	key.WriteByte(byte(DataDposRewardPrefix))
	k, _ := common2.Uint168FromAddress(addr)
	k.Serialize(key)
	common2.WriteUint32(key, height)
	buf, err := c.Get(key.Bytes())
	if err != nil {
		return nil, err
	}
	return common2.Fixed64FromBytes(buf)
}

func (c *ChainStoreExtend) GetBestHeightExt() (uint32, error) {
	key := new(bytes.Buffer)
	key.WriteByte(byte(DataBestHeightPrefix))
	data, err := c.Get(key.Bytes())
	if err != nil {
		return 0, err
	}
	buf := bytes.NewBuffer(data)
	return binary.LittleEndian.Uint32(buf.Bytes()), nil
}

func (c *ChainStoreExtend) GetStoredHeightExt(height uint32) (bool, error) {
	key := new(bytes.Buffer)
	key.WriteByte(byte(DataStoredHeightPrefix))
	common2.WriteUint32(key, height)
	_, err := c.Get(key.Bytes())
	if err != nil {
		return false, err
	}
	return true, nil
}

func (c *ChainStoreExtend) LockDposData() {
	c.mu.RLock()
}

func (c *ChainStoreExtend) UnlockDposData() {
	c.mu.RUnlock()
}

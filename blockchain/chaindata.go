package blockchain

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	common2 "github.com/elastos/Elastos.ELA.Elephant.Node/common"
	"github.com/elastos/Elastos.ELA.Elephant.Node/core/types"
	"github.com/elastos/Elastos.ELA/common"
	"github.com/elastos/Elastos.ELA/common/log"
	"github.com/elastos/Elastos.ELA/dpos/state"
	"io/ioutil"
	"net/http"
	"strconv"
)

var i *int = common2.NewInt(-1)

func (c ChainStoreExtend) begin() {
	c.NewBatch()
}

func (c ChainStoreExtend) commit() {
	c.BatchCommit()
}

func (c ChainStoreExtend) rollback() {

}

// key: DataEntryPrefix + height + address
// value: serialized history
func (c ChainStoreExtend) persistTransactionHistory(txhs []types.TransactionHistory) error {
	c.begin()
	for i, txh := range txhs {
		err := c.doPersistTransactionHistory(uint64(i), txh)
		if err != nil {
			c.rollback()
			log.Fatal("Error persist transaction history")
			return err
		}
	}
	c.commit()
	return nil
}

// key: DataEntryPrefix + height + address
// value: serialized history
func (c ChainStoreExtend) persistPbks(pbks map[common.Uint168][]byte) error {
	c.begin()
	for k, v := range pbks {
		err := c.doPersistPbks(k, v)
		if err != nil {
			c.rollback()
			log.Fatal("Error persist public keys")
			return err
		}
	}
	c.commit()
	return nil
}

func (c ChainStoreExtend) persistDposReward(rewardDpos map[common.Uint168]common.Fixed64, height uint32) error {
	c.begin()
	for k, v := range rewardDpos {
		err := c.doPersistDposReward(k, v, height)
		if err != nil {
			c.rollback()
			log.Fatal("Error persist dpos reward")
			return err
		}
	}
	c.commit()
	return nil
}

func (c ChainStoreExtend) persistBestHeight(height uint32) error {
	bestHeight, err := c.GetBestHeightExt()
	if (err == nil && bestHeight < height) || err != nil {
		c.begin()
		err = c.doPersistBestHeight(height)
		if err != nil {
			c.rollback()
			log.Fatal("Error persist best height")
			return err
		}
		c.commit()
	}
	return nil
}

func (c ChainStoreExtend) persistStoredHeight(height uint32) error {
	c.begin()
	err := c.doPersistStoredHeight(height)
	if err != nil {
		c.rollback()
		log.Fatal("Error persist best height")
		return err
	}
	c.commit()
	return nil
}

func (c ChainStoreExtend) doPersistStoredHeight(h uint32) error {
	key := new(bytes.Buffer)
	key.WriteByte(byte(DataStoredHeightPrefix))
	common.WriteUint32(key, h)
	value := new(bytes.Buffer)
	common.WriteVarBytes(value, []byte{1})
	c.BatchPut(key.Bytes(), value.Bytes())
	return nil
}

func (c ChainStoreExtend) doPersistBestHeight(h uint32) error {
	key := new(bytes.Buffer)
	key.WriteByte(byte(DataBestHeightPrefix))
	value := new(bytes.Buffer)
	common.WriteUint32(value, h)
	c.BatchPut(key.Bytes(), value.Bytes())
	return nil
}

func (c ChainStoreExtend) doPersistDposReward(k common.Uint168, v common.Fixed64, h uint32) error {
	key := new(bytes.Buffer)
	key.WriteByte(byte(DataDposRewardPrefix))
	err := k.Serialize(key)
	if err != nil {
		return err
	}
	common.WriteUint32(key, h)

	value := new(bytes.Buffer)
	v.Serialize(value)
	c.BatchPut(key.Bytes(), value.Bytes())
	return nil
}

func (c ChainStoreExtend) doPersistPbks(k common.Uint168, pub []byte) error {
	key := new(bytes.Buffer)
	key.WriteByte(byte(DataPkPrefix))
	err := k.Serialize(key)
	if err != nil {
		return err
	}
	value := new(bytes.Buffer)
	common.WriteVarBytes(value, pub)
	c.BatchPut(key.Bytes(), value.Bytes())
	return nil
}

func (c ChainStoreExtend) doPersistTransactionHistory(i uint64, history types.TransactionHistory) error {
	key := new(bytes.Buffer)
	key.WriteByte(byte(DataTxHistoryPrefix))
	err := common.WriteVarBytes(key, history.Address[:])
	if err != nil {
		return err
	}
	err = common.WriteUint64(key, history.Height)
	if err != nil {
		return err
	}
	err = common.WriteUint64(key, i)
	if err != nil {
		return err
	}

	value := new(bytes.Buffer)
	history.Serialize(value)
	c.BatchPut(key.Bytes(), value.Bytes())
	return nil
}

func (c ChainStoreExtend) initTask() {
	c.AddFunc("@every "+common2.Conf.Cmc.Inteval, c.renewCmcPrice)
	c.AddFunc("@every 2m", func() {
		if len(c.rp) == 0 {
			c.rp <- true
		}
	})
	c.Start()
}

func (c *ChainStoreExtend) renewProducer() {
	if DefaultChainStoreEx.GetHeight() >= 290000 {
		var err error
		db, err := DBA.Begin()
		defer func() {
			if err != nil {
				log.Errorf("Error renew producer %s", err.Error())
				db.Rollback()
			} else {
				db.Commit()
			}
		}()
		if err != nil {
			return
		}
		stmt1, err := db.Prepare("delete from chain_producer_info")
		if err != nil {
			return
		}
		_, err = stmt1.Exec()
		if err != nil {
			return
		}
		stmt1.Close()

		stmt, err := db.Prepare("insert into chain_producer_info (Ownerpublickey,Nodepublickey,Nickname,Url,Location,Active,Votes,Netaddress,State,Registerheight,Cancelheight,Inactiveheight,Illegalheight,`Index`) values(?,?,?,?,?,?,?,?,?,?,?,?,?,?)")
		if err != nil {
			return
		}
		producers := c.chain.GetState().GetAllProducers()
		for i, producer := range producers {
			var active int
			if producer.State() == state.Active {
				active = 1
			} else {
				active = 0
			}
			_, err = stmt.Exec(common.BytesToHexString(producer.OwnerPublicKey()), common.BytesToHexString(producer.NodePublicKey()),
				producer.Info().NickName, producer.Info().Url, producer.Info().Location, active, producer.Votes().String(),
				producer.Info().NetAddress, producer.State().String(), producer.RegisterHeight(), producer.CancelHeight(),
				producer.InactiveSince(), producer.IllegalHeight(), i)
			if err != nil {
				return
			}
		}
		stmt.Close()
	}
}

func (c *ChainStoreExtend) renewCmcPrice() {
	if *i == len(common2.Conf.Cmc.ApiKey)-1 {
		*i = 0
	} else {
		*i = *i + 1
	}
	cmcResponseUSD, err := fetchPrice(*i, "USD")
	if err != nil {
		log.Warnf("Error in cmc price %s", err.Error())
		return
	}
	cmcResponseCNY, err := fetchPrice(*i, "CNY")
	if err != nil {
		log.Warnf("Error in cmc price %s", err.Error())
		return
	}
	cmcResponseBTC, err := fetchPrice(*i, "BTC")
	if err != nil {
		log.Warnf("Error in cmc price %s", err.Error())
		return
	}
	c.begin()
	err = c.saveToDb(cmcResponseUSD, cmcResponseCNY, cmcResponseBTC)
	if err != nil {
		log.Warnf("Error in cmc price %s", err.Error())
		c.rollback()
		return
	}
	c.commit()
}

type hbg_price struct {
	Status string
	Ch     string
	Ts     int64
	Data   []hbg_price_data
}

type hbg_price_data struct {
	Id   int64
	Ts   int64
	Data []hg_price_data_data
}

type hg_price_data_data struct {
	Amount    float64
	Ts        int64
	Id        float64
	Price     float64
	Direction string
}

func getPriceFromHbg() (string, error) {
	resp, err := http.Get(types.HBG_ENDPOINT_URL)
	if err != nil {
		fmt.Printf("Error fetching price from hbg\n")
		return "", err
	} else {
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return "", err
		}
		var hbg_price hbg_price
		err = json.Unmarshal(body, &hbg_price)
		if err != nil {
			return "", err
		}
		if len(hbg_price.Data) > 0 && len(hbg_price.Data[0].Data) > 0 {
			return strconv.FormatFloat(hbg_price.Data[0].Data[0].Price, 'f', 8, 64), nil
		}
		return "", errors.New("Error fetching price from hbg, data structure is changed")
	}
}

func (c ChainStoreExtend) saveToDb(cmcResponseUSD, cmcResponseCNY, cmcResponseBTC types.CmcResponse) error {
	data := cmcResponseUSD.Data
	if len(cmcResponseCNY.Data) != len(cmcResponseUSD.Data) || len(cmcResponseUSD.Data) != len(cmcResponseBTC.Data) {
		log.Errorf("Invalid Key fetch Cmc Data m CNY :%v, BTC :%v, USD :%v", cmcResponseCNY, cmcResponseBTC, cmcResponseUSD)
		return nil
	}
	cmcs := make([]types.Cmc, 0)
	for i := 0; i < len(data); i++ {
		var btcPrice string
		if data[i].Symbol == "ELA" {
			btcPrice, err := getPriceFromHbg()
			if err != nil {
				log.Errorf("Error fetching Hbg Price")
			} else {
				log.Infof(" Getting Price From Hbg %s", btcPrice)
			}
		}
		if btcPrice == "" {
			btcPrice = strconv.FormatFloat(cmcResponseBTC.Data[i].Quote.BTC.Price, 'f', 8, 64)
		}
		cmc := types.Cmc{}
		cmc.Id = strconv.Itoa(int(data[i].Id))
		cmc.Name = data[i].Name
		cmc.Symbol = data[i].Symbol
		cmc.Rank = strconv.Itoa(data[i].Cmc_rank)
		cmc.Price_usd = strconv.FormatFloat(data[i].Quote.USD.Price, 'f', 8, 64)
		cmc.Price_cny = strconv.FormatFloat(cmcResponseCNY.Data[i].Quote.CNY.Price, 'f', 8, 64)
		cmc.Price_btc = btcPrice
		cmc.Volume_usd = strconv.FormatFloat(data[i].Quote.USD.Volume_24h, 'f', 8, 64)
		cmc.Market_cap_usd = strconv.FormatFloat(data[i].Quote.USD.Market_cap, 'f', 8, 64)
		cmc.Available_supply = strconv.FormatFloat(data[i].Circulating_supply, 'f', 8, 64)
		cmc.Total_supply = strconv.FormatFloat(data[i].Total_supply, 'f', 8, 64)
		cmc.Max_supply = strconv.FormatFloat(data[i].Max_supply, 'f', 8, 64)
		cmc.Percent_change_1h = strconv.FormatFloat(data[i].Quote.USD.Percent_change_1h, 'f', 8, 64)
		cmc.Percent_change_24h = strconv.FormatFloat(data[i].Quote.USD.Percent_change_24h, 'f', 8, 64)
		cmc.Percent_change_7d = strconv.FormatFloat(data[i].Quote.USD.Percent_change_7d, 'f', 8, 64)
		cmc.Last_updated = data[i].Quote.USD.Last_updated
		cmc.Volume_btc = strconv.FormatFloat(cmcResponseBTC.Data[i].Quote.BTC.Volume_24h, 'f', 8, 64)
		cmc.Market_cap_btc = strconv.FormatFloat(cmcResponseBTC.Data[i].Quote.BTC.Market_cap, 'f', 8, 64)
		cmc.Volume_cny = strconv.FormatFloat(cmcResponseCNY.Data[i].Quote.CNY.Volume_24h, 'f', 8, 64)
		cmc.Market_cap_cny = strconv.FormatFloat(cmcResponseCNY.Data[i].Quote.CNY.Market_cap, 'f', 8, 64)
		cmc.Platform_symbol = data[i].Platform.Symbol
		cmc.Platform_token_address = data[i].Platform.Token_Address
		cmc.Num_market_pairs = strconv.Itoa(int(data[i].Num_market_pairs))
		cmcs = append(cmcs, cmc)
	}
	if len(cmcs) > 0 {
		c.persistCmc(types.Cmcs{cmcs})
	}
	return nil
}

func (c ChainStoreExtend) persistCmc(cmc types.Cmcs) error {
	key := new(bytes.Buffer)
	key.WriteByte(byte(DataCmcPrefix))
	common.WriteVarString(key, "CMC")
	c.Delete(key.Bytes())
	value := new(bytes.Buffer)
	cmc.Serialize(value)
	c.Put(key.Bytes(), value.Bytes())
	return nil
}

func fetchPrice(i int, curr string) (types.CmcResponse, error) {
	url := fmt.Sprintf(types.CMC_ENDPOINT_URL, 2000, curr)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return types.CmcResponse{}, err
	}
	req.Header = map[string][]string{
		"X-CMC_PRO_API_KEY": []string{common2.Conf.Cmc.ApiKey[i]},
	}
	client := http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return types.CmcResponse{}, err
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return types.CmcResponse{}, err
	}
	cmcResp := types.CmcResponse{}
	err = json.Unmarshal(body, &cmcResp)
	if err != nil {
		return types.CmcResponse{}, err
	}
	return cmcResp, nil
}

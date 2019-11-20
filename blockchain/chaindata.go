package blockchain

import (
	"bytes"
	common2 "github.com/elastos/Elastos.ELA.Elephant.Node/common"
	"github.com/elastos/Elastos.ELA.Elephant.Node/core/types"
	"github.com/elastos/Elastos.ELA/common"
	"github.com/elastos/Elastos.ELA/common/log"
	"github.com/elastos/Elastos.ELA/dpos/state"
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
	for _, txh := range txhs {
		c.deleteMemPoolTx(txh.Txid)
	}
	c.commit()
	return nil
}

func (c ChainStoreExtend) deleteMemPoolTx(txid common.Uint256) {
	DefaultMemPool.DeleteMemPoolTx(txid)
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

package blockchain

import (
	"github.com/elastos/Elastos.ELA.Elephant.Node/core/types"
	. "github.com/elastos/Elastos.ELA/blockchain"
	"github.com/elastos/Elastos.ELA/common"
	. "github.com/elastos/Elastos.ELA/core/types"
)

var DefaultChainStoreEx IChainStoreExtend

type IChainStoreExtend interface {
	IChainStore
	persistTxHistory(block *Block) error
	CloseEx()
	AddTask(task interface{})
	GetTxHistory(addr, order, ver string) interface{}
	GetTxHistoryByPage(addr, order, ver string, pageNum, pageSize uint32) (interface{}, int)
	GetCmcPrice() types.Cmcs
	GetPublicKey(addr string) string
	GetDposReward(addr string) (*common.Fixed64, error)
	GetDposRewardByHeight(addr string, height uint32) (*common.Fixed64, error)
	LockDposData()
	UnlockDposData()
}

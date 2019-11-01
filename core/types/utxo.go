package types

import "github.com/elastos/Elastos.ELA/blockchain"

type Utxos []*blockchain.UTXO

func (a Utxos) Len() int           { return len(a) }
func (a Utxos) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a Utxos) Less(i, j int) bool { return a[i].Value > a[j].Value }

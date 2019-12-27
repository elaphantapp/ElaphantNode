package common

import (
	"container/list"
	"database/sql"
	"github.com/elastos/Elastos.ELA/common/log"
	_ "github.com/mattn/go-sqlite3"
	"os"
	"reflect"
	"strconv"
)

type Dba struct {
	*sql.DB
}

func NewInstance(filePath string) (*Dba, error) {
	_, err := os.Stat(filePath + "/dpos/dpos.db")
	if err != nil {
		os.MkdirAll(filePath+"/dpos", 0755)
		os.Create(filePath + "/dpos/dpos.db")
	}
	db, err := sql.Open("sqlite3", filePath+"/dpos/dpos.db")
	if err != nil {
		return nil, err
	}
	return &Dba{db}, nil
}

func (d *Dba) Qu(s string) (*list.List, error) {
	rows, err := d.Query(s)
	if err != nil {
		return nil, err
	}
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	values := make([][]byte, len(columns))

	// rows.Scan wants '[]interface{}' as an argument, so we must copy the
	// references into such a slice
	// See http://code.google.com/p/go-wiki/wiki/InterfaceSlice for details
	scanArgs := make([]interface{}, len(values))
	for i := range values {
		scanArgs[i] = &values[i]
	}
	retList := list.New()

	// Fetch rows
	for rows.Next() {
		retMap := make(map[string]interface{})
		retList.PushBack(retMap)
		// get RawBytes from data
		err = rows.Scan(scanArgs...)
		if err != nil {
			return nil, err
		}

		for i, col := range values {
			// Here we can check if the value is nil (NULL value)
			if col == nil {
				// skip nil value
				continue
			}
			retMap[columns[i]] = string(col)
		}
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return retList, nil
}

//
func (d *Dba) ToStruct(sql string, strct interface{}) ([]interface{}, error) {
	l, err := d.Qu(sql)
	if err != nil {
		return nil, err
	}
	i := 0
	r := make([]interface{}, l.Len())
	for e := l.Front(); e != nil; e = e.Next() {
		v := reflect.New(reflect.TypeOf(strct))
		src := e.Value.(map[string]interface{})
		vi := v.Interface()
		Map2Struct(src, vi)
		r[i] = reflect.ValueOf(vi).Interface()
		i++
	}
	return r, nil
}

func (d *Dba) ToInt(sql string) (int, error) {

	l, err := d.Qu(sql)
	if err != nil || l.Len() == 0 {
		return -1, err
	}
	m := l.Front().Value.(map[string]interface{})
	for _, v := range m {
		return strconv.Atoi(v.(string))
	}

	return -1, err
}

func (d *Dba) ToFloat(sql string) (float64, error) {

	l, err := d.Qu(sql)
	if err != nil || l.Len() == 0 {
		return -1, err
	}
	m := l.Front().Value.(map[string]interface{})
	for _, v := range m {
		return strconv.ParseFloat(v.(string), 64)
	}

	return -1, err
}

//
func (d *Dba) ToString(sql string) (string, error) {

	l, err := d.Qu(sql)
	if err != nil || l.Len() == 0 {
		return "", err
	}
	m := l.Front().Value.(map[string]interface{})
	for _, v := range m {
		return v.(string), nil
	}

	return "", err
}

func InitDb(db *Dba) error {
	createTableSqlStmtArr := []string{
		`PRAGMA encoding = "UTF-8";`,
		`CREATE TABLE IF not exists chain_vote_info (_id INTEGER primary key, producer_public_key varchar(66) not null, vote_type varchar(24) not null, txid varchar(64) not null, n INTEGER not null, value varchar(24) not null, outputlock INTEGER not null, address varchar(34) not null,block_time INTEGER not null, height INTEGER not null,is_valid varchar(10) DEFAULT 'YES', cancel_height INTEGER)`,
		`CREATE INDEX IF not exists idx_chain_vote_info_address ON chain_vote_info (address);`,
		`CREATE INDEX IF not exists idx_chain_vote_info_producer_public_key ON chain_vote_info (producer_public_key);`,
		"CREATE TABLE IF not exists chain_producer_info (_id INTEGER PRIMARY KEY,ownerpublickey varchar(66) NOT NULL,nodepublickey varchar(66) NOT NULL,nickname text  NOT NULL,url varchar(256)  NOT NULL,location INTEGER NOT NULL,active INTEGER NOT NULL,votes varchar(24)  NOT NULL,netaddress varchar(124)  NOT NULL,state varchar(24)  NOT NULL,registerheight INTEGER NOT NULL,cancelheight INTEGER NOT NULL,inactiveheight INTEGER NOT NULL,illegalheight INTEGER NOT NULL, `index` INTEGER NOT NULL)",
		`CREATE INDEX IF not exists idx_chain_producer_info ON chain_producer_info (ownerpublickey);`,
		`CREATE INDEX IF not exists idx_chain_vote_info_txid_n ON chain_vote_info (txid,n);`,

		`CREATE TABLE IF not exists chain_vote_cr_info (_id INTEGER primary key, did varchar(42) not null, vote_type varchar(24) not null, txid varchar(64) not null, n INTEGER not null, value varchar(24) not null, outputlock INTEGER not null, address varchar(34) not null,block_time INTEGER not null, height INTEGER not null,is_valid varchar(10) DEFAULT 'YES', cancel_height INTEGER)`,
		`CREATE INDEX IF not exists idx_chain_vote_cr_info_address ON chain_vote_cr_info (address);`,
		`CREATE INDEX IF not exists idx_chain_vote_cr_info_producer_public_key ON chain_vote_cr_info (did);`,
		"CREATE TABLE IF not exists chain_cr_candidate_info (_id INTEGER PRIMARY KEY,code varchar(70) NOT NULL,did varchar(34) NOT NULL,nickname text  NOT NULL,url varchar(256)  NOT NULL,location INTEGER NOT NULL,state varchar(20) NOT NULL,votes varchar(24)  NOT NULL,`index` INTEGER NOT NULL)",
		`CREATE INDEX IF not exists idx_chain_cr_candidate_info ON chain_cr_candidate_info (did);`,
		`CREATE INDEX IF not exists idx_chain_vote_cr_info_txid_n ON chain_vote_cr_info (txid,n);`,
	}

	r, err := db.Query(`SELECT name FROM sqlite_master WHERE name=?`, "chain_cr_candidate_info")
	if err != nil {
		log.Fatalf("Error Init db %s", err.Error())
		return err
	}
	if !r.Next() {
		for _, v := range createTableSqlStmtArr {
			_, err := db.Exec(v)
			if err != nil {
				log.Infof("Error execute sql : %s \n", err.Error())
				return err
			}
		}
	}
	return nil
}

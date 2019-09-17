package common

import (
	"encoding/json"
	"fmt"
	"os"
)

type config struct {
	Cmc            Cmc
	EarnReward     bool
	BundleUtxoSize int
}

type Cmc struct {
	ApiKey  []string
	Inteval string
}

var Conf *config

func init() {
	Conf = new(config)
	file, _ := os.Open("ext_config.json")
	defer file.Close()
	decoder := json.NewDecoder(file)
	err := decoder.Decode(&Conf)
	if err != nil {
		fmt.Println("Error init Config :", err.Error())
		os.Exit(-1)
	}
}

package common

import "github.com/urfave/cli"

var NodeKeyFlag = cli.StringFlag{
	Name:  "key",
	Usage: "private key used to generate node identity , should be the private key of config PowConfiguration.PayToAddr",
}

var PureSyncFlag = cli.BoolFlag{
	Name:  "pure",
	Usage: "only sync block chain data , web service are shutdown for now",
}

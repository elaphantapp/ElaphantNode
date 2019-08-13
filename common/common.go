package common

import (
	"github.com/elastos/Elastos.ELA/common"
	"github.com/elastos/Elastos.ELA/core/contract"
)

func GetProgramHash(public []byte) (*common.Uint168, error) {
	hash, err := contract.PublicKeyToStandardProgramHash(public)
	if err != nil {
		return nil, err
	}
	return hash, nil
}

func GetAddress(public []byte) (string, error) {
	hash, err := GetProgramHash(public)
	if err != nil {
		return "", err
	}
	return hash.ToAddress()
}

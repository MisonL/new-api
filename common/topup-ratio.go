package common

import (
	"errors"
	"sync"
)

var topupGroupRatio = map[string]float64{
	"default": 1,
	"vip":     1,
	"svip":    1,
}
var topupGroupRatioMutex sync.RWMutex

func TopupGroupRatio2JSONString() string {
	topupGroupRatioMutex.RLock()
	defer topupGroupRatioMutex.RUnlock()
	jsonBytes, err := Marshal(topupGroupRatio)
	if err != nil {
		SysError("error marshalling topup group ratio: " + err.Error())
	}
	return string(jsonBytes)
}

// UpdateTopupGroupRatioByJSONString replaces the ratio table with a parsed payload.
//
// The payload is decoded into a fresh map before the live one is replaced, so a
// malformed admin submission returns an error without emptying the table.
func UpdateTopupGroupRatioByJSONString(jsonStr string) error {
	loaded := make(map[string]float64)
	if err := Unmarshal([]byte(jsonStr), &loaded); err != nil {
		return err
	}
	if loaded == nil {
		return errors.New("topup group ratio payload must be a JSON object")
	}
	topupGroupRatioMutex.Lock()
	defer topupGroupRatioMutex.Unlock()
	topupGroupRatio = loaded
	return nil
}

func GetTopupGroupRatio(name string) float64 {
	topupGroupRatioMutex.RLock()
	defer topupGroupRatioMutex.RUnlock()
	ratio, ok := topupGroupRatio[name]
	if !ok {
		SysError("topup group ratio not found: " + name)
		return 1
	}
	return ratio
}

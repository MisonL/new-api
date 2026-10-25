package setting

import (
	"errors"
	"sync"

	"github.com/QuantumNous/new-api/common"
)

var userUsableGroups = map[string]string{
	"default": "默认分组",
	"vip":     "vip分组",
}
var userUsableGroupsMutex sync.RWMutex

func GetUserUsableGroupsCopy() map[string]string {
	userUsableGroupsMutex.RLock()
	defer userUsableGroupsMutex.RUnlock()

	copyUserUsableGroups := make(map[string]string)
	for k, v := range userUsableGroups {
		copyUserUsableGroups[k] = v
	}
	return copyUserUsableGroups
}

func UserUsableGroups2JSONString() string {
	userUsableGroupsMutex.RLock()
	defer userUsableGroupsMutex.RUnlock()

	jsonBytes, err := common.Marshal(userUsableGroups)
	if err != nil {
		common.SysLog("error marshalling user groups: " + err.Error())
	}
	return string(jsonBytes)
}

// UpdateUserUsableGroupsByJSONString replaces the group description map.
//
// The payload is decoded into a fresh map before the lock is taken, so a
// rejected submission leaves the live map intact instead of emptying it.
func UpdateUserUsableGroupsByJSONString(jsonStr string) error {
	loaded := make(map[string]string)
	if err := common.Unmarshal([]byte(jsonStr), &loaded); err != nil {
		return err
	}
	if loaded == nil {
		return errors.New("user usable groups payload must be a JSON object")
	}

	userUsableGroupsMutex.Lock()
	defer userUsableGroupsMutex.Unlock()
	userUsableGroups = loaded
	return nil
}

func GetUsableGroupDescription(groupName string) string {
	userUsableGroupsMutex.RLock()
	defer userUsableGroupsMutex.RUnlock()

	if desc, ok := userUsableGroups[groupName]; ok {
		return desc
	}
	return groupName
}

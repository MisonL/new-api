package service

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

const (
	syntheticCompactInstanceOptionKey = "runtime.responses_synthetic_compact_instance_id"
	syntheticCompactInstanceIDPrefix  = "n"
)

var (
	syntheticCompactInstanceMu       sync.Mutex
	syntheticCompactCachedInstanceID string
	syntheticCompactProcessInstance  = syntheticCompactInstanceIDPrefix + common.GetUUID()
)

func syntheticCompactLocalInstanceID(ctx context.Context) (string, error) {
	if model.DB == nil {
		return syntheticCompactProcessInstance, nil
	}

	syntheticCompactInstanceMu.Lock()
	defer syntheticCompactInstanceMu.Unlock()
	if syntheticCompactCachedInstanceID != "" {
		return syntheticCompactCachedInstanceID, nil
	}

	storeCtx, cancel := syntheticCompactStoreContext(ctx)
	defer cancel()

	option := model.Option{}
	err := model.DB.WithContext(storeCtx).First(&option, "key = ?", syntheticCompactInstanceOptionKey).Error
	if err == nil && syntheticCompactInstanceIDValid(option.Value) {
		syntheticCompactCachedInstanceID = strings.TrimSpace(option.Value)
		return syntheticCompactCachedInstanceID, nil
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", err
	}

	instanceID := syntheticCompactInstanceIDPrefix + common.GetUUID()
	if err == nil {
		option.Value = instanceID
		if saveErr := model.DB.WithContext(storeCtx).Save(&option).Error; saveErr != nil {
			return "", saveErr
		}
	} else if createErr := model.DB.WithContext(storeCtx).Create(&model.Option{Key: syntheticCompactInstanceOptionKey, Value: instanceID}).Error; createErr != nil {
		var reloaded model.Option
		if reloadErr := model.DB.WithContext(storeCtx).First(&reloaded, "key = ?", syntheticCompactInstanceOptionKey).Error; reloadErr == nil && syntheticCompactInstanceIDValid(reloaded.Value) {
			syntheticCompactCachedInstanceID = strings.TrimSpace(reloaded.Value)
			return syntheticCompactCachedInstanceID, nil
		}
		return "", createErr
	}

	syntheticCompactCachedInstanceID = instanceID
	return syntheticCompactCachedInstanceID, nil
}

func syntheticCompactInstanceIDValid(instanceID string) bool {
	instanceID = strings.TrimSpace(instanceID)
	if !strings.HasPrefix(instanceID, syntheticCompactInstanceIDPrefix) || len(instanceID) <= len(syntheticCompactInstanceIDPrefix) {
		return false
	}
	for _, r := range instanceID[len(syntheticCompactInstanceIDPrefix):] {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}

func resetSyntheticCompactInstanceForTest() {
	syntheticCompactInstanceMu.Lock()
	defer syntheticCompactInstanceMu.Unlock()
	syntheticCompactCachedInstanceID = ""
}

func syntheticCompactMarkerInstanceMatches(ctx context.Context, instanceID string) (bool, error) {
	instanceID = strings.TrimSpace(instanceID)
	if !syntheticCompactInstanceIDValid(instanceID) {
		return false, nil
	}
	localID, err := syntheticCompactLocalInstanceID(ctx)
	if err != nil {
		return false, err
	}
	return instanceID == localID, nil
}

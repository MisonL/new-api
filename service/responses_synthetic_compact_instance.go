package service

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"
)

const (
	syntheticCompactInstanceOptionKey = "runtime.responses_synthetic_compact_instance_id"
	syntheticCompactInstanceIDPrefix  = "n"
)

var (
	syntheticCompactInstanceMu       sync.Mutex
	syntheticCompactInstanceGroup    singleflight.Group
	syntheticCompactCachedInstanceID string
	syntheticCompactProcessInstance  = syntheticCompactInstanceIDPrefix + common.GetUUID()
)

func syntheticCompactLocalInstanceID(ctx context.Context) (string, error) {
	if model.DB == nil {
		return syntheticCompactProcessInstance, nil
	}
	syntheticCompactInstanceMu.Lock()
	if syntheticCompactCachedInstanceID != "" {
		cachedID := syntheticCompactCachedInstanceID
		syntheticCompactInstanceMu.Unlock()
		return cachedID, nil
	}
	syntheticCompactInstanceMu.Unlock()

	value, err, _ := syntheticCompactInstanceGroup.Do("local-instance-id", func() (any, error) {
		syntheticCompactInstanceMu.Lock()
		if syntheticCompactCachedInstanceID != "" {
			cachedID := syntheticCompactCachedInstanceID
			syntheticCompactInstanceMu.Unlock()
			return cachedID, nil
		}
		syntheticCompactInstanceMu.Unlock()

		instanceID, err := loadOrCreateSyntheticCompactLocalInstanceID(ctx)
		if err != nil {
			return "", err
		}
		syntheticCompactInstanceMu.Lock()
		syntheticCompactCachedInstanceID = instanceID
		syntheticCompactInstanceMu.Unlock()
		return instanceID, nil
	})
	if err != nil {
		return "", err
	}
	instanceID, _ := value.(string)
	return instanceID, nil
}

func loadOrCreateSyntheticCompactLocalInstanceID(ctx context.Context) (string, error) {
	storeCtx, cancel := syntheticCompactStoreContext(ctx)
	defer cancel()

	option := model.Option{}
	err := model.DB.WithContext(storeCtx).First(&option, "key = ?", syntheticCompactInstanceOptionKey).Error
	if err == nil && syntheticCompactInstanceIDValid(option.Value) {
		return strings.TrimSpace(option.Value), nil
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", err
	}

	instanceID := syntheticCompactInstanceIDPrefix + common.GetUUID()
	if err == nil {
		if saveErr := model.DB.WithContext(storeCtx).
			Model(&model.Option{}).
			Where("key = ?", syntheticCompactInstanceOptionKey).
			Update("value", instanceID).Error; saveErr != nil {
			return "", saveErr
		}
		return instanceID, nil
	}

	if createErr := model.DB.WithContext(storeCtx).Create(&model.Option{Key: syntheticCompactInstanceOptionKey, Value: instanceID}).Error; createErr != nil {
		var reloaded model.Option
		if reloadErr := model.DB.WithContext(storeCtx).First(&reloaded, "key = ?", syntheticCompactInstanceOptionKey).Error; reloadErr == nil && syntheticCompactInstanceIDValid(reloaded.Value) {
			return strings.TrimSpace(reloaded.Value), nil
		}
		return "", createErr
	}

	return instanceID, nil
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
	syntheticCompactInstanceGroup = singleflight.Group{}
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

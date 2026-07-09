package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestHasActiveUserSubscription(t *testing.T) {
	db := setupSubscriptionPaymentGuardTestDB(t)
	now := common.GetTimestamp()

	hasActive, err := HasActiveUserSubscription(1)
	require.NoError(t, err)
	require.False(t, hasActive)

	require.NoError(t, db.Create(&UserSubscription{
		UserId:  1,
		PlanId:  10,
		EndTime: now + 3600,
		Status:  "cancelled",
	}).Error)
	require.NoError(t, db.Create(&UserSubscription{
		UserId:  1,
		PlanId:  11,
		EndTime: now - 1,
		Status:  "active",
	}).Error)

	hasActive, err = HasActiveUserSubscription(1)
	require.NoError(t, err)
	require.False(t, hasActive)

	require.NoError(t, db.Create(&UserSubscription{
		UserId:  1,
		PlanId:  12,
		EndTime: now + 3600,
		Status:  "active",
	}).Error)

	hasActive, err = HasActiveUserSubscription(1)
	require.NoError(t, err)
	require.True(t, hasActive)
}

func TestHasActiveUserSubscriptionRejectsInvalidUser(t *testing.T) {
	setupSubscriptionPaymentGuardTestDB(t)

	hasActive, err := HasActiveUserSubscription(0)
	require.Error(t, err)
	require.False(t, hasActive)
}

package model

import (
	"bytes"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"

	"github.com/gin-gonic/gin"
)

// The V2 database path must report an invalid mapping the same way the other
// mapping-error sites do, so a dropped candidate is never mistaken for
// "this model has no channel".
func TestRouteV2EntriesFromDatabaseLogsInvalidMapping(t *testing.T) {
	previousDB := DB
	DB = openTestDB(t, &Ability{}, &Channel{})
	defer func() { DB = previousDB }()

	channel := &Channel{
		Id:           90701,
		Name:         "v2-mapping-diagnostics-probe",
		Status:       common.ChannelStatusEnabled,
		ModelMapping: common.GetPointer(`{"broken":`),
	}
	require.NoError(t, DB.Create(channel).Error)

	priority := int64(12)
	weight := uint(10000)
	ability := Ability{
		Group:                    "default",
		Model:                    "public-model",
		ChannelId:                channel.Id,
		Enabled:                  true,
		RoutePriority:            &priority,
		RoutePriorityConfigured:  true,
		RouteWeightBPS:           &weight,
		RouteWeightBPSConfigured: true,
	}
	require.NoError(t, DB.Create(&ability).Error)

	previousWriter := gin.DefaultErrorWriter
	var captured bytes.Buffer
	gin.DefaultErrorWriter = &captured
	defer func() { gin.DefaultErrorWriter = previousWriter }()

	entries, err := routeV2EntriesFromDatabaseWithRouteFilter(
		"default",
		[]routeModelCandidate{{model: "public-model"}},
		nil,
		nil,
		nil,
		false,
		nil,
	)
	require.NoError(t, err)
	require.Empty(t, entries, "an invalid mapping must not produce a V2 entry")

	logged := captured.String()
	require.Contains(t, logged, "route candidate skipped invalid model mapping")
	require.Contains(t, logged, "channel_id=90701")
}

// A valid mapping must stay quiet and still produce an entry.
func TestRouteV2EntriesFromDatabaseStaysQuietForValidMapping(t *testing.T) {
	previousDB := DB
	DB = openTestDB(t, &Ability{}, &Channel{})
	defer func() { DB = previousDB }()

	channel := &Channel{
		Id:           90702,
		Name:         "v2-mapping-valid-probe",
		Status:       common.ChannelStatusEnabled,
		ModelMapping: common.GetPointer(`{"public-model":"provider-model"}`),
	}
	require.NoError(t, DB.Create(channel).Error)

	priority := int64(12)
	weight := uint(10000)
	ability := Ability{
		Group:                    "default",
		Model:                    "public-model",
		ChannelId:                channel.Id,
		Enabled:                  true,
		RoutePriority:            &priority,
		RoutePriorityConfigured:  true,
		RouteWeightBPS:           &weight,
		RouteWeightBPSConfigured: true,
	}
	require.NoError(t, DB.Create(&ability).Error)

	previousWriter := gin.DefaultErrorWriter
	var captured bytes.Buffer
	gin.DefaultErrorWriter = &captured
	defer func() { gin.DefaultErrorWriter = previousWriter }()

	entries, err := routeV2EntriesFromDatabaseWithRouteFilter(
		"default",
		[]routeModelCandidate{{model: "public-model"}},
		nil,
		nil,
		nil,
		false,
		nil,
	)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, "provider-model", entries[0].EffectiveRouteModel)
	require.NotContains(t, captured.String(), "skipped invalid model mapping")
}

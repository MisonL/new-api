package model

import (
	"bytes"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"

	"github.com/gin-gonic/gin"
)

// loadRouteCandidateSelections must report an invalid mapping instead of
// dropping the candidate in silence. Without the diagnostic the empty result is
// indistinguishable from "this model has no channel", and the operator only
// ever sees "no available channel for model".
func TestLoadRouteCandidateSelectionsLogsInvalidMapping(t *testing.T) {
	previousDB := DB
	DB = openTestDB(t, &Ability{}, &Channel{})
	defer func() { DB = previousDB }()

	channel := &Channel{
		Id:           90601,
		Name:         "mapping-diagnostics-probe",
		Status:       common.ChannelStatusEnabled,
		ModelMapping: common.GetPointer(`{"broken":`),
	}
	require.NoError(t, DB.Create(channel).Error)

	ability := Ability{
		Group:     "default",
		Model:     "public-model",
		ChannelId: channel.Id,
		Enabled:   true,
	}
	require.NoError(t, DB.Create(&ability).Error)

	// SysError writes through gin.DefaultErrorWriter, so swapping the writer is
	// enough to observe the diagnostic without adding a test-only hook.
	previousWriter := gin.DefaultErrorWriter
	var captured bytes.Buffer
	gin.DefaultErrorWriter = &captured
	defer func() { gin.DefaultErrorWriter = previousWriter }()

	selections, err := loadRouteCandidateSelections(
		[]Ability{ability},
		routeModelCandidate{model: "public-model"},
		false,
		nil,
		nil,
		nil,
	)
	require.NoError(t, err)
	require.Empty(t, selections, "an invalid mapping must not produce a candidate")

	logged := captured.String()
	require.Contains(t, logged, "route candidate skipped invalid model mapping")
	require.Contains(t, logged, "channel_id=90601")
}

// A channel with a valid mapping must not emit the diagnostic at all.
func TestLoadRouteCandidateSelectionsStaysQuietForValidMapping(t *testing.T) {
	previousDB := DB
	DB = openTestDB(t, &Ability{}, &Channel{})
	defer func() { DB = previousDB }()

	channel := &Channel{
		Id:           90602,
		Name:         "mapping-valid-probe",
		Status:       common.ChannelStatusEnabled,
		ModelMapping: common.GetPointer(`{"public-model":"provider-model"}`),
	}
	require.NoError(t, DB.Create(channel).Error)

	ability := Ability{
		Group:     "default",
		Model:     "public-model",
		ChannelId: channel.Id,
		Enabled:   true,
	}
	require.NoError(t, DB.Create(&ability).Error)

	previousWriter := gin.DefaultErrorWriter
	var captured bytes.Buffer
	gin.DefaultErrorWriter = &captured
	defer func() { gin.DefaultErrorWriter = previousWriter }()

	selections, err := loadRouteCandidateSelections(
		[]Ability{ability},
		routeModelCandidate{model: "public-model"},
		false,
		nil,
		nil,
		nil,
	)
	require.NoError(t, err)
	require.Len(t, selections, 1)
	require.NotContains(t, captured.String(), "skipped invalid model mapping")
}

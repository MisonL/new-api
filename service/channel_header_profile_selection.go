package service

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// RuntimeHeaderProfileSelection is the request-local result of selecting a
// channel header profile. Headers are kept only in memory; the persisted task
// snapshot stores the profile identity and an authenticated content
// fingerprint instead.
type RuntimeHeaderProfileSelection struct {
	ChannelID    int
	AttemptIndex int
	ProfileID    string
	Headers      map[string]string
}

// PrepareChannelRuntimeHeaderProfileSelection selects a profile at most once
// for a channel attempt. Responses preflight, channel setup and the actual
// outbound request all call this function, so they cannot independently
// advance the round-robin cursor or choose different random profiles.
func PrepareChannelRuntimeHeaderProfileSelection(c *gin.Context, channelID int, strategy *dto.HeaderProfileStrategy, attemptIndex int) (RuntimeHeaderProfileSelection, error) {
	if c == nil {
		return RuntimeHeaderProfileSelection{}, errors.New("header profile selection context is nil")
	}
	if strategy == nil || !strategy.Enabled {
		clearRuntimeHeaderProfileSelection(c, channelID)
		return RuntimeHeaderProfileSelection{}, nil
	}
	// Direct helper callers (channel tests, migration tools, and a few internal
	// adapters) may not have a persisted channel row. Fixed/random selection and
	// round-robin selection driven by the request attempt are still deterministic
	// in that scope; only the durable cross-request cursor requires a channel ID.
	if channelID <= 0 {
		requestIndex := attemptIndex
		if requestIndex < 0 {
			requestIndex = 0
		}
		headers, profileID, err := dto.ResolveHeaderProfileStrategyHeaders(strategy, requestIndex)
		if err != nil {
			return RuntimeHeaderProfileSelection{}, err
		}
		selection := RuntimeHeaderProfileSelection{
			ChannelID:    0,
			AttemptIndex: attemptIndex,
			ProfileID:    strings.TrimSpace(profileID),
			Headers:      cloneRuntimeHeaderProfileHeaders(headers),
		}
		common.SetContextKey(c, constant.ContextKeyChannelHeaderProfileSelection, selection)
		return cloneRuntimeHeaderProfileSelection(selection), nil
	}

	if selections, ok := getRuntimeHeaderProfileSelections(c); ok {
		if existing, exists := selections[channelID]; exists && RuntimeHeaderProfileAttemptMatches(existing.AttemptIndex, attemptIndex) {
			selection := cloneRuntimeHeaderProfileSelection(existing)
			common.SetContextKey(c, constant.ContextKeyChannelHeaderProfileSelection, selection)
			return selection, nil
		}
	}

	if existing, ok := common.GetContextKeyType[RuntimeHeaderProfileSelection](c, constant.ContextKeyChannelHeaderProfileSelection); ok && existing.ChannelID == channelID && RuntimeHeaderProfileAttemptMatches(existing.AttemptIndex, attemptIndex) {
		return cloneRuntimeHeaderProfileSelection(existing), nil
	}

	headers, profileID, err := resolveChannelRuntimeHeaderProfileHeaders(channelID, strategy)
	if err != nil {
		return RuntimeHeaderProfileSelection{}, err
	}
	selection := RuntimeHeaderProfileSelection{
		ChannelID:    channelID,
		AttemptIndex: attemptIndex,
		ProfileID:    strings.TrimSpace(profileID),
		Headers:      cloneRuntimeHeaderProfileHeaders(headers),
	}
	selections, _ := getRuntimeHeaderProfileSelections(c)
	if selections == nil {
		selections = make(map[int]RuntimeHeaderProfileSelection)
	}
	selections[channelID] = cloneRuntimeHeaderProfileSelection(selection)
	common.SetContextKey(c, constant.ContextKeyChannelHeaderProfileSelections, selections)
	common.SetContextKey(c, constant.ContextKeyChannelHeaderProfileSelection, selection)
	return cloneRuntimeHeaderProfileSelection(selection), nil
}

// ResolveChannelRuntimeHeaderProfileHeadersForRequest returns the already
// selected profile for this channel attempt, or creates the selection when a
// direct caller did not pass through channel setup first.
func ResolveChannelRuntimeHeaderProfileHeadersForRequest(c *gin.Context, channelID int, strategy *dto.HeaderProfileStrategy, attemptIndex int) (map[string]string, string, error) {
	selection, err := PrepareChannelRuntimeHeaderProfileSelection(c, channelID, strategy, attemptIndex)
	if err != nil {
		return nil, "", err
	}
	return cloneRuntimeHeaderProfileHeaders(selection.Headers), selection.ProfileID, nil
}

func GetChannelRuntimeHeaderProfileSelection(c *gin.Context) (RuntimeHeaderProfileSelection, bool) {
	if c == nil {
		return RuntimeHeaderProfileSelection{}, false
	}
	selection, ok := common.GetContextKeyType[RuntimeHeaderProfileSelection](c, constant.ContextKeyChannelHeaderProfileSelection)
	if !ok || selection.ChannelID <= 0 || strings.TrimSpace(selection.ProfileID) == "" {
		return RuntimeHeaderProfileSelection{}, false
	}
	return cloneRuntimeHeaderProfileSelection(selection), true
}

func RuntimeHeaderProfileAttemptMatches(existing int, requested int) bool {
	return requested < 0 || existing < 0 || existing == requested
}

func cloneRuntimeHeaderProfileSelection(selection RuntimeHeaderProfileSelection) RuntimeHeaderProfileSelection {
	selection.ProfileID = strings.TrimSpace(selection.ProfileID)
	selection.Headers = cloneRuntimeHeaderProfileHeaders(selection.Headers)
	return selection
}

func cloneRuntimeHeaderProfileHeaders(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(headers))
	for name, value := range headers {
		cloned[name] = value
	}
	return cloned
}

func clearRuntimeHeaderProfileSelection(c *gin.Context, channelID int) {
	if c == nil {
		return
	}

	// Preflight may inspect several candidates in one request. Disabling the
	// strategy for one candidate must only discard that candidate's cached
	// selection; clearing the whole map would force an already inspected
	// candidate to advance its round-robin cursor again when it is committed.
	if channelID > 0 {
		if selections, ok := getRuntimeHeaderProfileSelections(c); ok {
			delete(selections, channelID)
			if len(selections) == 0 {
				common.SetContextKey(c, constant.ContextKeyChannelHeaderProfileSelections, map[int]RuntimeHeaderProfileSelection(nil))
			} else {
				common.SetContextKey(c, constant.ContextKeyChannelHeaderProfileSelections, selections)
			}
		}
		if current, ok := common.GetContextKeyType[RuntimeHeaderProfileSelection](c, constant.ContextKeyChannelHeaderProfileSelection); ok && current.ChannelID == channelID {
			common.SetContextKey(c, constant.ContextKeyChannelHeaderProfileSelection, RuntimeHeaderProfileSelection{})
		}
		return
	}

	// A channel-less direct caller has no durable map entry. Clear only the
	// current channel-less selection and leave any candidate selections intact.
	if current, ok := common.GetContextKeyType[RuntimeHeaderProfileSelection](c, constant.ContextKeyChannelHeaderProfileSelection); ok && current.ChannelID <= 0 {
		common.SetContextKey(c, constant.ContextKeyChannelHeaderProfileSelection, RuntimeHeaderProfileSelection{})
	}
}

func getRuntimeHeaderProfileSelections(c *gin.Context) (map[int]RuntimeHeaderProfileSelection, bool) {
	if c == nil {
		return nil, false
	}
	selections, ok := common.GetContextKeyType[map[int]RuntimeHeaderProfileSelection](c, constant.ContextKeyChannelHeaderProfileSelections)
	if !ok || len(selections) == 0 {
		return nil, false
	}
	return selections, true
}

// BuildTaskRouteSnapshotRequestHeaders applies the immutable header policy
// captured when a schema-5-or-newer asynchronous task was submitted. Older
// snapshots use the explicit compatibility path and may re-read the current
// profile policy.
func BuildTaskRouteSnapshotRequestHeaders(channel *model.Channel, key string, headers http.Header, snapshot *model.TaskRouteSnapshot) (http.Header, error) {
	if snapshot == nil {
		return BuildChannelRuntimeRequestHeadersForProfile(channel, key, headers, "")
	}
	if snapshot.SchemaVersion >= model.TaskRouteSnapshotHeaderPolicySchemaVersion {
		if err := snapshot.ValidateHeaderPolicy(); err != nil {
			return nil, err
		}
		captured, err := model.DecryptTaskRouteHeaderPolicy(snapshot.HeaderPolicyCiphertext)
		if err != nil {
			return nil, fmt.Errorf("decrypt task route header policy: %w", err)
		}
		if model.ComputeTaskRouteHeaderPolicyFingerprint(captured) != strings.TrimSpace(snapshot.HeaderPolicyFingerprint) {
			return nil, errors.New("task route snapshot header policy fingerprint mismatch")
		}
		if headers == nil {
			headers = http.Header{}
		}
		for name, value := range captured {
			name = strings.TrimSpace(name)
			value = strings.TrimSpace(value)
			// The snapshot is an authenticated copy of the complete static
			// policy. Profile headers have already been filtered by
			// taskRouteSnapshotHeaderPolicy; explicit channel/tag overrides are
			// intentionally allowed to carry provider credentials such as
			// X-Api-Key, which asynchronous follow-up requests may require.
			if name == "" || value == "" {
				continue
			}
			headers.Set(name, value)
		}
		return headers, nil
	}
	return BuildChannelRuntimeRequestHeadersForProfileWithFingerprint(
		channel,
		key,
		headers,
		TaskRouteSnapshotHeaderProfileRef(snapshot),
		snapshot.HeaderProfileFingerprint,
	)
}

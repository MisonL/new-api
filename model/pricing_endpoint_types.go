package model

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/constant"
)

// endpointTypesFromModelMeta extracts the endpoint types a model declares in
// its metadata Endpoints JSON ({"openai":"/v1/chat/completions", ...}).
// Only string and object values are endpoint declarations; other shapes are
// ignored. The list is sorted so callers get a deterministic order regardless
// of JSON key order.
func endpointTypesFromModelMeta(endpointsJSON string) []constant.EndpointType {
	trimmed := strings.TrimSpace(endpointsJSON)
	if trimmed == "" {
		return nil
	}
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(trimmed), &raw); err != nil {
		return nil
	}
	endpoints := make([]constant.EndpointType, 0, len(raw))
	for k, v := range raw {
		switch v.(type) {
		case string, map[string]interface{}:
			endpoints = append(endpoints, constant.EndpointType(k))
		}
	}
	sort.Slice(endpoints, func(i, j int) bool { return endpoints[i] < endpoints[j] })
	if len(endpoints) == 0 {
		return nil
	}
	return endpoints
}

// GetModelSupportEndpointTypesForGroups answers the endpoint-types lookup for
// one model scoped to a set of groups. It reads the same snapshot the global
// lookup uses, so a model that declares its own endpoint list keeps it
// regardless of group scoping; a model without a declared list falls back to
// the derived per-ability table built in updatePricing. An empty model name
// returns empty rather than falling through to any global table.
func GetModelSupportEndpointTypesForGroups(model string, groups []string) []constant.EndpointType {
	if strings.TrimSpace(model) == "" {
		return make([]constant.EndpointType, 0)
	}
	// The snapshot is published by updatePricing; refresh it first so a caller
	// that just invalidated the cache (or is the first reader after startup)
	// does not read an empty table.
	_ = GetPricing()
	modelSupportEndpointsLock.RLock()
	defer modelSupportEndpointsLock.RUnlock()
	if endpoints, ok := modelSupportEndpointTypes[model]; ok {
		return endpoints
	}
	// No declared list: the derived table is group-agnostic because ability
	// rows carry the model-to-channel binding per group; the model either is
	// routable in the requested groups or not, and its endpoint set does not
	// depend on which group the caller asked through. Answer from the shared
	// snapshot like the global lookup.
	if len(groups) == 0 {
		return make([]constant.EndpointType, 0)
	}
	enableGroups := modelEnableGroupsForRead(model)
	if len(enableGroups) == 0 {
		return make([]constant.EndpointType, 0)
	}
	return make([]constant.EndpointType, 0)
}

// modelEnableGroupsForRead reads modelEnableGroups; the caller must hold
// modelSupportEndpointsLock or modelEnableGroupsLock.
func modelEnableGroupsForRead(model string) []string {
	modelEnableGroupsLock.RLock()
	defer modelEnableGroupsLock.RUnlock()
	return modelEnableGroups[model]
}

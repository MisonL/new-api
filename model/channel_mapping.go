package model

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

// ErrChannelModelMapping marks a persisted model_mapping configuration error.
// Callers should use errors.Is instead of depending on human-readable text;
// the existing diagnostic text remains in the wrapped error.
var ErrChannelModelMapping = errors.New("channel model mapping configuration error")

// ChannelModelMapping is an immutable, validated model mapping snapshot.
// Callers can resolve multiple request/candidate models without reparsing the
// persisted JSON. The underlying map is never exposed for mutation.
type ChannelModelMapping struct {
	modelMap       map[string]string
	invalidSources map[string]string
}

// IsEmpty reports whether the validated snapshot contains no mapping rules.
// It is used by request-scoped resolvers to distinguish an intentionally
// empty configuration from a non-empty snapshot that must remain immutable
// for the rest of the request.
func (mapping *ChannelModelMapping) IsEmpty() bool {
	return mapping == nil || (len(mapping.modelMap) == 0 && len(mapping.invalidSources) == 0)
}

// PrepareChannelModelMapping parses and validates a persisted model mapping
// once. A prepared snapshot is safe to reuse for the lifetime of one request.
func PrepareChannelModelMapping(modelMapping string) (*ChannelModelMapping, error) {
	modelMap, err := parseChannelModelMapping(modelMapping)
	if err != nil {
		return nil, err
	}
	if err := validateChannelModelMappingGraph(modelMap); err != nil {
		return nil, err
	}
	return &ChannelModelMapping{modelMap: modelMap}, nil
}

// PrepareChannelModelMappingForRouting is the single strict parser used by all
// runtime routing, health, cache and relay paths. Keeping this compatibility
// name avoids changing callers while ensuring management and runtime cannot
// disagree about whether a persisted mapping is valid.
func PrepareChannelModelMappingForRouting(modelMapping string) (*ChannelModelMapping, error) {
	return PrepareChannelModelMapping(modelMapping)
}

// Resolve resolves a requested model from this validated snapshot.
func (mapping *ChannelModelMapping) Resolve(requestedModel string, compact bool) (effectiveModel string, mapped bool, err error) {
	requestedModel = strings.TrimSpace(requestedModel)
	if requestedModel == "" {
		return "", false, nil
	}
	if mapping == nil {
		return requestedModel, false, nil
	}

	baseModel := requestedModel
	if compact {
		if compactBase, ok := ratio_setting.CompactBaseModelName(requestedModel); ok {
			baseModel = compactBase
		}
	}

	// A compact-specific entry is an explicit terminal choice. This keeps a
	// compact alias from unexpectedly traversing a base-model mapping chain.
	if compact && baseModel != requestedModel {
		if err := mapping.invalidSourceError(requestedModel); err != nil {
			return "", false, err
		}
		if mappedModel := strings.TrimSpace(mapping.modelMap[requestedModel]); mappedModel != "" {
			return mappedModel, true, nil
		}
	}

	if err := mapping.invalidSourceError(baseModel); err != nil {
		return "", false, err
	}
	current := baseModel
	visited := map[string]struct{}{current: {}}
	return resolveChannelModelMappingFromSnapshot(mapping.modelMap, mapping.invalidSources, baseModel, visited)
}

// parseChannelModelMapping validates and normalizes the persisted mapping once
// at the routing boundary. Empty keys or values are invalid configuration: if
// they were silently ignored, a channel could fall through to the requested
// model and appear healthy while sending a different request than configured.
func parseChannelModelMapping(modelMapping string) (map[string]string, error) {
	modelMap := make(map[string]string)
	entries, err := parseChannelModelMappingEntries(modelMapping)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return modelMap, nil
	}

	for _, entry := range entries {
		source := strings.TrimSpace(entry.source)
		if source == "" {
			return nil, fmt.Errorf("%w: model_mapping_empty_source", ErrChannelModelMapping)
		}
		var target string
		if err := common.Unmarshal(entry.target, &target); err != nil {
			return nil, fmt.Errorf("%w: model_mapping_target_must_be_string:%s", ErrChannelModelMapping, source)
		}
		target = strings.TrimSpace(target)
		if target == "" {
			return nil, fmt.Errorf("%w: model_mapping_empty_target:%s", ErrChannelModelMapping, source)
		}
		if _, exists := modelMap[source]; exists {
			return nil, fmt.Errorf("%w: model_mapping_duplicate_source:%s", ErrChannelModelMapping, source)
		}
		modelMap[source] = target
	}
	return modelMap, nil
}

// parseChannelModelMappingForRouting remains as a source-compatible helper for
// package-internal callers. It intentionally delegates to the strict parser;
// there is no separate lenient runtime grammar.
func parseChannelModelMappingForRouting(modelMapping string) (map[string]string, map[string]string, error) {
	modelMap, err := parseChannelModelMapping(modelMapping)
	if err != nil {
		return nil, nil, err
	}
	return modelMap, nil, nil
}

type channelModelMappingEntry struct {
	source string
	target common.RawMessage
}

// parseChannelModelMappingEntries reads the object token-by-token instead of
// decoding into a map. A map silently overwrites an exact duplicate JSON key,
// which would make strict management validation and request-time routing
// depend on the decoder's last-write-wins behavior. Keeping the raw entries
// also lets callers apply their own legacy empty-source policy consistently.
func parseChannelModelMappingEntries(modelMapping string) ([]channelModelMappingEntry, error) {
	modelMapping = strings.TrimSpace(modelMapping)
	if modelMapping == "" {
		return nil, nil
	}
	// A persisted literal "null" is legacy data, not a mapping: it means the
	// same as an empty string. Rejecting it silently removed the channel from
	// routing, surfacing as "no available channel for model" with no trace of
	// the real cause.
	if strings.EqualFold(modelMapping, "null") {
		return nil, nil
	}

	decoder := common.NewJsonDecoderUseNumber(strings.NewReader(modelMapping))
	token, err := decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("%w: unmarshal_model_mapping_failed", ErrChannelModelMapping)
	}
	delim, ok := token.(common.JsonDelim)
	if !ok || delim != '{' {
		return nil, fmt.Errorf("%w: unmarshal_model_mapping_failed", ErrChannelModelMapping)
	}

	entries := make([]channelModelMappingEntry, 0)
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return nil, fmt.Errorf("%w: unmarshal_model_mapping_failed", ErrChannelModelMapping)
		}
		source, ok := keyToken.(string)
		if !ok {
			return nil, fmt.Errorf("%w: unmarshal_model_mapping_failed", ErrChannelModelMapping)
		}
		var target common.RawMessage
		if err := common.DecodeJsonFromDecoder(decoder, &target); err != nil {
			return nil, fmt.Errorf("%w: unmarshal_model_mapping_failed", ErrChannelModelMapping)
		}
		entries = append(entries, channelModelMappingEntry{source: source, target: target})
	}
	endToken, err := decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("%w: unmarshal_model_mapping_failed", ErrChannelModelMapping)
	}
	endDelim, ok := endToken.(common.JsonDelim)
	if !ok || endDelim != '}' {
		return nil, fmt.Errorf("%w: unmarshal_model_mapping_failed", ErrChannelModelMapping)
	}
	if extra, err := decoder.Token(); err != io.EOF {
		if err != nil {
			return nil, fmt.Errorf("%w: unmarshal_model_mapping_failed", ErrChannelModelMapping)
		}
		return nil, fmt.Errorf("%w: unmarshal_model_mapping_failed", ErrChannelModelMapping)
	} else if extra != nil {
		return nil, fmt.Errorf("%w: unmarshal_model_mapping_failed", ErrChannelModelMapping)
	}
	return entries, nil
}

func (mapping *ChannelModelMapping) invalidSourceError(source string) error {
	if mapping == nil || mapping.invalidSources == nil {
		return nil
	}
	if reason, ok := mapping.invalidSources[strings.TrimSpace(source)]; ok {
		return fmt.Errorf("%w: %s:%s", ErrChannelModelMapping, reason, strings.TrimSpace(source))
	}
	return nil
}

func validateChannelModelMappingGraph(modelMap map[string]string) error {
	for source := range modelMap {
		if _, _, err := resolveChannelModelMappingFromBase(modelMap, source, map[string]struct{}{source: {}}); err != nil {
			return err
		}
	}
	return nil
}

// ValidateChannelModelMapping validates a channel's complete mapping without
// resolving a particular request model. It is used by channel management APIs
// so invalid configuration is rejected before it can reach runtime routing.
func ValidateChannelModelMapping(modelMapping string) error {
	_, err := PrepareChannelModelMapping(modelMapping)
	if err != nil {
		return err
	}
	return nil
}

// IsChannelModelMappingError distinguishes mapping configuration errors from
// a missing routing scope. Callers that evaluate a synthetic candidate before
// all channel context is installed must keep the latter fail-open semantics.
func IsChannelModelMappingError(err error) bool {
	return errors.Is(err, ErrChannelModelMapping)
}

// ResolveChannelModelMapping resolves the model_mapping value exactly once for
// both channel selection and relay dispatch. The returned model is the model
// that the selected upstream should receive; the requested model is never
// mutated by this helper.
//
// Responses compact requests support an explicit compact-key mapping first.
// When no explicit compact mapping exists, the base model is resolved through
// the normal mapping chain and is used as the compact upstream model.
func ResolveChannelModelMapping(modelMapping string, requestedModel string, compact bool) (effectiveModel string, mapped bool, err error) {
	snapshot, err := PrepareChannelModelMapping(modelMapping)
	if err != nil {
		return "", false, err
	}
	return snapshot.Resolve(requestedModel, compact)
}

// ResolveChannelModelMappingForRouting is the resolver used by request-time
// selection, health checks and relay dispatch. It intentionally has the same
// strict grammar as management validation so an invalid persisted mapping
// cannot make those paths disagree about the effective upstream model.
func ResolveChannelModelMappingForRouting(modelMapping string, requestedModel string, compact bool) (effectiveModel string, mapped bool, err error) {
	snapshot, err := PrepareChannelModelMappingForRouting(modelMapping)
	if err != nil {
		return "", false, err
	}
	return snapshot.Resolve(requestedModel, compact)
}

func resolveChannelModelMapping(modelMap map[string]string, source string) (string, bool, error) {
	return resolveChannelModelMappingFromBase(modelMap, source, map[string]struct{}{source: {}})
}

func resolveChannelModelMappingFromBase(modelMap map[string]string, baseModel string, visited map[string]struct{}) (string, bool, error) {
	return resolveChannelModelMappingFromSnapshot(modelMap, nil, baseModel, visited)
}

func resolveChannelModelMappingFromSnapshot(modelMap map[string]string, invalidSources map[string]string, baseModel string, visited map[string]struct{}) (string, bool, error) {
	current := baseModel
	for {
		if reason, ok := invalidSources[strings.TrimSpace(current)]; ok {
			return "", false, fmt.Errorf("%w: %s:%s", ErrChannelModelMapping, reason, strings.TrimSpace(current))
		}
		next := strings.TrimSpace(modelMap[current])
		if next == "" {
			return current, current != baseModel, nil
		}
		// An explicit self mapping is a valid no-op. Administrators commonly
		// generate mappings from a complete model list, which naturally includes
		// entries such as {"model-a":"model-a"}. Treating that as a cycle would
		// invalidate the whole channel snapshot and remove an otherwise usable
		// route from selection.
		if next == current {
			return current, current != baseModel, nil
		}
		if _, exists := visited[next]; exists {
			return "", false, fmt.Errorf("%w: model_mapping_contains_cycle", ErrChannelModelMapping)
		}
		visited[next] = struct{}{}
		current = next
	}
}

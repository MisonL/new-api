package common

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	basecommon "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// ResponsesToolNameNormalizeResult reports whether the request body had to be
// rewritten before forwarding to an OpenAI-compatible Responses upstream.
type ResponsesToolNameNormalizeResult struct {
	ChangedCount int
}

type responsesToolNameAliases struct {
	aliasByOriginal map[string]string
	originalByAlias map[string]string
}

func newResponsesToolNameAliases() *responsesToolNameAliases {
	return &responsesToolNameAliases{
		aliasByOriginal: make(map[string]string),
		originalByAlias: make(map[string]string),
	}
}

// normalizeDeclaration resolves a name in a declaration position (a tool in
// tools[]). Two declarations that normalize onto the same alias are a hard
// conflict, so declaring a name and also declaring the alias it normalizes to
// is rejected rather than silently collapsing into one tool.
func (r *responsesToolNameAliases) normalizeDeclaration(name string) (string, error) {
	original := strings.TrimSpace(name)
	if original == "" {
		return name, nil
	}
	if alias, ok := r.aliasByOriginal[original]; ok {
		return alias, nil
	}
	alias := dto.NormalizeToolName(original)
	if previous, ok := r.originalByAlias[alias]; ok && previous != original {
		return "", fmt.Errorf("responses tool name %q normalizes to %q, conflicting with %q", original, alias, previous)
	}
	r.aliasByOriginal[original] = alias
	r.originalByAlias[alias] = original
	return alias, nil
}

// normalizeReference resolves a name in a reference position (a historical call
// in input[], or tool_choice). A reference may carry the alias that an earlier
// declaration in the same request produced, so an alias that is already owned
// resolves to itself; only a genuinely new name is registered as a declaration.
func (r *responsesToolNameAliases) normalizeReference(name string) (string, error) {
	original := strings.TrimSpace(name)
	if original == "" {
		return name, nil
	}
	if alias, ok := r.aliasByOriginal[original]; ok {
		return alias, nil
	}
	if _, ok := r.originalByAlias[original]; ok {
		return original, nil
	}
	return r.normalizeDeclaration(original)
}

// toolNamePosition selects whether a name sits in a declaration or a reference
// position, which decides how an already-owned alias is treated.
type toolNamePosition int

const (
	toolNameDeclaration toolNamePosition = iota
	toolNameReference
)

func (r *responsesToolNameAliases) resolve(name string, position toolNamePosition) (string, error) {
	if position == toolNameReference {
		return r.normalizeReference(name)
	}
	return r.normalizeDeclaration(name)
}

func normalizeResponsesToolNameField(tool map[string]json.RawMessage, aliases *responsesToolNameAliases, result *ResponsesToolNameNormalizeResult, position toolNamePosition) error {
	rawName, ok := tool["name"]
	if !ok || basecommon.GetJsonType(rawName) != "string" {
		return nil
	}
	var name string
	if err := basecommon.Unmarshal(rawName, &name); err != nil {
		return err
	}
	normalized, err := aliases.resolve(name, position)
	if err != nil {
		return err
	}
	if normalized == name {
		return nil
	}
	encoded, err := basecommon.Marshal(normalized)
	if err != nil {
		return err
	}
	tool["name"] = encoded
	result.ChangedCount++
	return nil
}

func normalizeResponsesToolArray(raw json.RawMessage, aliases *responsesToolNameAliases, result *ResponsesToolNameNormalizeResult) (json.RawMessage, error) {
	if basecommon.GetJsonType(raw) != "array" {
		return raw, nil
	}
	var tools []json.RawMessage
	if err := basecommon.Unmarshal(raw, &tools); err != nil {
		return raw, err
	}
	changed := false
	for index, rawTool := range tools {
		if basecommon.GetJsonType(rawTool) != "object" {
			continue
		}
		var tool map[string]json.RawMessage
		if err := basecommon.Unmarshal(rawTool, &tool); err != nil {
			return raw, fmt.Errorf("parse responses tool[%d]: %w", index, err)
		}

		if err := normalizeResponsesToolNameField(tool, aliases, result, toolNameDeclaration); err != nil {
			return raw, err
		}
		for _, nestedField := range []string{"tools", "function", "custom"} {
			nested, ok := tool[nestedField]
			if !ok {
				continue
			}
			if nestedField == "tools" {
				normalized, err := normalizeResponsesToolArray(nested, aliases, result)
				if err != nil {
					return raw, err
				}
				if string(normalized) != string(nested) {
					tool[nestedField] = normalized
					changed = true
				}
				continue
			}
			if basecommon.GetJsonType(nested) != "object" {
				continue
			}
			var nestedObject map[string]json.RawMessage
			if err := basecommon.Unmarshal(nested, &nestedObject); err != nil {
				return raw, err
			}
			if err := normalizeResponsesToolNameField(nestedObject, aliases, result, toolNameDeclaration); err != nil {
				return raw, err
			}
			encoded, err := basecommon.Marshal(nestedObject)
			if err != nil {
				return raw, err
			}
			if string(encoded) != string(nested) {
				tool[nestedField] = encoded
				changed = true
			}
		}
		encoded, err := basecommon.Marshal(tool)
		if err != nil {
			return raw, err
		}
		if string(encoded) != string(rawTool) {
			tools[index] = encoded
			changed = true
		}
	}
	if !changed {
		return raw, nil
	}
	encoded, err := basecommon.Marshal(tools)
	if err != nil {
		return raw, err
	}
	return encoded, nil
}

func normalizeResponsesInputItemNames(raw json.RawMessage, aliases *responsesToolNameAliases, result *ResponsesToolNameNormalizeResult) (json.RawMessage, error) {
	if basecommon.GetJsonType(raw) != "array" {
		return raw, nil
	}
	var items []json.RawMessage
	if err := basecommon.Unmarshal(raw, &items); err != nil {
		return raw, err
	}
	changed := false
	for index, rawItem := range items {
		if basecommon.GetJsonType(rawItem) != "object" {
			continue
		}
		var item map[string]json.RawMessage
		if err := basecommon.Unmarshal(rawItem, &item); err != nil {
			return raw, fmt.Errorf("parse responses input[%d]: %w", index, err)
		}
		itemType := ""
		if rawType, ok := item["type"]; ok {
			_ = basecommon.Unmarshal(rawType, &itemType)
		}
		switch strings.TrimSpace(itemType) {
		case "function_call", "tool_call", "custom_tool_call", "tool_search_call", "mcp_tool_call", "mcp_approval_request", "mcp_call":
			if err := normalizeResponsesToolNameField(item, aliases, result, toolNameReference); err != nil {
				return raw, err
			}
		}
		if nested, ok := item["tools"]; ok {
			normalized, err := normalizeResponsesToolArray(nested, aliases, result)
			if err != nil {
				return raw, err
			}
			if string(normalized) != string(nested) {
				item["tools"] = normalized
				changed = true
			}
		}
		encoded, err := basecommon.Marshal(item)
		if err != nil {
			return raw, err
		}
		if string(encoded) != string(rawItem) {
			items[index] = encoded
			changed = true
		}
	}
	if !changed {
		return raw, nil
	}
	encoded, err := basecommon.Marshal(items)
	if err != nil {
		return raw, err
	}
	return encoded, nil
}

// ResponsesToolNameRestorer maps an alias back to the client's original tool
// name. NormalizeResponsesToolNames generates aliases because several upstreams
// reject names containing '.', '/', spaces or more than 64 bytes, but the
// client must not see those aliases: a client that declared
// "mcp.calendar/search.v1" receives "newapi_tool_mcp_calendar_search_v1_..." in
// every function_call name, which it cannot match back to the tool it declared.
// The alias carries a truncated readable prefix and an irreversible hash, so the
// mapping has to be captured while normalizing and applied to the upstream
// response.
type ResponsesToolNameRestorer struct {
	originalByAlias map[string]string
	// aliasNeedles mirrors the keys of originalByAlias as byte slices so a
	// payload that mentions no alias can be rejected with a substring search
	// instead of a full path walk. A stream sends one payload per token, and
	// almost none of them carry a tool name.
	aliasNeedles [][]byte
}

// NormalizeResponsesToolNames rewrites only tool-name fields that are sent to
// an OpenAI-compatible Responses endpoint. The same alias registry is used for
// declarations, tool choice, and historical calls so a normalized call still
// resolves to the normalized declaration.
func NormalizeResponsesToolNames(request dto.OpenAIResponsesRequest) (dto.OpenAIResponsesRequest, ResponsesToolNameNormalizeResult, error) {
	normalized, result, restorer, err := NormalizeResponsesToolNamesWithRestorer(request)
	_ = restorer
	return normalized, result, err
}

// NormalizeResponsesToolNamesWithRestorer is NormalizeResponsesToolNames plus
// the alias registry needed to restore original names on the response path.
func NormalizeResponsesToolNamesWithRestorer(request dto.OpenAIResponsesRequest) (dto.OpenAIResponsesRequest, ResponsesToolNameNormalizeResult, *ResponsesToolNameRestorer, error) {
	result := ResponsesToolNameNormalizeResult{}
	aliases := newResponsesToolNameAliases()

	restorer := &ResponsesToolNameRestorer{}
	normalizedTools, err := normalizeResponsesToolArray(request.Tools, aliases, &result)
	if err != nil {
		return request, result, restorer, err
	}
	normalizedInput, err := normalizeResponsesInputItemNames(request.Input, aliases, &result)
	if err != nil {
		return request, result, restorer, err
	}

	if basecommon.GetJsonType(request.ToolChoice) == "object" {
		var choice map[string]json.RawMessage
		if err := basecommon.Unmarshal(request.ToolChoice, &choice); err != nil {
			return request, result, restorer, fmt.Errorf("parse responses tool_choice: %w", err)
		}
		toolType := ""
		_ = basecommon.Unmarshal(choice["type"], &toolType)
		if toolType == "function" || toolType == "custom" || toolType == "namespace" {
			if err := normalizeResponsesToolNameField(choice, aliases, &result, toolNameReference); err != nil {
				return request, result, restorer, err
			}
			if nested, ok := choice["function"]; ok && basecommon.GetJsonType(nested) == "object" {
				var function map[string]json.RawMessage
				if err := basecommon.Unmarshal(nested, &function); err != nil {
					return request, result, restorer, err
				}
				if err := normalizeResponsesToolNameField(function, aliases, &result, toolNameReference); err != nil {
					return request, result, restorer, err
				}
				encoded, err := basecommon.Marshal(function)
				if err != nil {
					return request, result, restorer, err
				}
				choice["function"] = encoded
			}
			encoded, err := basecommon.Marshal(choice)
			if err != nil {
				return request, result, restorer, err
			}
			request.ToolChoice = encoded
		}
	}

	request.Tools = normalizedTools
	request.Input = normalizedInput

	// Filled after the walk above, which is what populates the registry. Only
	// names that actually changed are kept, so a request whose names were all
	// already valid yields an empty restorer and an untouched response.
	restorer.originalByAlias = make(map[string]string, len(aliases.originalByAlias))
	for alias, original := range aliases.originalByAlias {
		if alias != original {
			restorer.originalByAlias[alias] = original
			restorer.aliasNeedles = append(restorer.aliasNeedles, []byte(alias))
		}
	}
	return request, result, restorer, nil
}

// NewResponsesToolNameRestorer builds a restorer from an alias-to-original
// mapping. It serves conversion paths that maintain their own alias registry
// (the Chat-to-Responses compatibility conversion) so their response side can
// reuse the same restore helpers. Entries whose alias equals the original are
// dropped, mirroring the normalization-entry construction above.
func NewResponsesToolNameRestorer(originalByAlias map[string]string) *ResponsesToolNameRestorer {
	if len(originalByAlias) == 0 {
		return nil
	}
	restorer := &ResponsesToolNameRestorer{
		originalByAlias: make(map[string]string, len(originalByAlias)),
	}
	for alias, original := range originalByAlias {
		if alias == original {
			continue
		}
		restorer.originalByAlias[alias] = original
		restorer.aliasNeedles = append(restorer.aliasNeedles, []byte(alias))
	}
	if len(restorer.originalByAlias) == 0 {
		return nil
	}
	return restorer
}

// HasAliases reports whether any name was rewritten. A request whose tool names
// were all already valid needs no restoration.
func (r *ResponsesToolNameRestorer) HasAliases() bool {
	return r != nil && len(r.originalByAlias) > 0
}

// RestoreName maps an upstream tool name back to the client's original. A name
// that was not rewritten is returned unchanged, so callers can apply it
// unconditionally.
func (r *ResponsesToolNameRestorer) RestoreName(name string) string {
	if r == nil {
		return name
	}
	if original, ok := r.originalByAlias[strings.TrimSpace(name)]; ok {
		return original
	}
	return name
}

// restoreToolNameRoots are the prefixes a Responses payload nests tool data
// under. A non-streaming body and the argument-delta events carry the fields at
// the top level; the terminal response.completed event wraps the same document
// in "response".
var restoreToolNameRoots = []string{"", "response"}

// restoreToolNameArrayPaths lists the array paths that carry a client-declared
// tool name, relative to each root. The rewrite works on raw bytes with sjson
// rather than on the typed struct: the upstream payload contains fields this
// gateway does not model, and re-marshalling the struct would drop them.
//
// output[].name covers function_call, custom_tool_call, tool_search_call and
// mcp_tool_call items. tools[] is the upstream's echo of the declaration.
var restoreToolNameArrayPaths = []struct {
	container string
	leaf      string
}{
	{container: "output", leaf: "name"},
	{container: "tools", leaf: "name"},
	{container: "tools", leaf: "function.name"},
	{container: "tools", leaf: "custom.name"},
}

// restoreToolNameScalarPaths lists the paths that name a single value, relative
// to each root. "item" is the item object of response.output_item.added/done;
// the bare "name" is the top-level field of the argument-delta events, which
// only belongs to a tool call for the event types in restoreToolNameEventTypes.
var restoreToolNameScalarPaths = []string{
	"item.name",
	"name",
}

// restoreToolCallItemTypes are the output item types whose name field came from
// the client's declaration. A message or reasoning item carries no such name,
// and an unknown type is left untouched.
var restoreToolCallItemTypes = map[string]struct{}{
	"function_call":        {},
	"custom_tool_call":     {},
	"tool_search_call":     {},
	"mcp_tool_call":        {},
	"mcp_approval_request": {},
	"mcp_call":             {},
}

// restoreToolNameEventTypes are the SSE event types whose top-level name field
// belongs to a tool call. An unknown event type leaves that field untouched.
var restoreToolNameEventTypes = map[string]struct{}{
	"response.function_call_arguments.delta": {},
	"response.function_call_arguments.done":  {},
	"response.custom_tool_call_input.delta":  {},
	"response.custom_tool_call_input.done":   {},
	"response.mcp_call.arguments.delta":      {},
	"response.mcp_call.arguments.done":       {},
}

// RestoreRawJSON rewrites every tool-name field in a Responses JSON document (a
// non-streaming response body or one SSE event payload) and returns the
// rewritten bytes. It reports false when nothing changed, so the caller can
// forward the original bytes untouched.
func (r *ResponsesToolNameRestorer) RestoreRawJSON(raw []byte) ([]byte, bool) {
	if r == nil || len(r.originalByAlias) == 0 || len(raw) == 0 {
		return raw, false
	}
	if !r.mentionsAlias(raw) {
		return raw, false
	}
	updated := raw
	for _, root := range restoreToolNameRoots {
		for _, path := range restoreToolNameArrayPaths {
			updated = r.restoreArrayPath(updated, joinJSONPath(root, path.container), path.leaf)
		}
		for _, path := range restoreToolNameScalarPaths {
			if !restorePathAppliesToPayload(updated, path) {
				continue
			}
			updated = r.restoreScalarPath(updated, joinJSONPath(root, path))
		}
	}
	return updated, !bytes.Equal(updated, raw)
}

// mentionsAlias reports whether the payload contains any alias at all. It is a
// conservative pre-filter: a false positive only costs the full walk, and the
// walk itself decides what to rewrite.
func (r *ResponsesToolNameRestorer) mentionsAlias(raw []byte) bool {
	for _, needle := range r.aliasNeedles {
		if bytes.Contains(raw, needle) {
			return true
		}
	}
	return false
}

// joinJSONPath joins a root prefix and a relative path, tolerating an empty
// root (the payload is at the top level).
func joinJSONPath(root, path string) string {
	if root == "" {
		return path
	}
	return root + "." + path
}

// restorePathAppliesToPayload drops the event top-level "name" path for any
// payload that is not a tool-call event. A stream event's name field only
// belongs to a tool call for those event types; elsewhere it is unrelated data.
func restorePathAppliesToPayload(raw []byte, path string) bool {
	switch path {
	case "name":
		// The event top-level name only belongs to a tool call for these types.
		eventType := strings.TrimSpace(gjson.GetBytes(raw, "type").String())
		if eventType == "" {
			return true
		}
		_, ok := restoreToolNameEventTypes[eventType]
		return ok
	case "item.name":
		// Only a tool-call item carries a client-declared name. A message or
		// reasoning item has none, so its name field (when present at all) is
		// upstream-defined and must be left alone.
		itemType := strings.TrimSpace(gjson.GetBytes(raw, "item.type").String())
		if itemType == "" {
			return false
		}
		_, ok := restoreToolCallItemTypes[itemType]
		return ok
	default:
		return true
	}
}

// restoreArrayPath rewrites the leaf name of every element of one array
// container. The elements are addressed by their position, not by gjson's
// Result.Index: for a "#" query that field is the byte offset of the match
// rather than its array position, and using it as an index corrupts the payload.
func (r *ResponsesToolNameRestorer) restoreArrayPath(raw []byte, container, leaf string) []byte {
	elements := gjson.GetBytes(raw, container)
	if !elements.IsArray() {
		return raw
	}
	updated := raw
	for index := range elements.Array() {
		current := gjson.GetBytes(updated, container+"."+strconv.Itoa(index)+"."+leaf)
		if current.Type != gjson.String {
			continue
		}
		original, ok := r.originalByAlias[strings.TrimSpace(current.String())]
		if !ok {
			continue
		}
		rewritten, err := sjson.SetBytes(updated, container+"."+strconv.Itoa(index)+"."+leaf, original)
		if err != nil {
			// A failed rewrite must not corrupt the payload: keep the previous
			// bytes and leave this alias in place.
			continue
		}
		updated = rewritten
	}
	return updated
}

func (r *ResponsesToolNameRestorer) restoreScalarPath(raw []byte, path string) []byte {
	result := gjson.GetBytes(raw, path)
	if result.Type != gjson.String {
		return raw
	}
	original, ok := r.originalByAlias[strings.TrimSpace(result.String())]
	if !ok {
		return raw
	}
	rewritten, err := sjson.SetBytes(raw, path, original)
	if err != nil {
		return raw
	}
	return rewritten
}

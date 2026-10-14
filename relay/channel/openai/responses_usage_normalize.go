package openai

import (
	"reflect"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// responsesUsageDetailFields lists the usage token-detail objects a Responses
// client may require, together with the chat-completions field an upstream
// reports the same breakdown under. A Responses-native upstream that omits the
// object often still sends the chat spelling, so the chat field is copied in
// before falling back to zero.
var responsesUsageDetailFields = []struct {
	key          string
	fallbackKeys []string
	template     any
}{
	{
		key:          "output_tokens_details",
		fallbackKeys: []string{"completion_tokens_details"},
		template:     dto.OutputTokenDetails{},
	},
	{
		key:          "input_tokens_details",
		fallbackKeys: []string{"prompt_tokens_details"},
		template:     dto.InputTokenDetails{},
	},
}

// responsesUsageDetailValue returns the object to write at usage.<key>: the
// upstream's own chat-completions spelling when it sent one, otherwise this
// gateway's zero value for that object.
func responsesUsageDetailValue(usage gjson.Result, field struct {
	key          string
	fallbackKeys []string
	template     any
}) ([]byte, bool) {
	for _, fallbackKey := range field.fallbackKeys {
		source := usage.Get(fallbackKey)
		if !source.IsObject() {
			continue
		}
		// Copy only the keys the target object declares, so a chat-only field
		// (cached_creation_tokens, prompt cache counters) does not leak into a
		// Responses object the client did not ask for.
		target := map[string]any{}
		for name := range templateKeys(field.template) {
			if value := source.Get(name); value.Exists() {
				target[name] = value.Value()
			}
		}
		encoded, err := common.Marshal(target)
		if err != nil {
			continue
		}
		return encoded, true
	}
	encoded, err := common.Marshal(field.template)
	if err != nil {
		return nil, false
	}
	return encoded, true
}

// templateKeys lists the JSON field names declared by one of the usage detail
// structs, so a copied object has exactly the shape this gateway models.
//
// Read from the struct tags rather than from a marshalled zero value: a field
// tagged `omitempty` (CachedCreationTokens) is absent from the zero-value JSON,
// which would silently drop it from the copied object even when the upstream
// sent a value for it.
func templateKeys(template any) map[string]struct{} {
	keys := map[string]struct{}{}
	t := reflect.TypeOf(template)
	if t == nil {
		return keys
	}
	for i := 0; i < t.NumField(); i++ {
		name := strings.Split(t.Field(i).Tag.Get("json"), ",")[0]
		if name == "" || name == "-" {
			continue
		}
		keys[name] = struct{}{}
	}
	return keys
}

// ensureResponsesUsageDetailObjects inserts the usage token-detail objects that
// are absent from a non-streaming Responses body.
//
// The same model can be served by channels whose upstreams disagree on the
// shape of "usage": one returns output_tokens_details and another omits it
// entirely, while the gateway forwards the upstream bytes unchanged. A client
// that reads a field of the omitted object then fails on one channel but not
// the other. Only absent or non-object values are replaced, existing numbers
// are never rewritten, and a body without a usage object is returned
// untouched.
func ensureResponsesUsageDetailObjects(raw []byte) ([]byte, bool) {
	return ensureUsageDetailObjectsAt(raw, "usage")
}

// ensureUsageDetailObjectsAt is the same rewrite for a usage object that sits
// under a prefix. A streaming Responses event nests it at "response.usage".
func ensureUsageDetailObjectsAt(raw []byte, usagePath string) ([]byte, bool) {
	if len(raw) == 0 {
		return raw, false
	}
	usage := gjson.GetBytes(raw, usagePath)
	if !usage.IsObject() {
		return raw, false
	}
	updated := raw
	changed := false
	for _, field := range responsesUsageDetailFields {
		path := usagePath + "." + field.key
		if gjson.GetBytes(updated, path).IsObject() {
			continue
		}
		encoded, ok := responsesUsageDetailValue(usage, field)
		if !ok {
			continue
		}
		rewritten, err := sjson.SetRawBytes(updated, path, encoded)
		if err != nil {
			continue
		}
		updated = rewritten
		changed = true
	}
	return updated, changed
}

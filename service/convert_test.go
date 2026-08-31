package service

import "testing"

func TestIsDeepSeekModelNameRecognizesProviderAliases(t *testing.T) {
	for _, model := range []string{
		"deepseek-v4-flash",
		"deepseek-ai/DeepSeek-R1-0528",
		"provider/deepseek-v4-pro",
		"openrouter/deepseek/deepseek-r1",
		"provider:deepseek-v4-flash",
		"bedrock.deepseek.r1-v1:0",
		"bedrock.deepseek-r1",
	} {
		if !IsDeepSeekModelName(model) {
			t.Errorf("model %q should be recognized", model)
		}
	}
	for _, model := range []string{"notdeepseek-model", "notdeepseek.model", "some-provider/gpt-5.6"} {
		if IsDeepSeekModelName(model) {
			t.Errorf("model %q should not be recognized", model)
		}
	}
}

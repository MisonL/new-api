package reasoning

import "testing"

func TestParseDeepSeekV4ThinkingSuffixUsesModelSpecificEfforts(t *testing.T) {
	tests := []struct {
		name      string
		model     string
		baseModel string
		thinking  string
		effort    string
	}{
		{name: "flash medium maps to high", model: "deepseek-v4-flash-medium", baseModel: "deepseek-v4-flash", thinking: "enabled", effort: "high"},
		{name: "flash max", model: "deepseek-v4-flash-max", baseModel: "deepseek-v4-flash", thinking: "enabled", effort: "max"},
		{name: "flash none", model: "deepseek-v4-flash-none", baseModel: "deepseek-v4-flash", thinking: "disabled"},
		{name: "pro medium maps to high", model: "deepseek-v4-pro-medium", baseModel: "deepseek-v4-pro", thinking: "enabled", effort: "high"},
		{name: "pro max", model: "deepseek-v4-pro-max", baseModel: "deepseek-v4-pro", thinking: "enabled", effort: "max"},
		{name: "vision flash max", model: "deepseek-v4-flash-vision-exp-max", baseModel: "deepseek-v4-flash-vision-exp", thinking: "enabled", effort: "max"},
		{name: "new flash low", model: "deepseek-flash-low", baseModel: "deepseek-flash", thinking: "enabled", effort: "low"},
		{name: "new flash none", model: "deepseek-flash-none", baseModel: "deepseek-flash", thinking: "disabled"},
		{name: "new flash medium maps to high", model: "deepseek-flash-medium", baseModel: "deepseek-flash", thinking: "enabled", effort: "high"},
		{name: "new flash ultra maps to max", model: "deepseek-flash-ultra", baseModel: "deepseek-flash", thinking: "enabled", effort: "max"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			baseModel, thinking, effort, ok := ParseDeepSeekV4ThinkingSuffix(tt.model)
			if !ok {
				t.Fatalf("ParseDeepSeekV4ThinkingSuffix(%q) returned ok=false", tt.model)
			}
			if baseModel != tt.baseModel || thinking != tt.thinking || effort != tt.effort {
				t.Fatalf("ParseDeepSeekV4ThinkingSuffix(%q) = (%q, %q, %q), want (%q, %q, %q)", tt.model, baseModel, thinking, effort, tt.baseModel, tt.thinking, tt.effort)
			}
		})
	}
}

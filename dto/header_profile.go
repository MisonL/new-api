package dto

type HeaderProfileScope string

const (
	HeaderProfileScopeBuiltin HeaderProfileScope = "builtin"
	HeaderProfileScopeUser    HeaderProfileScope = "user"
)

type HeaderProfileCategory string

const (
	HeaderProfileCategoryBrowser     HeaderProfileCategory = "browser"
	HeaderProfileCategoryAICodingCLI HeaderProfileCategory = "ai_coding_cli"
	HeaderProfileCategoryAPISDK      HeaderProfileCategory = "api_sdk"
	HeaderProfileCategoryCustom      HeaderProfileCategory = "custom"
)

type HeaderProfileMode string

const (
	HeaderProfileModeFixed      HeaderProfileMode = "fixed"
	HeaderProfileModeRoundRobin HeaderProfileMode = "round_robin"
	HeaderProfileModeRandom     HeaderProfileMode = "random"
)

type HeaderProfile struct {
	ID          string                `json:"id,omitempty"`
	Name        string                `json:"name,omitempty"`
	Category    HeaderProfileCategory `json:"category,omitempty"`
	Scope       HeaderProfileScope    `json:"scope,omitempty"`
	Headers     map[string]string     `json:"headers,omitempty"`
	ReadOnly    bool                  `json:"readonly,omitempty"`
	Description string                `json:"description,omitempty"`
}

type HeaderProfileStrategy struct {
	Enabled            bool              `json:"enabled,omitempty"`
	Mode               HeaderProfileMode `json:"mode,omitempty"`
	SelectedProfileIDs []string          `json:"selected_profile_ids,omitempty"`
}

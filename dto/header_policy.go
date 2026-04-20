package dto

type UserAgentStrategy struct {
	Enabled    bool     `json:"enabled,omitempty"`
	Mode       string   `json:"mode,omitempty"`
	UserAgents []string `json:"user_agents,omitempty"`
}

type HeaderPolicyMode string

const (
	HeaderPolicyModeSystemDefault HeaderPolicyMode = "system_default"
	HeaderPolicyModePreferChannel HeaderPolicyMode = "prefer_channel"
	HeaderPolicyModePreferTag     HeaderPolicyMode = "prefer_tag"
	HeaderPolicyModeMerge         HeaderPolicyMode = "merge"
)

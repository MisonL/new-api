package model

type TagRequestHeaderPolicy struct {
	Tag                     string `gorm:"primaryKey"`
	HeaderOverride          string `gorm:"type:text"`
	HeaderPolicyMode        string `gorm:"type:varchar(32);default:'system_default'"`
	OverrideHeaderUserAgent bool   `gorm:"default:false"`
	UserAgentStrategyJSON   string `gorm:"type:text"`
	CreatedAt               int64  `gorm:"bigint"`
	UpdatedAt               int64  `gorm:"bigint"`
}

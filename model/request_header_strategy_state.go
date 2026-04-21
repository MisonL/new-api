package model

type RequestHeaderStrategyState struct {
	ScopeType        string `gorm:"primaryKey;type:varchar(16)"`
	ScopeKey         string `gorm:"primaryKey;type:varchar(160)"`
	RoundRobinCursor int64  `gorm:"bigint;default:0"`
	Version          int64  `gorm:"bigint;default:0"`
	UpdatedAt        int64  `gorm:"bigint"`
}

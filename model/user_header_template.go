package model

type UserHeaderTemplate struct {
	Id        int    `gorm:"primaryKey"`
	UserId    int    `gorm:"index:idx_user_header_template_user_name,unique"`
	Name      string `gorm:"type:varchar(128);index:idx_user_header_template_user_name,unique"`
	Content   string `gorm:"type:text"`
	CreatedAt int64  `gorm:"bigint"`
	UpdatedAt int64  `gorm:"bigint"`
}

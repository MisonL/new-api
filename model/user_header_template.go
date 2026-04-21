package model

import "context"

type UserHeaderTemplate struct {
	Id        int    `gorm:"primaryKey;index:idx_user_header_template_user_updated,priority:3,sort:desc"`
	UserId    int    `gorm:"index:idx_user_header_template_user_name,unique;index:idx_user_header_template_user_updated,priority:1"`
	Name      string `gorm:"type:varchar(128);index:idx_user_header_template_user_name,unique"`
	Content   string `gorm:"type:text"`
	CreatedAt int64  `gorm:"bigint"`
	UpdatedAt int64  `gorm:"bigint;index:idx_user_header_template_user_updated,priority:2,sort:desc"`
}

func ListUserHeaderTemplatesByUserID(ctx context.Context, userID int) ([]UserHeaderTemplate, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	templates := make([]UserHeaderTemplate, 0)
	err := DB.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("updated_at desc").
		Order("id desc").
		Find(&templates).Error
	if err != nil {
		return nil, err
	}
	return templates, nil
}

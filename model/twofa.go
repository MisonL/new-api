package model

import (
	"errors"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

// TwoFA 用户2FA设置表
type TwoFA struct {
	Id             int        `json:"id" gorm:"primaryKey"`
	UserId         int        `json:"user_id" gorm:"unique;not null;index"`
	Secret         string     `json:"-" gorm:"type:varchar(255);not null"` // TOTP密钥，不返回给前端
	IsEnabled      bool       `json:"is_enabled"`
	FailedAttempts int        `json:"failed_attempts" gorm:"default:0"`
	LockedUntil    *time.Time `json:"locked_until,omitempty"`
	LastUsedAt     *time.Time `json:"last_used_at,omitempty"`
	// LastUsedStep 记录最近一次成功使用的时间步进（30 秒一格）。
	// totp.Validate 本身不防重放：同一个 6 位码在有效期内可以反复通过，
	// 截获后即可在另一个会话里重放。持久化步进后，旧的码一律拒绝。
	LastUsedStep int64          `json:"-" gorm:"default:0"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	DeletedAt    gorm.DeletedAt `json:"-" gorm:"index"`
}

// TwoFABackupCode 备用码使用记录表
type TwoFABackupCode struct {
	Id        int            `json:"id" gorm:"primaryKey"`
	UserId    int            `json:"user_id" gorm:"not null;index"`
	CodeHash  string         `json:"-" gorm:"type:varchar(255);not null"` // 备用码哈希
	IsUsed    bool           `json:"is_used"`
	UsedAt    *time.Time     `json:"used_at,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	DeletedAt gorm.DeletedAt `json:"-" gorm:"index"`
}

// GetTwoFAByUserId 根据用户ID获取2FA设置
func GetTwoFAByUserId(userId int) (*TwoFA, error) {
	if userId == 0 {
		return nil, errors.New("用户ID不能为空")
	}

	var twoFA TwoFA
	err := DB.Where("user_id = ?", userId).First(&twoFA).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil // 返回nil表示未设置2FA
		}
		return nil, err
	}

	return &twoFA, nil
}

// CreateTwoFA 创建2FA设置
func (t *TwoFA) Create() error {
	// 检查用户是否已存在2FA设置
	existing, err := GetTwoFAByUserId(t.UserId)
	if err != nil {
		return err
	}
	if existing != nil {
		return errors.New("用户已存在2FA设置")
	}

	// 验证用户存在
	var user User
	if err := DB.First(&user, t.UserId).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("用户不存在")
		}
		return err
	}

	return DB.Create(t).Error
}

// Update 更新2FA设置
func (t *TwoFA) Update() error {
	if t.Id == 0 {
		return errors.New("2FA记录ID不能为空")
	}
	// 只写本方法负责的两列。整行 Save 会把 created_at 写成零值（MySQL 严格
	// 模式下超出 DATETIME 下界直接报错，PG/SQLite 留下公元 1 年），并清空
	// failed_attempts / locked_until / last_used_step，绕过锁定与重放保护。
	return DB.Model(&TwoFA{}).Where("id = ?", t.Id).
		Updates(map[string]interface{}{
			"secret":     t.Secret,
			"is_enabled": t.IsEnabled,
		}).Error
}

// Delete 删除2FA设置
func (t *TwoFA) Delete() error {
	if t.Id == 0 {
		return errors.New("2FA记录ID不能为空")
	}

	// 使用事务确保原子性
	return DB.Transaction(func(tx *gorm.DB) error {
		// 同时删除相关的备用码记录（硬删除）
		if err := tx.Unscoped().Where("user_id = ?", t.UserId).Delete(&TwoFABackupCode{}).Error; err != nil {
			return err
		}

		// 硬删除2FA记录
		return tx.Unscoped().Delete(t).Error
	})
}

// ResetFailedAttempts 重置失败尝试次数
func (t *TwoFA) ResetFailedAttempts() error {
	// 只写这两个字段：整行 Save 会把调用方加载时的快照整体写回，
	// 包括并发的成功路径刚推进的 last_used_step。
	result := DB.Model(&TwoFA{}).Where("id = ?", t.Id).
		Updates(map[string]interface{}{"failed_attempts": 0, "locked_until": nil})
	if result.Error != nil {
		return result.Error
	}
	t.FailedAttempts = 0
	t.LockedUntil = nil
	return nil
}

// IncrementFailedAttempts 增加失败尝试次数
func (t *TwoFA) IncrementFailedAttempts() error {
	// 失败计数必须是增量写：t 是调用方早先加载的快照，用 Save 落库会把
	// last_used_step 写回旧值（重放保护失效），也会把并发刚触发的
	// locked_until 抹成 nil（锁定被解除）。两个探针都能稳定复现这两点。
	lockUntil := time.Now().Add(time.Duration(common.LockoutDuration) * time.Second)
	if err := DB.Model(&TwoFA{}).Where("id = ?", t.Id).
		Updates(map[string]interface{}{"failed_attempts": gorm.Expr("failed_attempts + 1")}).Error; err != nil {
		return err
	}
	// 锁定判定用写后的真实值，不用本地快照的算术结果：并发失败请求各自
	// 只知道自己那一次，靠 SQL 里的比较才能让第 N 次真正触发锁定。
	if err := DB.Model(&TwoFA{}).
		Where("id = ? AND failed_attempts >= ?", t.Id, common.MaxFailAttempts).
		Where("locked_until IS NULL OR locked_until < ?", time.Now()).
		Updates(map[string]interface{}{"locked_until": lockUntil}).Error; err != nil {
		return err
	}

	var stored TwoFA
	if err := DB.Select("failed_attempts", "locked_until").Where("id = ?", t.Id).First(&stored).Error; err != nil {
		return err
	}
	t.FailedAttempts = stored.FailedAttempts
	t.LockedUntil = stored.LockedUntil
	return nil
}

// IsLocked 检查账户是否被锁定
func (t *TwoFA) IsLocked() bool {
	if t.LockedUntil == nil {
		return false
	}
	return time.Now().Before(*t.LockedUntil)
}

// CreateBackupCodes 创建备用码
func CreateBackupCodes(userId int, codes []string) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		// 先删除现有的备用码
		if err := tx.Where("user_id = ?", userId).Delete(&TwoFABackupCode{}).Error; err != nil {
			return err
		}

		// 创建新的备用码记录
		for _, code := range codes {
			hashedCode, err := common.HashBackupCode(code)
			if err != nil {
				return err
			}

			backupCode := TwoFABackupCode{
				UserId:   userId,
				CodeHash: hashedCode,
				IsUsed:   false,
			}

			if err := tx.Create(&backupCode).Error; err != nil {
				return err
			}
		}

		return nil
	})
}

// ValidateBackupCode 验证并使用备用码
func ValidateBackupCode(userId int, code string) (bool, error) {
	if !common.ValidateBackupCode(code) {
		return false, errors.New("验证码或备用码不正确")
	}

	normalizedCode := common.NormalizeBackupCode(code)

	// 查找未使用的备用码
	var backupCodes []TwoFABackupCode
	if err := DB.Where("user_id = ? AND is_used = false", userId).Find(&backupCodes).Error; err != nil {
		return false, err
	}

	// 验证备用码
	for _, bc := range backupCodes {
		if common.ValidatePasswordAndHash(normalizedCode, bc.CodeHash) {
			// 标记为已使用必须带条件写：备用码是单次凭证，而上面的查询与
			// 这里的更新之间存在窗口，并发提交同一个备用码时，多个请求都能
			// 通过校验并各自登录成功。条件写让只有一个请求真正消费到它。
			// 这与 TOTP 路径用 last_used_step 条件写解决同一竞态的做法一致。
			now := time.Now()
			result := DB.Model(&TwoFABackupCode{}).
				Where("id = ? AND is_used = ?", bc.Id, false).
				Updates(map[string]interface{}{
					"is_used": true,
					"used_at": &now,
				})
			if result.Error != nil {
				return false, result.Error
			}
			if result.RowsAffected == 0 {
				// 另一个并发请求已经消费了这个备用码。
				return false, nil
			}

			return true, nil
		}
	}

	return false, nil
}

// GetUnusedBackupCodeCount 获取未使用的备用码数量
func GetUnusedBackupCodeCount(userId int) (int, error) {
	var count int64
	err := DB.Model(&TwoFABackupCode{}).Where("user_id = ? AND is_used = false", userId).Count(&count).Error
	return int(count), err
}

// DisableTwoFA 禁用用户的2FA
func DisableTwoFA(userId int) error {
	twoFA, err := GetTwoFAByUserId(userId)
	if err != nil {
		return err
	}
	if twoFA == nil {
		return ErrTwoFANotEnabled
	}

	// 删除2FA设置和备用码
	return twoFA.Delete()
}

// EnableTwoFA 启用2FA
func (t *TwoFA) Enable() error {
	// 启用时会消费调用方算出的时间步进（controller 把 LastUsedStep 设为
	// 本次启用的步进）。步进必须条件写：并发请求可能已经把步进推到更后面，
	// 整行 Save 会把它写回旧值，重放保护随之失效。
	if t.LastUsedStep > 0 {
		result := DB.Model(&TwoFA{}).
			Where("id = ? AND (last_used_step IS NULL OR last_used_step < ?)", t.Id, t.LastUsedStep).
			Updates(map[string]interface{}{"last_used_step": t.LastUsedStep})
		if result.Error != nil {
			return result.Error
		}
	}
	result := DB.Model(&TwoFA{}).Where("id = ?", t.Id).
		Updates(map[string]interface{}{
			"is_enabled":      true,
			"failed_attempts": 0,
			"locked_until":    nil,
		})
	if result.Error != nil {
		return result.Error
	}
	t.IsEnabled = true
	t.FailedAttempts = 0
	t.LockedUntil = nil
	return nil
}

// ValidateTOTPAndUpdateUsage 验证TOTP并更新使用记录
func (t *TwoFA) ValidateTOTPAndUpdateUsage(code string) (bool, error) {
	// 检查是否被锁定
	if t.IsLocked() {
		return false, fmt.Errorf("账户已被锁定，请在%v后重试", t.LockedUntil.Format("2006-01-02 15:04:05"))
	}

	// 验证TOTP码：同时消费该时间步进，使同一个码不能在别处重放。
	step, ok := common.ValidateTOTPStep(t.Secret, code, time.Now(), t.LastUsedStep, t.LastUsedStep > 0)
	if !ok {
		// 增加失败次数
		if err := t.IncrementFailedAttempts(); err != nil {
			common.SysLog("更新2FA失败次数失败: " + err.Error())
		}
		return false, nil
	}

	// 验证成功。步进必须带条件写回：校验与落库之间若有并发请求，
	// 两者都会通过上面的比较，只有一个能真正把步进推进。
	now := time.Now()
	result := DB.Model(&TwoFA{}).
		Where("id = ? AND (last_used_step IS NULL OR last_used_step < ?)", t.Id, step).
		Updates(map[string]interface{}{
			"failed_attempts": 0,
			"locked_until":    nil,
			"last_used_at":    &now,
			"last_used_step":  step,
		})
	if result.Error != nil {
		common.SysLog("更新2FA使用记录失败: " + result.Error.Error())
		return true, nil
	}
	if result.RowsAffected == 0 {
		// 另一个并发请求已经消费了这个码。
		return false, nil
	}

	t.FailedAttempts = 0
	t.LockedUntil = nil
	t.LastUsedAt = &now
	t.LastUsedStep = step
	return true, nil
}

// ValidateBackupCodeAndUpdateUsage 验证备用码并更新使用记录
func (t *TwoFA) ValidateBackupCodeAndUpdateUsage(code string) (bool, error) {
	// 检查是否被锁定
	if t.IsLocked() {
		return false, fmt.Errorf("账户已被锁定，请在%v后重试", t.LockedUntil.Format("2006-01-02 15:04:05"))
	}

	// 验证备用码
	valid, err := ValidateBackupCode(t.UserId, code)
	if err != nil {
		return false, err
	}

	if !valid {
		// 增加失败次数
		if err := t.IncrementFailedAttempts(); err != nil {
			common.SysLog("更新2FA失败次数失败: " + err.Error())
		}
		return false, nil
	}

	// 验证成功，重置失败次数并更新最后使用时间。这里同样不能整行 Save：
	// 备用码路径与 TOTP 路径会交错执行，整行写回会把 TOTP 路径刚推进的
	// last_used_step 覆盖成 t 里的旧值，重放保护随之失效。
	now := time.Now()
	result := DB.Model(&TwoFA{}).Where("id = ?", t.Id).
		Updates(map[string]interface{}{
			"failed_attempts": 0,
			"locked_until":    nil,
			"last_used_at":    &now,
		})
	if result.Error != nil {
		common.SysLog("更新2FA使用记录失败: " + result.Error.Error())
		return true, nil
	}

	t.FailedAttempts = 0
	t.LockedUntil = nil
	t.LastUsedAt = &now

	return true, nil
}

// GetTwoFAStats 获取2FA统计信息（管理员使用）
func GetTwoFAStats() (map[string]interface{}, error) {
	var totalUsers, enabledUsers int64

	// 总用户数
	if err := DB.Model(&User{}).Count(&totalUsers).Error; err != nil {
		return nil, err
	}

	// 启用2FA的用户数
	if err := DB.Model(&TwoFA{}).Where("is_enabled = true").Count(&enabledUsers).Error; err != nil {
		return nil, err
	}

	enabledRate := float64(0)
	if totalUsers > 0 {
		enabledRate = float64(enabledUsers) / float64(totalUsers) * 100
	}

	return map[string]interface{}{
		"total_users":   totalUsers,
		"enabled_users": enabledUsers,
		"enabled_rate":  fmt.Sprintf("%.1f%%", enabledRate),
	}, nil
}

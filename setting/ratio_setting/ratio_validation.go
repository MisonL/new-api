package ratio_setting

import (
	"fmt"
	"math"

	"github.com/QuantumNous/new-api/common"
)

// ValidateNonNegativeRatioJSON 解析倍率 JSON 并拒绝负值与非有限值。
//
// 负倍率不是"便宜的模型"，而是反向计费：倍率为负时 text_quota 也为负，
// service.PostConsumeQuota 对 quota <= 0 走 IncreaseUserQuota 分支，等于向
// 用户账户加钱。GroupRatio 一直有 CheckGroupRatio 兜住，其余倍率表没有任何
// 守卫，控制器、数据库回载、上游同步三条写路径都能把负值写进内存表。
//
// 0 是合法值：未定价模型依赖它走 fail-closed 分支，因此只拒绝 < 0。
// NaN/Inf 无法由 JSON 字面量直接表达，但超范围数字（如 1e400）解码后可能是
// Inf，一旦进入 quota 计算会让整段计费变成 0 或 NaN，这里一并拒绝。
func ValidateNonNegativeRatioJSON(jsonStr string) error {
	parsed := make(map[string]float64)
	if err := common.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		return err
	}
	for name, ratio := range parsed {
		if math.IsNaN(ratio) || math.IsInf(ratio, 0) {
			return fmt.Errorf("ratio must be a finite number: %s", name)
		}
		if ratio < 0 {
			return fmt.Errorf("ratio must be not less than 0: %s", name)
		}
	}
	return nil
}

// ValidateNonNegativeNestedRatioJSON 同上，用于 GroupGroupRatio 这类两层倍率表。
func ValidateNonNegativeNestedRatioJSON(jsonStr string) error {
	parsed := make(map[string]map[string]float64)
	if err := common.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		return err
	}
	for outer, inner := range parsed {
		for name, ratio := range inner {
			if math.IsNaN(ratio) || math.IsInf(ratio, 0) {
				return fmt.Errorf("ratio must be a finite number: %s.%s", outer, name)
			}
			if ratio < 0 {
				return fmt.Errorf("ratio must be not less than 0: %s.%s", outer, name)
			}
		}
	}
	return nil
}

package model

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/types"

	"github.com/samber/lo"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Channel struct {
	Id                 int     `json:"id"`
	Type               int     `json:"type" gorm:"default:0"`
	Key                string  `json:"key" gorm:"not null"`
	OpenAIOrganization *string `json:"openai_organization"`
	TestModel          *string `json:"test_model"`
	Status             int     `json:"status" gorm:"default:1"`
	Name               string  `json:"name" gorm:"index"`
	Weight             *uint   `json:"weight" gorm:"default:0"`
	CreatedTime        int64   `json:"created_time" gorm:"bigint"`
	TestTime           int64   `json:"test_time" gorm:"bigint"`
	ResponseTime       int     `json:"response_time"` // in milliseconds
	BaseURL            *string `json:"base_url" gorm:"column:base_url;default:''"`
	Other              string  `json:"other"`
	Balance            float64 `json:"balance"` // in USD
	BalanceUpdatedTime int64   `json:"balance_updated_time" gorm:"bigint"`
	Models             string  `json:"models"`
	Group              string  `json:"group" gorm:"type:varchar(64);default:'default'"`
	UsedQuota          int64   `json:"used_quota" gorm:"bigint;default:0"`
	ModelMapping       *string `json:"model_mapping" gorm:"type:text"`
	//MaxInputTokens     *int    `json:"max_input_tokens" gorm:"default:0"`
	StatusCodeMapping *string `json:"status_code_mapping" gorm:"type:varchar(1024);default:''"`
	Priority          *int64  `json:"priority" gorm:"bigint;default:0"`
	PinOrder          *int64  `json:"pin_order" gorm:"bigint;default:0;index"`
	MaxConcurrency    int     `json:"max_concurrency" gorm:"not null;default:0;index"`
	AutoBan           *int    `json:"auto_ban" gorm:"default:1"`
	OtherInfo         string  `json:"other_info"`
	Tag               *string `json:"tag" gorm:"index"`
	Setting           *string `json:"setting" gorm:"type:text"` // 渠道额外设置
	ParamOverride     *string `json:"param_override" gorm:"type:text"`
	HeaderOverride    *string `json:"header_override" gorm:"type:text"`
	Remark            *string `json:"remark" gorm:"type:varchar(255)" validate:"max=255"`
	// add after v0.8.5
	ChannelInfo ChannelInfo `json:"channel_info" gorm:"type:json"`

	OtherSettings string `json:"settings" gorm:"column:settings"` // 其他设置，存储azure版本等不需要检索的信息，详见dto.ChannelOtherSettings

	// cache info
	Keys []string `json:"-" gorm:"-"`
}

type ChannelInfo struct {
	IsMultiKey             bool                  `json:"is_multi_key"`                        // 是否多Key模式
	MultiKeySize           int                   `json:"multi_key_size"`                      // 多Key模式下的Key数量
	MultiKeyStatusList     map[int]int           `json:"multi_key_status_list"`               // key状态列表，key index -> status
	MultiKeyDisabledReason map[int]string        `json:"multi_key_disabled_reason,omitempty"` // key禁用原因列表，key index -> reason
	MultiKeyDisabledTime   map[int]int64         `json:"multi_key_disabled_time,omitempty"`   // key禁用时间列表，key index -> time
	MultiKeyPollingIndex   int                   `json:"multi_key_polling_index"`             // 多Key模式下轮询的key索引
	MultiKeyMode           constant.MultiKeyMode `json:"multi_key_mode"`
}

func (channel *Channel) CloneForCache() *Channel {
	if channel == nil {
		return nil
	}
	cloned := *channel
	cloned.OpenAIOrganization = cloneStringPointer(channel.OpenAIOrganization)
	cloned.TestModel = cloneStringPointer(channel.TestModel)
	cloned.Weight = cloneUintPointer(channel.Weight)
	cloned.BaseURL = cloneStringPointer(channel.BaseURL)
	cloned.ModelMapping = cloneStringPointer(channel.ModelMapping)
	cloned.StatusCodeMapping = cloneStringPointer(channel.StatusCodeMapping)
	cloned.Priority = cloneInt64Pointer(channel.Priority)
	cloned.PinOrder = cloneInt64Pointer(channel.PinOrder)
	cloned.AutoBan = cloneIntPointer(channel.AutoBan)
	cloned.Tag = cloneStringPointer(channel.Tag)
	cloned.Setting = cloneStringPointer(channel.Setting)
	cloned.ParamOverride = cloneStringPointer(channel.ParamOverride)
	cloned.HeaderOverride = cloneStringPointer(channel.HeaderOverride)
	cloned.Remark = cloneStringPointer(channel.Remark)
	cloned.Keys = append([]string(nil), channel.Keys...)
	cloned.ChannelInfo.MultiKeyStatusList = cloneIntMap(channel.ChannelInfo.MultiKeyStatusList)
	cloned.ChannelInfo.MultiKeyDisabledReason = cloneIntStringMap(channel.ChannelInfo.MultiKeyDisabledReason)
	cloned.ChannelInfo.MultiKeyDisabledTime = cloneInt64Map(channel.ChannelInfo.MultiKeyDisabledTime)
	return &cloned
}

func cloneStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneUintPointer(value *uint) *uint {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneIntPointer(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneInt64Pointer(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneIntMap(source map[int]int) map[int]int {
	if source == nil {
		return nil
	}
	cloned := make(map[int]int, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

func cloneIntStringMap(source map[int]string) map[int]string {
	if source == nil {
		return nil
	}
	cloned := make(map[int]string, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

func cloneInt64Map(source map[int]int64) map[int]int64 {
	if source == nil {
		return nil
	}
	cloned := make(map[int]int64, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

type ChannelSortOptions struct {
	SortBy    string
	SortOrder string
	IDSort    bool
}

type ChannelPriority struct {
	Id       int    `json:"id"`
	Priority *int64 `json:"priority"`
}

// ChannelPinOrder is the management-only ordering metadata used by the
// channel list. It is deliberately separate from route priority so pinning a
// channel cannot alter request selection semantics.
type ChannelPinOrder struct {
	Id       int    `json:"id"`
	PinOrder *int64 `json:"pin_order"`
}

var channelSortColumns = map[string]string{
	"id":            "id",
	"name":          "name",
	"priority":      "priority",
	"pin_order":     "pin_order",
	"balance":       "balance",
	"response_time": "response_time",
	"test_time":     "test_time",
}

func NewChannelSortOptions(sortBy string, sortOrder string, idSort bool) ChannelSortOptions {
	normalizedSortBy := strings.ToLower(strings.TrimSpace(sortBy))
	normalizedSortOrder := strings.ToLower(strings.TrimSpace(sortOrder))
	if _, ok := channelSortColumns[normalizedSortBy]; !ok {
		normalizedSortBy = ""
		normalizedSortOrder = ""
	} else if normalizedSortOrder != "asc" {
		normalizedSortOrder = "desc"
	}

	return ChannelSortOptions{
		SortBy:    normalizedSortBy,
		SortOrder: normalizedSortOrder,
		IDSort:    idSort,
	}
}

func channelPrioritySortClause(sortOrder string) string {
	sortDirection := "DESC"
	if strings.EqualFold(strings.TrimSpace(sortOrder), "asc") {
		sortDirection = "ASC"
	}
	return "COALESCE(priority, 0) " + sortDirection + ", id DESC"
}

func channelPinOrderSortClause(sortOrder string) string {
	sortDirection := "DESC"
	if strings.EqualFold(strings.TrimSpace(sortOrder), "asc") {
		sortDirection = "ASC"
	}
	return "COALESCE(pin_order, 0) " + sortDirection + ", id DESC"
}

func channelDefaultSortClause() string {
	return "COALESCE(pin_order, 0) DESC, COALESCE(priority, 0) DESC, id DESC"
}

func (options ChannelSortOptions) Apply(query *gorm.DB) *gorm.DB {
	if columnName, ok := channelSortColumns[options.SortBy]; ok {
		if columnName == "priority" {
			return query.Order(channelPrioritySortClause(options.SortOrder))
		}
		if columnName == "pin_order" {
			return query.Order(channelPinOrderSortClause(options.SortOrder))
		}
		query = query.Order(clause.OrderByColumn{
			Column: clause.Column{Name: columnName},
			Desc:   options.SortOrder != "asc",
		})
		return query
	}
	if options.IDSort {
		return query.Order(clause.OrderByColumn{
			Column: clause.Column{Name: "id"},
			Desc:   true,
		})
	}
	return query.Order(channelDefaultSortClause())
}

func resolveChannelSortOptions(idSort bool, sortOptions []ChannelSortOptions) ChannelSortOptions {
	if len(sortOptions) == 0 {
		return NewChannelSortOptions("", "", idSort)
	}
	options := sortOptions[0]
	options.IDSort = options.IDSort || idSort
	return options
}

func NormalizeChannelGroupFilter(group string) string {
	group = strings.TrimSpace(group)
	if group == "" || strings.EqualFold(group, "all") || strings.EqualFold(group, "null") {
		return ""
	}
	return group
}

func channelGroupFilterCondition() string {
	if commonGroupCol == "" {
		initCol()
	}
	if common.UsingMySQL {
		return `CONCAT(',', ` + commonGroupCol + `, ',') LIKE ? ESCAPE '!'`
	}
	return `(',' || ` + commonGroupCol + ` || ',') LIKE ? ESCAPE '!'`
}

func channelGroupFilterPattern(group string) string {
	return "%," + escapeChannelLikeLiteral(group) + ",%"
}

func ApplyChannelGroupFilter(query *gorm.DB, group string) *gorm.DB {
	group = NormalizeChannelGroupFilter(group)
	if group == "" {
		return query
	}
	return query.Where(channelGroupFilterCondition(), channelGroupFilterPattern(group))
}

// Value implements driver.Valuer interface
func (c ChannelInfo) Value() (driver.Value, error) {
	bytes, err := common.Marshal(&c)
	if err != nil {
		return nil, err
	}
	return string(bytes), nil
}

// Scan implements sql.Scanner interface
func (c *ChannelInfo) Scan(value interface{}) error {
	switch typedValue := value.(type) {
	case nil:
		*c = ChannelInfo{}
		return nil
	case []byte:
		if len(typedValue) == 0 {
			*c = ChannelInfo{}
			return nil
		}
		return common.Unmarshal(typedValue, c)
	case string:
		if typedValue == "" {
			*c = ChannelInfo{}
			return nil
		}
		return common.Unmarshal([]byte(typedValue), c)
	default:
		return fmt.Errorf("unsupported channel info type: %T", value)
	}
}

func (channel *Channel) GetKeys() []string {
	if channel.Key == "" {
		return []string{}
	}
	if len(channel.Keys) > 0 {
		return channel.Keys
	}
	trimmed := strings.TrimSpace(channel.Key)
	// If the key starts with '[', try to parse it as a JSON array (e.g., for Vertex AI scenarios)
	if strings.HasPrefix(trimmed, "[") {
		var arr []common.RawMessage
		if err := common.Unmarshal([]byte(trimmed), &arr); err == nil {
			res := make([]string, len(arr))
			for i, v := range arr {
				// JSON string elements must be decoded before use. Returning the
				// raw bytes would include quotes (for example, `"key"`) and make
				// credential matching fail for multi-key channels. Objects and
				// numbers intentionally remain their canonical raw JSON values.
				res[i] = common.JsonRawMessageToString(v)
			}
			return res
		}
	}
	// Otherwise, fall back to splitting by newline
	keys := strings.Split(strings.Trim(channel.Key, "\n"), "\n")
	return keys
}

func (channel *Channel) GetNextEnabledKey() (string, int, *types.NewAPIError) {
	return channel.GetNextEnabledKeyExcluding(nil)
}

// GetNextEnabledKeyExcluding selects an enabled key while skipping the
// per-request credential indexes supplied by the caller. The exclusions are
// intentionally ephemeral and never modify the persisted multi-key status.
func (channel *Channel) GetNextEnabledKeyExcluding(excluded map[int]struct{}) (string, int, *types.NewAPIError) {
	// If not in multi-key mode, return the original key string directly.
	if !channel.ChannelInfo.IsMultiKey {
		return channel.Key, 0, nil
	}

	// Obtain all keys (split by \n)
	keys := channel.GetKeys()
	if len(keys) == 0 {
		// No keys available, return error, should disable the channel
		return "", 0, types.NewError(errors.New("no keys available"), types.ErrorCodeChannelNoAvailableKey)
	}

	lock := GetChannelPollingLock(channel.Id)
	lock.Lock()
	defer lock.Unlock()

	statusList := channel.ChannelInfo.MultiKeyStatusList
	// helper to get key status, default to enabled when missing
	getStatus := func(idx int) int {
		if statusList == nil {
			return common.ChannelStatusEnabled
		}
		if status, ok := statusList[idx]; ok {
			return status
		}
		return common.ChannelStatusEnabled
	}

	// Collect indexes of enabled keys
	enabledIdx := make([]int, 0, len(keys))
	for i := range keys {
		if getStatus(i) == common.ChannelStatusEnabled {
			if _, skip := excluded[i]; skip {
				continue
			}
			enabledIdx = append(enabledIdx, i)
		}
	}
	// If no specific status list or none enabled, return an explicit error so caller can
	// properly handle a channel with no available keys (e.g. mark channel disabled).
	// Returning the first key here caused requests to keep using an already-disabled key.
	if len(enabledIdx) == 0 {
		return "", 0, types.NewError(errors.New("no enabled keys"), types.ErrorCodeChannelNoAvailableKey)
	}

	switch channel.ChannelInfo.MultiKeyMode {
	case constant.MultiKeyModeRandom:
		// Randomly pick one enabled key
		selectedIdx := enabledIdx[rand.Intn(len(enabledIdx))]
		return keys[selectedIdx], selectedIdx, nil
	case constant.MultiKeyModePolling:
		// Use channel-specific lock to ensure thread-safe polling

		channelInfo, err := CacheGetChannelInfo(channel.Id)
		if err != nil {
			return "", 0, types.NewError(err, types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
		}
		//println("before polling index:", channel.ChannelInfo.MultiKeyPollingIndex)
		defer func() {
			if common.DebugEnabled {
				println(fmt.Sprintf("channel %d polling index: %d", channel.Id, channel.ChannelInfo.MultiKeyPollingIndex))
			}
			if !common.MemoryCacheEnabled {
				_ = channel.SaveChannelInfo()
			} else {
				CacheUpdateChannelPollingIndex(channel.Id, channel.ChannelInfo.MultiKeyPollingIndex)
			}
		}()
		// Start from the saved polling index and look for the next enabled key
		start := channelInfo.MultiKeyPollingIndex
		if start < 0 || start >= len(keys) {
			start = 0
		}
		for i := 0; i < len(keys); i++ {
			idx := (start + i) % len(keys)
			if getStatus(idx) == common.ChannelStatusEnabled {
				if _, skip := excluded[idx]; skip {
					continue
				}
				// update polling index for next call (point to the next position)
				channel.ChannelInfo.MultiKeyPollingIndex = (idx + 1) % len(keys)
				return keys[idx], idx, nil
			}
		}
		// Fallback - should not happen, but return first enabled key
		return keys[enabledIdx[0]], enabledIdx[0], nil
	default:
		// Unknown mode, default to first enabled key (or original key string)
		return keys[enabledIdx[0]], enabledIdx[0], nil
	}
}

func (channel *Channel) SaveChannelInfo() error {
	return DB.Model(channel).Update("channel_info", channel.ChannelInfo).Error
}

func (channel *Channel) GetModels() []string {
	if channel.Models == "" {
		return []string{}
	}
	return strings.Split(strings.Trim(channel.Models, ","), ",")
}

func (channel *Channel) GetGroups() []string {
	if channel.Group == "" {
		return []string{}
	}
	groups := strings.Split(strings.Trim(channel.Group, ","), ",")
	for i, group := range groups {
		groups[i] = strings.TrimSpace(group)
	}
	return groups
}

func (channel *Channel) GetOtherInfo() map[string]interface{} {
	otherInfo := make(map[string]interface{})
	if channel.OtherInfo != "" {
		err := common.Unmarshal([]byte(channel.OtherInfo), &otherInfo)
		if err != nil {
			common.SysLog(fmt.Sprintf("failed to unmarshal other info: channel_id=%d, tag=%s, name=%s, error=%v", channel.Id, channel.GetTag(), channel.Name, err))
		}
	}
	return otherInfo
}

func (channel *Channel) SetOtherInfo(otherInfo map[string]interface{}) {
	otherInfoBytes, err := common.Marshal(otherInfo)
	if err != nil {
		common.SysLog(fmt.Sprintf("failed to marshal other info: channel_id=%d, tag=%s, name=%s, error=%v", channel.Id, channel.GetTag(), channel.Name, err))
		return
	}
	channel.OtherInfo = string(otherInfoBytes)
}

func (channel *Channel) GetTag() string {
	if channel.Tag == nil {
		return ""
	}
	return *channel.Tag
}

func (channel *Channel) SetTag(tag string) {
	channel.Tag = &tag
}

func (channel *Channel) GetAutoBan() bool {
	if channel.AutoBan == nil {
		return false
	}
	return *channel.AutoBan == 1
}

func (channel *Channel) SaveWithoutKey() error {
	if channel.Id == 0 {
		return errors.New("channel ID is 0")
	}
	if err := DB.Omit("key").Save(channel).Error; err != nil {
		return err
	}
	CacheReloadChannel(channel.Id)
	return nil
}

func GetAllChannels(startIdx int, num int, selectAll bool, idSort bool, sortOptions ...ChannelSortOptions) ([]*Channel, error) {
	var channels []*Channel
	var err error
	order := resolveChannelSortOptions(idSort, sortOptions)
	if selectAll {
		err = order.Apply(DB).Find(&channels).Error
	} else {
		err = order.Apply(DB).Limit(num).Offset(startIdx).Omit("key").Find(&channels).Error
	}
	return channels, err
}

func GetChannelsByTag(tag string, idSort bool, selectAll bool, sortOptions ...ChannelSortOptions) ([]*Channel, error) {
	var channels []*Channel
	order := resolveChannelSortOptions(idSort, sortOptions)
	query := order.Apply(DB.Where("tag = ?", tag))
	if !selectAll {
		query = query.Omit("key")
	}
	err := query.Find(&channels).Error
	return channels, err
}

func GetTopChannelPriorities() ([]ChannelPriority, error) {
	var channels []ChannelPriority
	err := DB.Model(&Channel{}).
		Select("id, priority").
		Where("COALESCE(priority, 0) > ?", 0).
		Order(channelPrioritySortClause("desc")).
		Find(&channels).Error
	return channels, err
}

func GetTopChannelPinOrders() ([]ChannelPinOrder, error) {
	var channels []ChannelPinOrder
	err := DB.Model(&Channel{}).
		Select("id, pin_order").
		Where("COALESCE(pin_order, 0) > ?", 0).
		Order(channelPinOrderSortClause("desc")).
		Find(&channels).Error
	return channels, err
}

// UpdateChannelPinOrders updates management-only pin ordering atomically. It
// intentionally does not touch abilities or route priority fields.
func UpdateChannelPinOrders(updates []ChannelPinOrder) error {
	if len(updates) == 0 {
		return errors.New("pin order updates cannot be empty")
	}
	seenIDs := make(map[int]struct{}, len(updates))
	for _, update := range updates {
		if update.Id <= 0 || update.PinOrder == nil || *update.PinOrder < 0 {
			return fmt.Errorf("invalid pin order update for channel %d", update.Id)
		}
		if _, exists := seenIDs[update.Id]; exists {
			return fmt.Errorf("duplicate pin order update for channel %d", update.Id)
		}
		seenIDs[update.Id] = struct{}{}
	}

	if err := DB.Transaction(func(tx *gorm.DB) error {
		for _, update := range updates {
			var channel Channel
			if err := tx.Select("id").Where("id = ?", update.Id).First(&channel).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return fmt.Errorf("channel does not exist: %d", update.Id)
				}
				return err
			}
			if err := tx.Model(&Channel{}).
				Where("id = ?", update.Id).
				Select("pin_order").
				Update("pin_order", *update.PinOrder).Error; err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	for _, update := range updates {
		CacheReloadChannel(update.Id)
	}
	return nil
}

func ApplyChannelSearchFilters(query *gorm.DB, keyword string, group string, model string) *gorm.DB {
	if commonGroupCol == "" || commonKeyCol == "" {
		initCol()
	}
	modelsCol := "`models`"
	if common.UsingPostgreSQL {
		modelsCol = `"models"`
	}
	baseURLCol := "`base_url`"
	if common.UsingPostgreSQL {
		baseURLCol = `"base_url"`
	}

	keyword = strings.TrimSpace(keyword)
	model = strings.TrimSpace(model)
	if keyword != "" {
		keywordLikePattern := "%" + escapeChannelLikeLiteral(keyword) + "%"
		query = query.Where(
			"(id = ? OR name LIKE ? ESCAPE '!' OR "+commonKeyCol+" = ? OR "+baseURLCol+" LIKE ? ESCAPE '!')",
			common.String2Int(keyword),
			keywordLikePattern,
			keyword,
			keywordLikePattern,
		)
	}
	if model != "" {
		modelLikePattern := "%" + escapeChannelLikeLiteral(model) + "%"
		query = query.Where(modelsCol+" LIKE ? ESCAPE '!'", modelLikePattern)
	}
	return ApplyChannelGroupFilter(query, group)
}

func escapeChannelLikeLiteral(input string) string {
	return strings.NewReplacer(
		"!", "!!",
		"%", "!%",
		"_", "!_",
	).Replace(input)
}

func SearchChannels(keyword string, group string, model string, idSort bool, sortOptions ...ChannelSortOptions) ([]*Channel, error) {
	var channels []*Channel
	order := resolveChannelSortOptions(idSort, sortOptions)
	baseQuery := ApplyChannelSearchFilters(DB.Model(&Channel{}).Omit("key"), keyword, group, model)

	// 执行查询
	err := order.Apply(baseQuery).Find(&channels).Error
	if err != nil {
		return nil, err
	}
	return channels, nil
}

func GetChannelById(id int, selectAll bool) (*Channel, error) {
	channel := &Channel{Id: id}
	var err error = nil
	if selectAll {
		err = DB.First(channel, "id = ?", id).Error
	} else {
		err = DB.Omit("key").First(channel, "id = ?", id).Error
	}
	if err != nil {
		return nil, err
	}
	if channel == nil {
		return nil, errors.New("channel not found")
	}
	return channel, nil
}

func BatchInsertChannels(channels []Channel) (err error) {
	if len(channels) == 0 {
		return nil
	}
	tx := DB.Begin()
	if tx.Error != nil {
		return tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			err = fmt.Errorf("batch insert channels panic: %v", r)
		}
	}()

	for _, chunk := range lo.Chunk(channels, 50) {
		if err := syncPostgresChannelIDSequence(tx); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Create(&chunk).Error; err != nil {
			tx.Rollback()
			return err
		}
		if err := syncPostgresChannelIDSequence(tx); err != nil {
			tx.Rollback()
			return err
		}
		for _, channel_ := range chunk {
			if err := channel_.AddAbilities(tx); err != nil {
				tx.Rollback()
				return err
			}
		}
	}
	if err := tx.Commit().Error; err != nil {
		return err
	}
	refreshChannelCacheAfterWrite()
	return nil
}

const syncChannelIDSequenceSQL = `
WITH channel_max AS (
	SELECT COALESCE(MAX(id), 0) AS max_id FROM channels
),
channel_sequence AS (
	SELECT pg_get_serial_sequence('channels', 'id')::regclass AS sequence_regclass
),
sequence_state AS (
	SELECT s.last_value
	FROM channel_sequence cs
	JOIN pg_class c ON c.oid = cs.sequence_regclass
	JOIN pg_namespace n ON n.oid = c.relnamespace
	JOIN pg_sequences s
		ON s.schemaname = n.nspname
		AND s.sequencename = c.relname
)
SELECT setval(cs.sequence_regclass, cm.max_id, true)
FROM channel_max cm
CROSS JOIN channel_sequence cs
LEFT JOIN sequence_state ss ON true
WHERE cs.sequence_regclass IS NOT NULL
	AND cm.max_id > 0
	AND COALESCE(ss.last_value, 0) < cm.max_id
`

func syncPostgresChannelIDSequence(tx *gorm.DB) error {
	if !common.UsingPostgreSQL {
		return nil
	}
	if err := tx.Exec("LOCK TABLE channels IN SHARE ROW EXCLUSIVE MODE").Error; err != nil {
		return err
	}
	return tx.Exec(syncChannelIDSequenceSQL).Error
}

func InsertChannel(channel *Channel) (err error) {
	if channel == nil {
		return errors.New("channel is nil")
	}
	tx := DB.Begin()
	if tx.Error != nil {
		return tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			err = fmt.Errorf("insert channel panic: %v", r)
		}
	}()

	if err := syncPostgresChannelIDSequence(tx); err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Create(channel).Error; err != nil {
		tx.Rollback()
		return err
	}
	if err := syncPostgresChannelIDSequence(tx); err != nil {
		tx.Rollback()
		return err
	}
	if err := channel.AddAbilities(tx); err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Commit().Error; err != nil {
		return err
	}
	refreshChannelCacheAfterWrite()
	return nil
}

func BatchDeleteChannels(ids []int) error {
	if len(ids) == 0 {
		return nil
	}
	// 使用事务 分批删除channel表和abilities表
	tx := DB.Begin()
	if tx.Error != nil {
		return tx.Error
	}
	for _, chunk := range lo.Chunk(ids, 200) {
		if err := tx.Where("id in (?)", chunk).Delete(&Channel{}).Error; err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Where("channel_id in (?)", chunk).Delete(&Ability{}).Error; err != nil {
			tx.Rollback()
			return err
		}
	}
	if err := tx.Commit().Error; err != nil {
		return err
	}
	refreshChannelCacheAfterWrite()
	return nil
}

func (channel *Channel) GetPriority() int64 {
	if channel.Priority == nil {
		return 0
	}
	return *channel.Priority
}

func (channel *Channel) GetPinOrder() int64 {
	if channel.PinOrder == nil {
		return 0
	}
	return *channel.PinOrder
}

func (channel *Channel) GetWeight() int {
	if channel.Weight == nil {
		return 0
	}
	return int(*channel.Weight)
}

func (channel *Channel) GetBaseURL() string {
	if channel.BaseURL == nil {
		return ""
	}
	url := *channel.BaseURL
	if url == "" {
		url, _ = constant.ChannelBaseURL(channel.Type)
	}
	return url
}

func (channel *Channel) GetModelMapping() string {
	if channel.ModelMapping == nil {
		return ""
	}
	return *channel.ModelMapping
}

func (channel *Channel) GetStatusCodeMapping() string {
	if channel.StatusCodeMapping == nil {
		return ""
	}
	return *channel.StatusCodeMapping
}

func (channel *Channel) Insert() error {
	return InsertChannel(channel)
}

func (channel *Channel) Update() error {
	if channel == nil {
		return errors.New("channel is nil")
	}
	// If this is a multi-key channel, recalculate MultiKeySize based on the current key list to avoid inconsistency after editing keys
	if channel.ChannelInfo.IsMultiKey {
		var keyStr string
		if channel.Key != "" {
			keyStr = channel.Key
		} else {
			// If key is not provided, read the existing key from the database
			if existing, err := GetChannelById(channel.Id, true); err == nil {
				keyStr = existing.Key
			}
		}
		// Parse the key list (supports newline separation or JSON array)
		keys := []string{}
		if keyStr != "" {
			trimmed := strings.TrimSpace(keyStr)
			if strings.HasPrefix(trimmed, "[") {
				var arr []common.RawMessage
				if err := common.Unmarshal([]byte(trimmed), &arr); err == nil {
					keys = make([]string, len(arr))
					for i, v := range arr {
						keys[i] = string(v)
					}
				}
			}
			if len(keys) == 0 { // fallback to newline split
				keys = strings.Split(strings.Trim(keyStr, "\n"), "\n")
			}
		}
		channel.ChannelInfo.MultiKeySize = len(keys)
		// Clean up status data that exceeds the new key count to prevent index out of range
		if channel.ChannelInfo.MultiKeyStatusList != nil {
			for idx := range channel.ChannelInfo.MultiKeyStatusList {
				if idx >= channel.ChannelInfo.MultiKeySize {
					delete(channel.ChannelInfo.MultiKeyStatusList, idx)
				}
			}
		}
	}
	err := DB.Transaction(func(tx *gorm.DB) error {
		// used_quota 与 balance 由运行时累加（计费、余额查询），不是编辑表单的
		// 可写字段：前端回传的是打开弹窗时的快照，刷新计数与提交之间可能又有
		// 流量经过，旧值会把累计值改回去；调用方也可以伪造一个更大的值。
		// GORM 的 Updates(struct) 默认跳过零值，所以这里必须显式 Omit 掉它们，
		// 否则 payload 里非零的旧值仍会写回。
		if err := tx.Model(channel).
			Omit("used_quota", "balance").
			Updates(channel).Error; err != nil {
			return err
		}
		if err := tx.First(channel, "id = ?", channel.Id).Error; err != nil {
			return err
		}
		return channel.UpdateAbilities(tx)
	})
	if err != nil {
		return err
	}
	if channel.Id > 0 {
		CacheReloadChannel(channel.Id)
	} else {
		refreshChannelCacheAfterWrite()
	}
	return nil
}

func (channel *Channel) UpdateResponseTime(responseTime int64) {
	err := DB.Model(channel).Select("response_time", "test_time").Updates(Channel{
		TestTime:     common.GetTimestamp(),
		ResponseTime: int(responseTime),
	}).Error
	if err != nil {
		common.SysLog(fmt.Sprintf("failed to update response time: channel_id=%d, error=%v", channel.Id, err))
	}
}

func (channel *Channel) UpdateBalance(balance float64) {
	now := common.GetTimestamp()
	err := DB.Model(channel).Select("balance_updated_time", "balance").Updates(Channel{
		BalanceUpdatedTime: now,
		Balance:            balance,
	}).Error
	if err != nil {
		common.SysLog(fmt.Sprintf("failed to update balance: channel_id=%d, error=%v", channel.Id, err))
		return
	}
	channel.BalanceUpdatedTime = now
	channel.Balance = balance
	CacheReloadChannel(channel.Id)
}

func (channel *Channel) Delete() error {
	// The channel row and its abilities must be removed atomically: deleting
	// the channel first would leave orphan abilities on a later failure, and
	// deleting abilities first would briefly advertise a channel with no
	// routable model.
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(channel).Error; err != nil {
			return err
		}
		return tx.Where("channel_id = ?", channel.Id).Delete(&Ability{}).Error
	})
	if err != nil {
		return err
	}
	refreshChannelCacheAfterWrite()
	return nil
}

func refreshChannelCacheAfterWrite() {
	if common.MemoryCacheEnabled {
		InitChannelCache()
	}
}

var channelStatusLock sync.Mutex

// channelPollingLocks stores locks for each channel.id to ensure thread-safe polling
var channelPollingLocks sync.Map

// GetChannelPollingLock returns or creates a mutex for the given channel ID
func GetChannelPollingLock(channelId int) *sync.Mutex {
	if lock, exists := channelPollingLocks.Load(channelId); exists {
		return lock.(*sync.Mutex)
	}
	// Create new lock for this channel
	newLock := &sync.Mutex{}
	actual, _ := channelPollingLocks.LoadOrStore(channelId, newLock)
	return actual.(*sync.Mutex)
}

// CleanupChannelPollingLocks removes locks for channels that no longer exist
// This is optional and can be called periodically to prevent memory leaks
func CleanupChannelPollingLocks() {
	var activeChannelIds []int
	DB.Model(&Channel{}).Pluck("id", &activeChannelIds)

	activeChannelSet := make(map[int]bool)
	for _, id := range activeChannelIds {
		activeChannelSet[id] = true
	}

	channelPollingLocks.Range(func(key, value interface{}) bool {
		channelId := key.(int)
		if !activeChannelSet[channelId] {
			channelPollingLocks.Delete(channelId)
		}
		return true
	})
}

func handlerMultiKeyUpdate(channel *Channel, usingKey string, status int, reason string) bool {
	keys := channel.GetKeys()
	if len(keys) == 0 {
		channel.Status = status
		return true
	}
	keyIndex := -1
	for i, key := range keys {
		if key == usingKey {
			keyIndex = i
			break
		}
	}
	if keyIndex < 0 {
		if usingKey != "" {
			common.SysLog(fmt.Sprintf("failed to update multi-key status: channel_id=%d, using key not found", channel.Id))
			return false
		}
		channel.Status = status
		info := channel.GetOtherInfo()
		info["status_reason"] = reason
		info["status_time"] = common.GetTimestamp()
		channel.SetOtherInfo(info)
		return true
	}
	if channel.ChannelInfo.MultiKeyStatusList == nil {
		channel.ChannelInfo.MultiKeyStatusList = make(map[int]int)
	}
	if status == common.ChannelStatusEnabled {
		delete(channel.ChannelInfo.MultiKeyStatusList, keyIndex)
		delete(channel.ChannelInfo.MultiKeyDisabledReason, keyIndex)
		delete(channel.ChannelInfo.MultiKeyDisabledTime, keyIndex)
	} else {
		channel.ChannelInfo.MultiKeyStatusList[keyIndex] = status
		if channel.ChannelInfo.MultiKeyDisabledReason == nil {
			channel.ChannelInfo.MultiKeyDisabledReason = make(map[int]string)
		}
		if channel.ChannelInfo.MultiKeyDisabledTime == nil {
			channel.ChannelInfo.MultiKeyDisabledTime = make(map[int]int64)
		}
		channel.ChannelInfo.MultiKeyDisabledReason[keyIndex] = reason
		channel.ChannelInfo.MultiKeyDisabledTime[keyIndex] = common.GetTimestamp()
	}
	if !hasEnabledMultiKey(keys, channel.ChannelInfo.MultiKeyStatusList) {
		channel.Status = common.ChannelStatusAutoDisabled
		info := channel.GetOtherInfo()
		info["status_reason"] = "All keys are disabled"
		info["status_time"] = common.GetTimestamp()
		channel.SetOtherInfo(info)
	} else if status == common.ChannelStatusEnabled {
		channel.Status = common.ChannelStatusEnabled
		info := channel.GetOtherInfo()
		delete(info, "status_reason")
		delete(info, "status_time")
		channel.SetOtherInfo(info)
	}
	return true
}

func hasEnabledMultiKey(keys []string, statusList map[int]int) bool {
	for i := range keys {
		if statusList == nil {
			return true
		}
		status, ok := statusList[i]
		if !ok || status == common.ChannelStatusEnabled {
			return true
		}
	}
	return false
}

// UpdateChannelStatus updates channel state and its ability visibility atomically.
func UpdateChannelStatus(channelId int, usingKey string, status int, reason string) bool {
	if common.MemoryCacheEnabled {
		channelStatusLock.Lock()
		defer channelStatusLock.Unlock()
	}

	// 读-改-写必须在同一把 per-channel 锁内：加锁前读出的快照会让并发更新
	// 互相覆盖（多密钥渠道不同 key 并发禁用时，后写者抹掉先写者的禁用状态）。
	pollingLock := GetChannelPollingLock(channelId)
	pollingLock.Lock()
	defer pollingLock.Unlock()

	channel, err := GetChannelById(channelId, true)
	if err != nil {
		return false
	}
	shouldUpdateAbilities := false
	if channel.ChannelInfo.IsMultiKey {
		beforeStatus := channel.Status
		changed := handlerMultiKeyUpdate(channel, usingKey, status, reason)
		if !changed {
			return false
		}
		if beforeStatus != channel.Status {
			shouldUpdateAbilities = true
		}
	} else {
		if channel.Status == status {
			return false
		}
		info := channel.GetOtherInfo()
		info["status_reason"] = reason
		info["status_time"] = common.GetTimestamp()
		channel.SetOtherInfo(info)
		channel.Status = status
		shouldUpdateAbilities = true
	}
	err = DB.Transaction(func(tx *gorm.DB) error {
		// 只写本函数负责的列。整行 Save 会把读取后、保存前由管理端或其他
		// 写路径修改的 models/group/settings 等列回滚成旧快照。
		updates := map[string]interface{}{
			"status":       channel.Status,
			"channel_info": channel.ChannelInfo,
			"other_info":   channel.OtherInfo,
		}
		if err := tx.Model(&Channel{}).Where("id = ?", channelId).Updates(updates).Error; err != nil {
			return err
		}
		if shouldUpdateAbilities {
			err := tx.Model(&Ability{}).
				Where("channel_id = ?", channelId).
				Select("enabled").
				Update("enabled", status == common.ChannelStatusEnabled).Error
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		if shouldUpdateAbilities {
			common.SysLog(fmt.Sprintf("failed to update channel or ability status atomically: channel_id=%d, status=%d, error=%v", channelId, status, err))
		} else {
			common.SysLog(fmt.Sprintf("failed to update channel status: channel_id=%d, status=%d, error=%v", channel.Id, status, err))
		}
		return false
	}
	if common.MemoryCacheEnabled {
		InitChannelCache()
	}
	return true
}

// EnableChannelByTag enables every channel carrying the tag and its abilities
// in one transaction. It delegates to the audited implementation so every tag
// status change, including this legacy entry point, leaves a per-channel audit
// entry.
func EnableChannelByTag(tag string) error {
	return EnableChannelByTagWithActor(tag, ChannelStatusActor{})
}

// DisableChannelByTag disables every channel carrying the tag and its
// abilities in one transaction. Splitting the two writes used to commit a
// half-state — channels disabled while abilities stayed enabled — when the
// ability write failed, and the status write is what takes the channel out of
// rotation, so the rollback boundary must cover both. It delegates to the
// audited implementation so this legacy entry point cannot bypass the audit
// trail.
func DisableChannelByTag(tag string) error {
	return DisableChannelByTagWithActor(tag, ChannelStatusActor{})
}

func EditChannelByTag(tag string, newTag *string, modelMapping *string, models *string, group *string, priority *int64, weight *uint, paramOverride *string, headerOverride *string) error {
	if IsChannelRoutingV2StrictWrites() && (priority != nil || weight != nil) {
		return fmt.Errorf("%w: 请使用 /api/channel-route-scopes", ErrLegacyRouteFieldWriteBlocked)
	}
	updateData := Channel{}
	shouldReCreateAbilities := false
	updatedTag := tag
	// 如果 newTag 不为空且不等于 tag，则更新 tag
	if newTag != nil && *newTag != tag {
		updateData.Tag = newTag
		updatedTag = *newTag
	}
	// A non-nil pointer is an explicit caller decision, including an empty
	// string. Empty model_mapping is the supported way to remove a previous
	// mapping through the tag batch endpoint; checking the pointed-to value
	// would silently leave stale routing rules in place.
	if modelMapping != nil {
		updateData.ModelMapping = modelMapping
	}
	if models != nil && *models != "" {
		shouldReCreateAbilities = true
		updateData.Models = *models
	}
	if group != nil && *group != "" {
		shouldReCreateAbilities = true
		updateData.Group = *group
	}
	if priority != nil {
		updateData.Priority = priority
	}
	if weight != nil {
		updateData.Weight = weight
	}
	if paramOverride != nil {
		updateData.ParamOverride = paramOverride
	}
	if headerOverride != nil {
		updateData.HeaderOverride = headerOverride
	}

	err := DB.Model(&Channel{}).Where("tag = ?", tag).Updates(updateData).Error
	if err != nil {
		return err
	}
	if shouldReCreateAbilities {
		channels, err := GetChannelsByTag(updatedTag, false, false)
		if err != nil {
			refreshChannelCacheAfterWrite()
			return err
		}
		for _, channel := range channels {
			err = channel.UpdateAbilities(nil)
			if err != nil {
				common.SysLog(fmt.Sprintf("failed to update abilities: channel_id=%d, tag=%s, error=%v", channel.Id, channel.GetTag(), err))
				refreshChannelCacheAfterWrite()
				return err
			}
		}
	} else {
		err = UpdateAbilityByTag(tag, newTag, priority, weight)
		if err != nil {
			refreshChannelCacheAfterWrite()
			return err
		}
	}
	refreshChannelCacheAfterWrite()
	return nil
}

// UpdateChannelUsedQuota 累加渠道已用额度。与用户用量计数同理：写入失败
// 必须返回给调用方，才能写进消费日志并定位到具体请求。
func UpdateChannelUsedQuota(id int, quota int) error {
	if common.BatchUpdateEnabled {
		addNewRecord(BatchUpdateTypeChannelUsedQuota, id, quota)
		return nil
	}
	return updateChannelUsedQuota(id, quota)
}

func updateChannelUsedQuota(id int, quota int) error {
	err := DB.Model(&Channel{}).Where("id = ?", id).Update("used_quota", gorm.Expr("used_quota + ?", quota)).Error
	if err != nil {
		common.SysLog(fmt.Sprintf("failed to update channel used quota: channel_id=%d, delta_quota=%d, error=%v", id, quota, err))
	}
	return err
}

func DeleteChannelByStatus(status int64) (int64, error) {
	result := DB.Where("status = ?", status).Delete(&Channel{})
	if result.Error == nil && result.RowsAffected > 0 {
		refreshChannelCacheAfterWrite()
	}
	return result.RowsAffected, result.Error
}

func DeleteDisabledChannel() (int64, error) {
	var deleted int64
	disabledStatuses := []int{common.ChannelStatusAutoDisabled, common.ChannelStatusManuallyDisabled}
	err := DB.Transaction(func(tx *gorm.DB) error {
		// Collect the ids first so the ability cleanup can be scoped to exactly
		// the rows this call deletes; a concurrent insert between the two
		// statements would otherwise lose its abilities.
		var ids []int
		if err := tx.Model(&Channel{}).
			Where("status IN ?", disabledStatuses).
			Pluck("id", &ids).Error; err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		for _, chunk := range lo.Chunk(ids, 200) {
			// 删除语句必须重新断言 status：Pluck 与删除之间管理员可能重新启用
			// 某个渠道，按过期 id 列表删除会把刚启用的渠道连带 ability 一起删掉。
			result := tx.Where("id IN ? AND status IN ?", chunk, disabledStatuses).Delete(&Channel{})
			if result.Error != nil {
				return result.Error
			}
			deleted += result.RowsAffected
			if result.RowsAffected == 0 {
				continue
			}
			// A channel's abilities must not outlive it: an orphan ability row
			// keeps the model listed as routable in some cached paths and breaks
			// the recreated-channel-with-same-id scenario. 只清理真正被删掉的
			// 渠道：被并发重新启用的渠道虽然还在 chunk 里，但不在 survivors
			// 之外，必须保留它的 ability。
			var survivors []int
			if err := tx.Model(&Channel{}).Where("id IN ?", chunk).Pluck("id", &survivors).Error; err != nil {
				return err
			}
			survivorSet := make(map[int]struct{}, len(survivors))
			for _, id := range survivors {
				survivorSet[id] = struct{}{}
			}
			removed := make([]int, 0, len(chunk))
			for _, id := range chunk {
				if _, ok := survivorSet[id]; !ok {
					removed = append(removed, id)
				}
			}
			if len(removed) == 0 {
				continue
			}
			if err := tx.Where("channel_id IN ?", removed).Delete(&Ability{}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err == nil && deleted > 0 {
		refreshChannelCacheAfterWrite()
	}
	return deleted, err
}

func GetPaginatedTags(offset int, limit int) ([]*string, error) {
	return GetPaginatedChannelTags(DB.Model(&Channel{}), offset, limit)
}

func GetPaginatedChannelTags(query *gorm.DB, offset int, limit int) ([]*string, error) {
	var tags []*string
	err := query.
		Select("DISTINCT tag").
		Where("tag is not null AND tag != ''").
		Order(clause.OrderByColumn{Column: clause.Column{Name: "tag"}}).
		Offset(offset).
		Limit(limit).
		Find(&tags).Error
	return tags, err
}

func SearchTags(keyword string, group string, model string, idSort bool) ([]*string, error) {
	var tags []*string
	order := channelPrioritySortClause("desc")
	if idSort {
		order = "id desc"
	}

	// 构造基础查询
	baseQuery := ApplyChannelSearchFilters(DB.Model(&Channel{}).Omit("key"), keyword, group, model)

	subQuery := baseQuery.
		Select("tag").
		Where("tag is not null AND tag != ''").
		Order(order)

	err := DB.Table("(?) as sub", subQuery).
		Select("DISTINCT tag").
		Find(&tags).Error

	if err != nil {
		return nil, err
	}

	return tags, nil
}

func (channel *Channel) ValidateSettings() error {
	channelParams := &dto.ChannelSettings{}
	if channel.Setting != nil && *channel.Setting != "" {
		err := common.Unmarshal([]byte(*channel.Setting), channelParams)
		if err != nil {
			return err
		}
	}
	return nil
}

func (channel *Channel) GetSetting() dto.ChannelSettings {
	setting := dto.ChannelSettings{}
	if channel.Setting != nil && *channel.Setting != "" {
		err := common.Unmarshal([]byte(*channel.Setting), &setting)
		if err != nil {
			common.SysLog(fmt.Sprintf("failed to unmarshal setting (read-only): channel_id=%d, error=%v", channel.Id, err))
		}
	}
	return setting
}

func (channel *Channel) SetSetting(setting dto.ChannelSettings) {
	settingBytes, err := common.Marshal(setting)
	if err != nil {
		common.SysLog(fmt.Sprintf("failed to marshal setting: channel_id=%d, error=%v", channel.Id, err))
		return
	}
	channel.Setting = common.GetPointer[string](string(settingBytes))
}

func (channel *Channel) GetOtherSettings() dto.ChannelOtherSettings {
	setting := dto.ChannelOtherSettings{}
	if channel.OtherSettings != "" {
		err := common.UnmarshalJsonStr(channel.OtherSettings, &setting)
		if err != nil {
			common.SysLog(fmt.Sprintf("failed to unmarshal other settings (read-only): channel_id=%d, error=%v", channel.Id, err))
		}
	}
	return setting
}

func (channel *Channel) SetOtherSettings(setting dto.ChannelOtherSettings) {
	settingBytes, err := common.Marshal(setting)
	if err != nil {
		common.SysLog(fmt.Sprintf("failed to marshal setting: channel_id=%d, error=%v", channel.Id, err))
		return
	}
	channel.OtherSettings = string(settingBytes)
}

func (channel *Channel) ShouldSkipForRequestBodySize(requestBodySize int64, now time.Time, ttlHours int) bool {
	if channel == nil || requestBodySize <= 0 {
		return false
	}
	settings := channel.GetOtherSettings()
	return settings.ShouldSkipForRequestBodySize(requestBodySize, now, ttlHours)
}

func GetChannelsByGroupAndModels(group string, routeModels []string) ([]*Channel, error) {
	if strings.TrimSpace(group) == "" || len(routeModels) == 0 {
		return nil, nil
	}
	var channels []*Channel
	if err := DB.Where("status = ?", common.ChannelStatusEnabled).Find(&channels).Error; err != nil {
		return nil, err
	}
	filtered := make([]*Channel, 0, len(channels))
	for _, channel := range channels {
		if !channelMatchesAnyGroup(channel, []string{group}) {
			continue
		}
		if !channelSupportsAnyModel(channel, routeModels) {
			continue
		}
		filtered = append(filtered, channel)
	}
	return filtered, nil
}

func FindRequestBodyLimitExcludedChannels(group string, modelName string, requestBodySize int64, ttlHours int, now time.Time) (map[int]struct{}, error) {
	if requestBodySize <= 0 {
		return nil, nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if common.MemoryCacheEnabled {
		channelSyncLock.RLock()
		defer channelSyncLock.RUnlock()
		return findRequestBodyLimitExcludedChannelsFromCacheLocked(group, modelName, requestBodySize, ttlHours, now), nil
	}
	return findRequestBodyLimitExcludedChannelsFromDatabase(group, modelName, requestBodySize, ttlHours, now)
}

func findRequestBodyLimitExcludedChannelsFromDatabase(group string, modelName string, requestBodySize int64, ttlHours int, now time.Time) (map[int]struct{}, error) {
	if requestBodySize <= 0 {
		return nil, nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	excluded := make(map[int]struct{})
	routeModels := getGroupModelRouteCandidates(modelName)
	channels, err := GetChannelsByGroupAndModels(group, routeModels)
	if err != nil {
		return nil, err
	}
	for _, channel := range channels {
		if channel.ShouldSkipForRequestBodySize(requestBodySize, now, ttlHours) {
			excluded[channel.Id] = struct{}{}
		}
	}
	return excluded, nil
}

func findRequestBodyLimitExcludedChannelsFromCacheLocked(group string, modelName string, requestBodySize int64, ttlHours int, now time.Time) map[int]struct{} {
	if requestBodySize <= 0 {
		return nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	excluded := make(map[int]struct{})
	routeModels := getGroupModelRouteCandidates(modelName)
	for _, channel := range channelsIDM {
		if channel == nil || channel.Status != common.ChannelStatusEnabled {
			continue
		}
		if !channelMatchesAnyGroup(channel, []string{group}) || !channelSupportsAnyModel(channel, routeModels) {
			continue
		}
		if channel.ShouldSkipForRequestBodySize(requestBodySize, now, ttlHours) {
			excluded[channel.Id] = struct{}{}
		}
	}
	return excluded
}

func mergeExcludedChannels(base map[int]struct{}, extra map[int]struct{}) map[int]struct{} {
	if len(extra) == 0 {
		return base
	}
	merged := make(map[int]struct{}, len(base)+len(extra))
	for channelID := range base {
		merged[channelID] = struct{}{}
	}
	for channelID := range extra {
		merged[channelID] = struct{}{}
	}
	return merged
}

func MarkResponsesCompactAutoFallback(channelId int, reason string) error {
	if channelId <= 0 {
		return nil
	}
	var updatedChannel *Channel
	if err := DB.Transaction(func(tx *gorm.DB) error {
		channel := &Channel{}
		query := tx
		if !common.UsingSQLite {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := query.First(channel, channelId).Error; err != nil {
			return err
		}
		settings := dto.ChannelOtherSettings{}
		if channel.OtherSettings != "" {
			if err := common.UnmarshalJsonStr(channel.OtherSettings, &settings); err != nil {
				return err
			}
		}
		if !settings.IsAutoResponsesCompact() {
			return nil
		}
		settings.MarkResponsesCompactAutoFallback(time.Now().UTC(), reason)
		channel.SetOtherSettings(settings)
		if err := tx.Model(&Channel{}).Where("id = ?", channelId).Update("settings", channel.OtherSettings).Error; err != nil {
			return err
		}
		updatedChannel = channel
		return nil
	}); err != nil {
		return err
	}
	if updatedChannel != nil {
		CacheUpdateChannel(updatedChannel)
	}
	return nil
}

func MarkChannelRequestBodyLimit(channelId int, limit dto.ChannelRequestBodyLimit) (dto.ChannelOtherSettings, error) {
	if channelId <= 0 {
		return dto.ChannelOtherSettings{}, fmt.Errorf("invalid channel id: %d", channelId)
	}
	if limit.MaxBytes <= 0 {
		return dto.ChannelOtherSettings{}, fmt.Errorf("invalid request body limit max bytes: %d", limit.MaxBytes)
	}
	var updatedSettings dto.ChannelOtherSettings
	updatedOtherSettings := ""
	shouldReloadCache := false
	if err := DB.Transaction(func(tx *gorm.DB) error {
		channel := &Channel{}
		query := tx
		if !common.UsingSQLite {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := query.First(channel, channelId).Error; err != nil {
			return err
		}
		settings := dto.ChannelOtherSettings{}
		if channel.OtherSettings != "" {
			if err := common.UnmarshalJsonStr(channel.OtherSettings, &settings); err != nil {
				return err
			}
		}
		settings.RequestBodyLimit = &limit
		channel.SetOtherSettings(settings)
		if err := tx.Model(&Channel{}).Where("id = ?", channelId).Update("settings", channel.OtherSettings).Error; err != nil {
			return err
		}
		updatedSettings = settings
		updatedOtherSettings = channel.OtherSettings
		shouldReloadCache = true
		return nil
	}); err != nil {
		return dto.ChannelOtherSettings{}, err
	}
	if shouldReloadCache {
		CacheUpdateChannelOtherSettings(channelId, updatedOtherSettings)
	}
	return updatedSettings, nil
}

const responsesCapabilityObservationMinUpdateIntervalSeconds int64 = 300

func MarkResponsesCapabilityObservation(channelId int, observation dto.ResponsesCapabilityObservation) (dto.ChannelOtherSettings, error) {
	if channelId <= 0 || observation.ObservedAt == 0 {
		return dto.ChannelOtherSettings{}, nil
	}
	if settings, ok, err := cachedResponsesCapabilityObservationSettings(channelId, observation); err != nil {
		return dto.ChannelOtherSettings{}, err
	} else if ok {
		return settings, nil
	}
	var updatedSettings dto.ChannelOtherSettings
	shouldReloadCache := false
	updatedOtherSettings := ""
	if err := DB.Transaction(func(tx *gorm.DB) error {
		channel := &Channel{}
		query := tx
		if !common.UsingSQLite {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := query.First(channel, channelId).Error; err != nil {
			return err
		}
		settings := dto.ChannelOtherSettings{}
		if channel.OtherSettings != "" {
			if err := common.UnmarshalJsonStr(channel.OtherSettings, &settings); err != nil {
				return err
			}
		}
		if settings.ResponsesCapabilityRegistry == nil {
			settings.ResponsesCapabilityRegistry = &dto.ResponsesChannelCapabilityRegistry{}
		}
		if !shouldUpdateResponsesCapabilityObservation(settings.ResponsesCapabilityRegistry.Observed, observation) {
			updatedSettings = settings
			return nil
		}
		settings.ResponsesCapabilityRegistry.Observed = &observation
		settings.ResponsesCapabilityRegistry.Source = "observed_calls"
		channel.SetOtherSettings(settings)
		if err := tx.Model(&Channel{}).Where("id = ?", channelId).Update("settings", channel.OtherSettings).Error; err != nil {
			return err
		}
		updatedSettings = settings
		updatedOtherSettings = channel.OtherSettings
		shouldReloadCache = true
		return nil
	}); err != nil {
		return dto.ChannelOtherSettings{}, err
	}
	if shouldReloadCache {
		CacheUpdateChannelOtherSettings(channelId, updatedOtherSettings)
	}
	return updatedSettings, nil
}

func cachedResponsesCapabilityObservationSettings(channelId int, observation dto.ResponsesCapabilityObservation) (dto.ChannelOtherSettings, bool, error) {
	if !common.MemoryCacheEnabled {
		return dto.ChannelOtherSettings{}, false, nil
	}
	channel, err := CacheGetChannel(channelId)
	if err != nil || channel == nil || channel.OtherSettings == "" {
		return dto.ChannelOtherSettings{}, false, nil
	}
	settings := dto.ChannelOtherSettings{}
	if err := common.UnmarshalJsonStr(channel.OtherSettings, &settings); err != nil {
		return dto.ChannelOtherSettings{}, false, err
	}
	if settings.ResponsesCapabilityRegistry == nil ||
		shouldUpdateResponsesCapabilityObservation(settings.ResponsesCapabilityRegistry.Observed, observation) {
		return dto.ChannelOtherSettings{}, false, nil
	}
	return settings, true, nil
}

func shouldUpdateResponsesCapabilityObservation(existing *dto.ResponsesCapabilityObservation, next dto.ResponsesCapabilityObservation) bool {
	if existing == nil || existing.ObservedAt <= 0 {
		return true
	}
	if next.ObservedAt <= existing.ObservedAt {
		return false
	}
	if existing.StatusCode != next.StatusCode || existing.ErrorCode != next.ErrorCode {
		return true
	}
	return next.ObservedAt-existing.ObservedAt >= responsesCapabilityObservationMinUpdateIntervalSeconds
}

func (channel *Channel) GetParamOverride() map[string]interface{} {
	paramOverride := make(map[string]interface{})
	if channel.ParamOverride != nil && *channel.ParamOverride != "" {
		err := common.Unmarshal([]byte(*channel.ParamOverride), &paramOverride)
		if err != nil {
			common.SysLog(fmt.Sprintf("failed to unmarshal param override: channel_id=%d, error=%v", channel.Id, err))
		}
	}
	return paramOverride
}

func (channel *Channel) GetHeaderOverride() map[string]interface{} {
	headerOverride := make(map[string]interface{})
	if channel.HeaderOverride != nil && *channel.HeaderOverride != "" {
		err := common.Unmarshal([]byte(*channel.HeaderOverride), &headerOverride)
		if err != nil {
			common.SysLog(fmt.Sprintf("failed to unmarshal header override: channel_id=%d, error=%v", channel.Id, err))
		}
	}
	return headerOverride
}

func GetChannelsByIds(ids []int) ([]*Channel, error) {
	var channels []*Channel
	err := DB.Where("id in (?)", ids).Find(&channels).Error
	return channels, err
}

func BatchSetChannelTag(ids []int, tag *string) error {
	// 开启事务
	tx := DB.Begin()
	if tx.Error != nil {
		return tx.Error
	}

	// 更新标签
	err := tx.Model(&Channel{}).Where("id in (?)", ids).Update("tag", tag).Error
	if err != nil {
		tx.Rollback()
		return err
	}

	// update ability status
	var channels []*Channel
	if err := tx.Where("id in (?)", ids).Find(&channels).Error; err != nil {
		tx.Rollback()
		return err
	}

	for _, channel := range channels {
		err = channel.UpdateAbilities(tx)
		if err != nil {
			tx.Rollback()
			return err
		}
	}

	// 提交事务
	if err := tx.Commit().Error; err != nil {
		return err
	}
	refreshChannelCacheAfterWrite()
	return nil
}

// CountAllChannels returns total channels in DB
func CountAllChannels() (int64, error) {
	var total int64
	err := DB.Model(&Channel{}).Count(&total).Error
	return total, err
}

// CountAllTags returns number of non-empty distinct tags
func CountAllTags() (int64, error) {
	return CountChannelTags(DB.Model(&Channel{}))
}

func CountChannelTags(query *gorm.DB) (int64, error) {
	var total int64
	err := query.Where("tag is not null AND tag != ''").Distinct("tag").Count(&total).Error
	return total, err
}

// Get channels of specified type with pagination
func GetChannelsByType(startIdx int, num int, idSort bool, channelType int) ([]*Channel, error) {
	var channels []*Channel
	order := channelPrioritySortClause("desc")
	if idSort {
		order = "id desc"
	}
	err := DB.Where("type = ?", channelType).Order(order).Limit(num).Offset(startIdx).Omit("key").Find(&channels).Error
	return channels, err
}

// Count channels of specific type
func CountChannelsByType(channelType int) (int64, error) {
	var count int64
	err := DB.Model(&Channel{}).Where("type = ?", channelType).Count(&count).Error
	return count, err
}

// Return map[type]count for all channels
func CountChannelsGroupByType() (map[int64]int64, error) {
	type result struct {
		Type  int64 `gorm:"column:type"`
		Count int64 `gorm:"column:count"`
	}
	var results []result
	err := DB.Model(&Channel{}).Select("type, count(*) as count").Group("type").Find(&results).Error
	if err != nil {
		return nil, err
	}
	counts := make(map[int64]int64)
	for _, r := range results {
		counts[r.Type] = r.Count
	}
	return counts, nil
}

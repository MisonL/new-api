package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"sort"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/pricingrepair"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

type logRow struct {
	ID               int64
	UserID           int64
	Username         string
	TokenID          int64
	ChannelID        int64
	ModelName        string
	Quota            int64
	PromptTokens     int64
	CompletionTokens int64
	CreatedAt        int64
	Other            string
}

type repairedRow struct {
	Row      logRow
	NewQuota int64
	Delta    int64
	NewOther string
}

type summary struct {
	Count    int64
	OldQuota int64
	NewQuota int64
	Delta    int64
}

type quotaBucket struct {
	UserID    int64
	Username  string
	ModelName string
	CreatedAt int64
}

type plan struct {
	Rows          []repairedRow
	ByModel       map[string]summary
	UserDeltas    map[int64]int64
	TokenDeltas   map[int64]int64
	ChannelDeltas map[int64]int64
	Buckets       map[quotaBucket]struct{}
}

func main() {
	dsn := flag.String("dsn", "", "PostgreSQL DSN")
	channelID := flag.Int64("channel-id", 106, "Channel ID to repair")
	apply := flag.Bool("apply", false, "Apply updates instead of dry-run")
	flag.Parse()

	if *dsn == "" {
		log.Fatal("缺少 -dsn")
	}

	ratio_setting.InitRatioSettings()

	db, err := sql.Open("pgx", *dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	rows, err := loadLogs(db, *channelID)
	if err != nil {
		log.Fatal(err)
	}
	plan, err := buildPlan(rows)
	if err != nil {
		log.Fatal(err)
	}

	printSummary(*channelID, plan)
	if !*apply {
		return
	}

	if err := applyPlan(db, plan); err != nil {
		log.Fatal(err)
	}
	log.Printf("apply completed: repaired_logs=%d", len(plan.Rows))
}

func loadLogs(db *sql.DB, channelID int64) ([]logRow, error) {
	const query = `
select id, user_id, username, token_id, channel_id, model_name, quota, prompt_tokens, completion_tokens, created_at, other
from logs
where type = 2
  and channel_id = $1
order by id`

	rows, err := db.Query(query, channelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []logRow
	for rows.Next() {
		var row logRow
		if err := rows.Scan(&row.ID, &row.UserID, &row.Username, &row.TokenID, &row.ChannelID, &row.ModelName, &row.Quota, &row.PromptTokens, &row.CompletionTokens, &row.CreatedAt, &row.Other); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

func buildPlan(rows []logRow) (*plan, error) {
	result := &plan{
		ByModel:       make(map[string]summary),
		UserDeltas:    make(map[int64]int64),
		TokenDeltas:   make(map[int64]int64),
		ChannelDeltas: make(map[int64]int64),
		Buckets:       make(map[quotaBucket]struct{}),
	}

	for _, row := range rows {
		repaired, ok, err := repairRow(row)
		if err != nil {
			return nil, fmt.Errorf("repair log %d: %w", row.ID, err)
		}
		if !ok {
			continue
		}
		result.Rows = append(result.Rows, repaired)
		accumulate(result, repaired)
	}
	return result, nil
}

func repairRow(row logRow) (repairedRow, bool, error) {
	other, err := common.StrToMap(row.Other)
	if err != nil {
		return repairedRow{}, false, err
	}
	if !shouldRepairWalletLog(row, other) {
		return repairedRow{}, false, nil
	}

	modelRatio, ok, _ := ratio_setting.GetConfiguredModelRatio(row.ModelName)
	if !ok {
		return repairedRow{}, false, nil
	}

	cacheRatio, hasCacheRatio := ratio_setting.GetCacheRatio(row.ModelName)
	if !hasCacheRatio {
		cacheRatio = 1
	}

	newQuota := pricingrepair.CalculateQuota(pricingrepair.LogSnapshot{
		PromptTokens:     row.PromptTokens,
		CompletionTokens: row.CompletionTokens,
		CacheTokens:      int64(numberValue(other["cache_tokens"])),
		GroupRatio:       numberValueOrDefault(other["group_ratio"], 1),
	}, pricingrepair.ModelPricing{
		ModelRatio:      modelRatio,
		CompletionRatio: ratio_setting.GetCompletionRatio(row.ModelName),
		CacheRatio:      cacheRatio,
	})

	other["model_ratio"] = modelRatio
	other["completion_ratio"] = ratio_setting.GetCompletionRatio(row.ModelName)
	if hasCacheRatio {
		other["cache_ratio"] = cacheRatio
	}
	newOther := common.MapToJsonStr(other)

	if newQuota == row.Quota && newOther == row.Other {
		return repairedRow{}, false, nil
	}

	return repairedRow{
		Row:      row,
		NewQuota: newQuota,
		Delta:    newQuota - row.Quota,
		NewOther: newOther,
	}, true, nil
}

func accumulate(plan *plan, repaired repairedRow) {
	item := plan.ByModel[repaired.Row.ModelName]
	item.Count++
	item.OldQuota += repaired.Row.Quota
	item.NewQuota += repaired.NewQuota
	item.Delta += repaired.Delta
	plan.ByModel[repaired.Row.ModelName] = item

	plan.UserDeltas[repaired.Row.UserID] += repaired.Delta
	if repaired.Row.TokenID > 0 {
		plan.TokenDeltas[repaired.Row.TokenID] += repaired.Delta
	}
	plan.ChannelDeltas[repaired.Row.ChannelID] += repaired.Delta
	plan.Buckets[quotaBucket{
		UserID:    repaired.Row.UserID,
		Username:  repaired.Row.Username,
		ModelName: repaired.Row.ModelName,
		CreatedAt: pricingrepair.HourBucket(repaired.Row.CreatedAt),
	}] = struct{}{}
}

func printSummary(channelID int64, plan *plan) {
	log.Printf("channel_id=%d repaired_logs=%d", channelID, len(plan.Rows))
	models := make([]string, 0, len(plan.ByModel))
	for model := range plan.ByModel {
		models = append(models, model)
	}
	sort.Strings(models)
	for _, model := range models {
		item := plan.ByModel[model]
		log.Printf("model=%s count=%d old_quota=%d new_quota=%d delta=%d", model, item.Count, item.OldQuota, item.NewQuota, item.Delta)
	}
}

func applyPlan(db *sql.DB, plan *plan) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := applyLogUpdates(tx, plan.Rows); err != nil {
		return err
	}
	if err := applyUserDeltas(tx, plan.UserDeltas); err != nil {
		return err
	}
	if err := applyTokenDeltas(tx, plan.TokenDeltas); err != nil {
		return err
	}
	if err := applyChannelDeltas(tx, plan.ChannelDeltas); err != nil {
		return err
	}
	if err := rebuildQuotaData(tx, plan.Buckets); err != nil {
		return err
	}

	return tx.Commit()
}

func applyLogUpdates(tx *sql.Tx, rows []repairedRow) error {
	const query = `update logs set quota = $1, other = $2 where id = $3`
	for _, row := range rows {
		if _, err := tx.Exec(query, row.NewQuota, row.NewOther, row.Row.ID); err != nil {
			return err
		}
	}
	return nil
}

func applyUserDeltas(tx *sql.Tx, deltas map[int64]int64) error {
	const query = `update users set quota = quota - $1, used_quota = used_quota + $1 where id = $2`
	for userID, delta := range deltas {
		if delta == 0 {
			continue
		}
		if _, err := tx.Exec(query, delta, userID); err != nil {
			return err
		}
	}
	return nil
}

func applyTokenDeltas(tx *sql.Tx, deltas map[int64]int64) error {
	const query = `update tokens set remain_quota = remain_quota - $1, used_quota = used_quota + $1 where id = $2`
	for tokenID, delta := range deltas {
		if delta == 0 {
			continue
		}
		if _, err := tx.Exec(query, delta, tokenID); err != nil {
			return err
		}
	}
	return nil
}

func applyChannelDeltas(tx *sql.Tx, deltas map[int64]int64) error {
	const query = `update channels set used_quota = used_quota + $1 where id = $2`
	for channelID, delta := range deltas {
		if delta == 0 {
			continue
		}
		if _, err := tx.Exec(query, delta, channelID); err != nil {
			return err
		}
	}
	return nil
}

func rebuildQuotaData(tx *sql.Tx, buckets map[quotaBucket]struct{}) error {
	for bucket := range buckets {
		if err := rebuildQuotaBucket(tx, bucket); err != nil {
			return err
		}
	}
	return nil
}

func rebuildQuotaBucket(tx *sql.Tx, bucket quotaBucket) error {
	const deleteSQL = `delete from quota_data where user_id = $1 and username = $2 and model_name = $3 and created_at = $4`
	if _, err := tx.Exec(deleteSQL, bucket.UserID, bucket.Username, bucket.ModelName, bucket.CreatedAt); err != nil {
		return err
	}

	const selectSQL = `
select count(*), coalesce(sum(quota), 0), coalesce(sum(prompt_tokens + completion_tokens), 0)
from logs
where type = 2
  and user_id = $1
  and username = $2
  and model_name = $3
  and created_at >= $4
  and created_at < $5`

	var count, quota, tokenUsed int64
	if err := tx.QueryRow(selectSQL, bucket.UserID, bucket.Username, bucket.ModelName, bucket.CreatedAt, bucket.CreatedAt+3600).Scan(&count, &quota, &tokenUsed); err != nil {
		return err
	}
	if count == 0 {
		return nil
	}

	const insertSQL = `
insert into quota_data (user_id, username, model_name, created_at, token_used, count, quota)
values ($1, $2, $3, $4, $5, $6, $7)`

	_, err := tx.Exec(insertSQL, bucket.UserID, bucket.Username, bucket.ModelName, bucket.CreatedAt, tokenUsed, count, quota)
	return err
}

func billingSource(other map[string]interface{}) string {
	if other == nil {
		return ""
	}
	if value, ok := other["billing_source"].(string); ok {
		return value
	}
	return ""
}

func shouldRepairWalletLog(row logRow, other map[string]interface{}) bool {
	switch billingSource(other) {
	case "wallet":
		return true
	case "subscription":
		return false
	case "":
		return row.TokenID == 0 && !hasSubscriptionMetadata(other)
	default:
		return false
	}
}

func hasSubscriptionMetadata(other map[string]interface{}) bool {
	if other == nil {
		return false
	}
	keys := []string{
		"subscription_id",
		"subscription_pre_consumed",
		"subscription_post_delta",
		"subscription_plan_id",
		"subscription_plan_title",
		"subscription_total",
		"subscription_used",
		"subscription_remain",
		"subscription_consumed",
	}
	for _, key := range keys {
		if _, ok := other[key]; ok {
			return true
		}
	}
	return false
}

func numberValueOrDefault(value interface{}, fallback float64) float64 {
	parsed := numberValue(value)
	if parsed == 0 {
		return fallback
	}
	return parsed
}

func numberValue(value interface{}) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	default:
		return 0
	}
}

package main

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/pkg/pricingrepair"
	"github.com/QuantumNous/new-api/setting/billing_setting"
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
	dsnEnv := flag.String("dsn-env", "", "Read PostgreSQL DSN from the named environment variable")
	channelID := flag.Int64("channel-id", 106, "Channel ID to repair")
	modelNames := flag.String("models", "", "Comma-separated model names to repair; required when channel-id is 0")
	all := flag.Bool("all", false, "Repair all type=2 logs; explicit opt-in")
	excludeModelNames := flag.String("exclude-models", "", "Comma-separated model names to leave unchanged")
	minID := flag.Int64("min-id", 0, "Only inspect logs with an ID greater than or equal to this value")
	maxID := flag.Int64("max-id", 0, "Only inspect logs with an ID less than or equal to this value")
	beforeCreatedAt := flag.Int64("before-created-at", 0, "Only inspect logs created before this Unix timestamp")
	apply := flag.Bool("apply", false, "Apply updates instead of dry-run")
	redisDSN := flag.String("redis-dsn", os.Getenv("REDIS_CONN_STRING"), "Redis connection URL used to invalidate quota caches after apply")
	redisDSNEnv := flag.String("redis-dsn-env", "", "Read Redis connection URL from the named environment variable")
	skipCacheInvalidation := flag.Bool("skip-cache-invalidation", false, "Explicitly skip Redis quota cache invalidation after apply")
	flag.Parse()

	if strings.TrimSpace(*dsn) == "" && strings.TrimSpace(*dsnEnv) != "" {
		*dsn = os.Getenv(strings.TrimSpace(*dsnEnv))
	}
	if strings.TrimSpace(*redisDSNEnv) != "" {
		*redisDSN = os.Getenv(strings.TrimSpace(*redisDSNEnv))
	}
	if *dsn == "" {
		log.Fatal("缺少 -dsn")
	}
	models := parseModelFilter(*modelNames)
	excludedModels := parseModelFilter(*excludeModelNames)
	if *all {
		*channelID = 0
		models = nil
	}
	if !*all && *channelID == 0 && len(models) == 0 {
		log.Fatal("channel-id 为 0 时必须通过 -models 限定模型")
	}
	if *minID < 0 || *maxID < 0 || (*minID > 0 && *maxID > 0 && *minID > *maxID) {
		log.Fatal("无效的 ID 范围")
	}

	ratio_setting.InitRatioSettings()

	db, err := sql.Open("pgx", *dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	rows, err := loadLogs(db, *channelID, models, excludedModels, *minID, *maxID, *beforeCreatedAt)
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

	if err := applyPlan(db, plan, *redisDSN, *skipCacheInvalidation); err != nil {
		log.Fatal(err)
	}
	log.Printf("apply completed: repaired_logs=%d", len(plan.Rows))
}

func loadLogs(db *sql.DB, channelID int64, models, excludedModels []string, minID, maxID, beforeCreatedAt int64) ([]logRow, error) {
	query := `
select id, user_id, username, token_id, channel_id, model_name, quota, prompt_tokens, completion_tokens, created_at, other
from logs
where type = 2
	`
	args := make([]interface{}, 0, 1+len(models))
	if channelID != 0 {
		args = append(args, channelID)
		query += fmt.Sprintf("  and channel_id = $%d\n", len(args))
	}
	if minID > 0 {
		args = append(args, minID)
		query += fmt.Sprintf("  and id >= $%d\n", len(args))
	}
	if maxID > 0 {
		args = append(args, maxID)
		query += fmt.Sprintf("  and id <= $%d\n", len(args))
	}
	if len(models) > 0 {
		placeholders := make([]string, 0, len(models))
		for _, model := range models {
			args = append(args, model)
			placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
		}
		query += fmt.Sprintf("  and lower(model_name) in (%s)\n", strings.Join(placeholders, ", "))
	}
	if len(excludedModels) > 0 {
		placeholders := make([]string, 0, len(excludedModels))
		for _, model := range excludedModels {
			args = append(args, model)
			placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
		}
		query += fmt.Sprintf("  and lower(model_name) not in (%s)\n", strings.Join(placeholders, ", "))
	}
	if beforeCreatedAt > 0 {
		args = append(args, beforeCreatedAt)
		query += fmt.Sprintf("  and created_at < $%d\n", len(args))
	}
	query += "order by id"

	rows, err := db.Query(query, args...)
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

func parseModelFilter(raw string) []string {
	parts := strings.Split(raw, ",")
	models := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		model := strings.ToLower(strings.TrimSpace(part))
		if model == "" {
			continue
		}
		if _, exists := seen[model]; exists {
			continue
		}
		seen[model] = struct{}{}
		models = append(models, model)
	}
	return models
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
	// Compare against a canonical representation so historical key ordering or
	// insignificant JSON formatting does not turn a no-op into a database write.
	originalOther := common.MapToJsonStr(other)
	if repaired, ok, err := repairGrok45TieredFallback(row, other, originalOther); ok || err != nil {
		return repaired, ok, err
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
	cacheCreationRatio, _ := ratio_setting.GetCreateCacheRatio(row.ModelName)
	cacheCreationRatio5m := cacheCreationRatio
	cacheCreationRatio1h := cacheCreationRatio * pricingrepair.ClaudeCacheCreation1hMultiplier

	cacheTokens, err := tokenCount(other, "cache_tokens")
	if err != nil {
		return repairedRow{}, false, fmt.Errorf("invalid cache_tokens: %w", err)
	}
	cacheCreationTokens, err := tokenCount(other, "cache_creation_tokens")
	if err != nil {
		return repairedRow{}, false, fmt.Errorf("invalid cache_creation_tokens: %w", err)
	}
	cacheCreationTokens5m, err := tokenCount(other, "cache_creation_tokens_5m")
	if err != nil {
		return repairedRow{}, false, fmt.Errorf("invalid cache_creation_tokens_5m: %w", err)
	}
	cacheCreationTokens1h, err := tokenCount(other, "cache_creation_tokens_1h")
	if err != nil {
		return repairedRow{}, false, fmt.Errorf("invalid cache_creation_tokens_1h: %w", err)
	}

	groupRatio := 1.0
	groupRatioSet := false
	if rawGroupRatio, exists := other["group_ratio"]; exists {
		parsedGroupRatio, ok := numberValuePresent(rawGroupRatio)
		if !ok || parsedGroupRatio < 0 {
			return repairedRow{}, false, fmt.Errorf("invalid group_ratio")
		}
		groupRatio = parsedGroupRatio
		groupRatioSet = true
	}
	usageSemantic := strings.ToLower(strings.TrimSpace(stringValue(other["usage_semantic"])))
	legacyClaudeDerived := usageSemantic == "" && !boolValue(other["claude"]) && (cacheCreationTokens5m > 0 || cacheCreationTokens1h > 0)
	claudeUsage := usageSemantic == "anthropic" || boolValue(other["claude"])

	newQuota := pricingrepair.CalculateQuota(pricingrepair.LogSnapshot{
		PromptTokens:          row.PromptTokens,
		CompletionTokens:      row.CompletionTokens,
		CacheTokens:           cacheTokens,
		CacheCreationTokens:   cacheCreationTokens,
		CacheCreationTokens5m: cacheCreationTokens5m,
		CacheCreationTokens1h: cacheCreationTokens1h,
		GroupRatio:            groupRatio,
		GroupRatioSet:         groupRatioSet,
		UsageSemantic:         map[bool]string{true: "anthropic", false: "openai"}[claudeUsage],
		LegacyClaudeDerived:   legacyClaudeDerived,
	}, pricingrepair.ModelPricing{
		ModelRatio:           modelRatio,
		CompletionRatio:      ratio_setting.GetCompletionRatio(row.ModelName),
		CacheRatio:           cacheRatio,
		CacheCreationRatio:   cacheCreationRatio,
		CacheCreationRatio5m: cacheCreationRatio5m,
		CacheCreationRatio1h: cacheCreationRatio1h,
	})

	other["model_ratio"] = modelRatio
	other["completion_ratio"] = ratio_setting.GetCompletionRatio(row.ModelName)
	if hasCacheRatio || cacheTokens > 0 || hasAnyKey(other, "cache_tokens", "cache_ratio") {
		other["cache_ratio"] = cacheRatio
	}
	if hasAnyKey(other, "cache_creation_tokens", "cache_creation_tokens_5m", "cache_creation_tokens_1h", "cache_creation_ratio", "cache_creation_ratio_5m", "cache_creation_ratio_1h") || cacheCreationTokens > 0 || cacheCreationTokens5m > 0 || cacheCreationTokens1h > 0 {
		other["cache_creation_ratio"] = cacheCreationRatio
		if hasAnyKey(other, "cache_creation_tokens_5m", "cache_creation_ratio_5m") || cacheCreationTokens5m > 0 {
			other["cache_creation_ratio_5m"] = cacheCreationRatio5m
		}
		if hasAnyKey(other, "cache_creation_tokens_1h", "cache_creation_ratio_1h") || cacheCreationTokens1h > 0 {
			other["cache_creation_ratio_1h"] = cacheCreationRatio1h
		}
	}
	newOther := common.MapToJsonStr(other)

	if newQuota == row.Quota && newOther == originalOther {
		return repairedRow{}, false, nil
	}

	return repairedRow{
		Row:      row,
		NewQuota: newQuota,
		Delta:    newQuota - row.Quota,
		NewOther: newOther,
	}, true, nil
}

// repairGrok45TieredFallback repairs only the unambiguous Grok 4.5 aliases
// that were logged with the old 37.5/1/1 fallback snapshot. The standard
// ratio repair path intentionally skips current tiered models because it
// cannot reproduce context tiers. Grok 4.5's official expression is safe here
// because the request shape and cache dimensions are recorded in the log.
// A zero-cache grok-4-5 row is also accepted for metadata-only normalization:
// its standard ratio and the tiered expression produce the same quota.
func repairGrok45TieredFallback(row logRow, other map[string]interface{}, originalOther string) (repairedRow, bool, error) {
	if other == nil || !hasRepairablePriceSnapshot(other) {
		return repairedRow{}, false, nil
	}
	if !strings.EqualFold(ratio_setting.FormatMatchingModelName(row.ModelName), "grok-4.5") {
		return repairedRow{}, false, nil
	}
	if billingMode, ok := other["billing_mode"].(string); ok && billingMode == billing_setting.BillingModeTieredExpr {
		return repairedRow{}, false, nil
	}
	if billing_setting.GetBillingMode(row.ModelName) != billing_setting.BillingModeTieredExpr {
		return repairedRow{}, false, nil
	}
	if hasImageRequestMetadata(other) {
		return repairedRow{}, false, nil
	}
	for _, key := range []string{
		"web_search",
		"file_search",
		"image_generation_call",
		"audio_input_seperate_price",
		"image",
	} {
		if _, ok := other[key]; ok {
			return repairedRow{}, false, nil
		}
	}
	switch billingSource(other) {
	case "wallet":
	case "subscription":
		return repairedRow{}, false, nil
	case "":
		if row.TokenID != 0 || hasSubscriptionMetadata(other) {
			return repairedRow{}, false, nil
		}
	default:
		return repairedRow{}, false, nil
	}

	modelRatio := numberValue(other["model_ratio"])
	completionRatio := numberValue(other["completion_ratio"])
	cacheRatio := numberValueOrDefault(other["cache_ratio"], -1)
	cacheTokens, err := tokenCount(other, "cache_tokens")
	if err != nil {
		return repairedRow{}, false, fmt.Errorf("invalid cache_tokens: %w", err)
	}

	fallbackSnapshot := approxEqual(modelRatio, 37.5) && approxEqual(completionRatio, 1) && approxEqual(cacheRatio, 1)
	metadataOnlySnapshot := strings.EqualFold(strings.TrimSpace(row.ModelName), "grok-4-5") &&
		cacheTokens == 0 && approxEqual(modelRatio, 1) && approxEqual(completionRatio, 3) && approxEqual(cacheRatio, 0.25)
	if !fallbackSnapshot && !metadataOnlySnapshot {
		return repairedRow{}, false, nil
	}

	exprString, ok := billing_setting.GetBillingExpr(row.ModelName)
	if !ok || exprString == "" {
		return repairedRow{}, false, nil
	}
	groupRatio := 1.0
	if rawGroupRatio, exists := other["group_ratio"]; exists {
		parsedGroupRatio, ok := numberValuePresent(rawGroupRatio)
		if !ok || parsedGroupRatio < 0 {
			return repairedRow{}, false, fmt.Errorf("invalid group_ratio")
		}
		groupRatio = parsedGroupRatio
	}

	params := billingexpr.TokenParams{
		P:   float64(row.PromptTokens),
		C:   float64(row.CompletionTokens),
		Len: float64(row.PromptTokens),
		CR:  float64(cacheTokens),
	}
	usedVars := billingexpr.UsedVars(exprString)
	if usedVars["cr"] {
		params.P -= params.CR
		if params.P < 0 {
			params.P = 0
		}
	}
	rawCost, trace, err := billingexpr.RunExpr(exprString, params)
	if err != nil {
		return repairedRow{}, false, fmt.Errorf("run Grok 4.5 billing expression: %w", err)
	}
	newQuota := int64(billingexpr.QuotaRound(rawCost / 1_000_000 * common.QuotaPerUnit * groupRatio))

	other["billing_mode"] = billing_setting.BillingModeTieredExpr
	other["expr_b64"] = base64.StdEncoding.EncodeToString([]byte(exprString))
	other["matched_tier"] = trace.MatchedTier
	other["model_price"] = 0
	other["model_ratio"] = 0
	other["completion_ratio"] = 0
	other["cache_ratio"] = 0
	newOther := common.MapToJsonStr(other)
	if newQuota == row.Quota && newOther == originalOther {
		return repairedRow{}, false, nil
	}

	return repairedRow{
		Row:      row,
		NewQuota: newQuota,
		Delta:    newQuota - row.Quota,
		NewOther: newOther,
	}, true, nil
}

func approxEqual(left, right float64) bool {
	return math.Abs(left-right) < 1e-9
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

func applyPlan(db *sql.DB, plan *plan, redisDSN string, skipCacheInvalidation bool) error {
	if err := validateApplyCachePrerequisites(redisDSN, skipCacheInvalidation); err != nil {
		return err
	}

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

	if err := tx.Commit(); err != nil {
		return err
	}
	if skipCacheInvalidation {
		log.Printf("redis cache invalidation explicitly skipped")
		return nil
	}
	if err := invalidateRedisCaches(db, plan, redisDSN); err != nil {
		return fmt.Errorf("database committed but Redis cache invalidation failed: %w", err)
	}
	return nil
}

func validateApplyCachePrerequisites(redisDSN string, skipCacheInvalidation bool) error {
	if skipCacheInvalidation {
		return nil
	}
	if strings.TrimSpace(redisDSN) == "" {
		return fmt.Errorf("--apply requires Redis DSN; pass -redis-dsn or REDIS_CONN_STRING, or explicitly use -skip-cache-invalidation")
	}
	if err := configureCryptoSecretFromEnv(); err != nil {
		return err
	}
	return pingRedis(redisDSN)
}

func configureCryptoSecretFromEnv() error {
	cryptoSecret := strings.TrimSpace(os.Getenv("CRYPTO_SECRET"))
	if cryptoSecret == "" {
		return fmt.Errorf("--apply requires CRYPTO_SECRET so token cache keys match the running service")
	}
	if cryptoSecret == "random_string" {
		return fmt.Errorf("CRYPTO_SECRET must be a stable random string")
	}
	common.CryptoSecret = cryptoSecret
	return nil
}

func pingRedis(redisDSN string) error {
	options, err := redis.ParseURL(strings.TrimSpace(redisDSN))
	if err != nil {
		return fmt.Errorf("parse Redis DSN: %w", err)
	}
	client := redis.NewClient(options)
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := client.Ping(ctx).Result(); err != nil {
		return fmt.Errorf("ping Redis before database transaction: %w", err)
	}
	return nil
}

func invalidateRedisCaches(db *sql.DB, plan *plan, redisDSN string) error {
	redisDSN = strings.TrimSpace(redisDSN)
	if redisDSN == "" {
		return fmt.Errorf("Redis DSN is required for cache invalidation")
	}

	options, err := redis.ParseURL(redisDSN)
	if err != nil {
		return fmt.Errorf("parse Redis DSN: %w", err)
	}
	client := redis.NewClient(options)
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := client.Ping(ctx).Result(); err != nil {
		return fmt.Errorf("ping Redis: %w", err)
	}

	keys := make([]string, 0, len(plan.UserDeltas)+len(plan.TokenDeltas))
	for userID, delta := range plan.UserDeltas {
		if delta != 0 {
			keys = append(keys, fmt.Sprintf("user:%d", userID))
		}
	}
	for tokenID, delta := range plan.TokenDeltas {
		if delta == 0 {
			continue
		}
		var tokenKey string
		if err := db.QueryRowContext(ctx, `select "key" from tokens where id = $1`, tokenID).Scan(&tokenKey); err != nil {
			if err == sql.ErrNoRows {
				continue
			}
			return fmt.Errorf("load token %d key: %w", tokenID, err)
		}
		if tokenKey != "" {
			keys = append(keys, "token:"+common.GenerateHMAC(tokenKey))
		}
	}
	if len(keys) == 0 {
		return nil
	}
	if err := client.Del(ctx, keys...).Err(); err != nil {
		return err
	}
	log.Printf("redis cache invalidated: keys=%d", len(keys))
	return nil
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
	if other == nil || !hasRepairablePriceSnapshot(other) {
		return false
	}
	if billing_setting.GetBillingMode(row.ModelName) == billing_setting.BillingModeTieredExpr {
		return false
	}
	if isNonTokenModel(row.ModelName) {
		return false
	}
	if hasImageRequestMetadata(other) {
		return false
	}
	if billingMode, ok := other["billing_mode"].(string); ok && billingMode == "tiered_expr" {
		return false
	}
	for _, key := range []string{
		"web_search",
		"file_search",
		"image_generation_call",
		"audio_input_seperate_price",
		"image",
	} {
		if _, ok := other[key]; ok {
			return false
		}
	}

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

// Standard ratio repair cannot reproduce per-item, per-second, modality-specific,
// or search-aware billing. Keep those logs on their historical snapshot unless a
// dedicated repair path has the required measurement fields.
func isNonTokenModel(modelName string) bool {
	name := strings.ToLower(strings.TrimSpace(modelName))
	if name == "" {
		return true
	}
	if _, ok := ratio_setting.GetModelPrice(modelName, false); ok {
		return true
	}
	for _, marker := range []string{
		"audio", "embedding", "rerank", "re-rank", "realtime", "speech", "whisper", "tts", "asr",
		"image", "imagen", "imagine", "video", "veo", "i2v", "t2v", "search", "deep-research",
		"suno", "flux", "mj_",
	} {
		if strings.Contains(name, marker) {
			return true
		}
	}
	return name == "deepseek-v4-flash-free" || name == "sensenova-6.7-flash-lite"
}

func hasImageRequestMetadata(other map[string]interface{}) bool {
	for _, key := range []string{"request_path", "upstream_request_path"} {
		path, ok := other[key].(string)
		if !ok {
			continue
		}
		path = strings.ToLower(strings.TrimSpace(path))
		if index := strings.IndexByte(path, '?'); index >= 0 {
			path = path[:index]
		}
		if strings.HasPrefix(path, "/v1/images/") || strings.Contains(path, "/images/generations") || strings.Contains(path, "/images/edits") {
			return true
		}
	}

	value, exists := other["request_conversion"]
	if !exists {
		return false
	}
	return containsImageConversion(value)
}

func containsImageConversion(value interface{}) bool {
	match := func(raw string) bool {
		return strings.EqualFold(strings.TrimSpace(raw), "openai_image")
	}
	switch values := value.(type) {
	case string:
		return match(values)
	case []string:
		for _, item := range values {
			if match(item) {
				return true
			}
		}
	case []interface{}:
		for _, item := range values {
			if raw, ok := item.(string); ok && match(raw) {
				return true
			}
		}
	}
	return false
}

func hasRepairablePriceSnapshot(other map[string]interface{}) bool {
	modelPrice, ok := numberValuePresent(other["model_price"])
	if !ok || modelPrice >= 0 {
		return false
	}
	for _, key := range []string{"model_ratio", "completion_ratio"} {
		ratio, ok := numberValuePresent(other[key])
		if !ok || ratio < 0 {
			return false
		}
	}
	return true
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
	parsed, ok := numberValuePresent(value)
	if !ok {
		return fallback
	}
	return parsed
}

func numberValue(value interface{}) float64 {
	parsed, ok := numberValuePresent(value)
	if !ok {
		return 0
	}
	return parsed
}

func numberValuePresent(value interface{}) (float64, bool) {
	switch v := value.(type) {
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return 0, false
		}
		return v, true
	case float32:
		parsed := float64(v)
		if math.IsNaN(parsed) || math.IsInf(parsed, 0) {
			return 0, false
		}
		return parsed, true
	case int:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		parsed, err := strconv.ParseFloat(string(v), 64)
		if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
			return 0, false
		}
		return parsed, true
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err == nil && !math.IsNaN(parsed) && !math.IsInf(parsed, 0) {
			return parsed, true
		}
	default:
		return 0, false
	}
	return 0, false
}

func tokenCount(other map[string]interface{}, key string) (int64, error) {
	raw, exists := other[key]
	if !exists {
		return 0, nil
	}
	parsed, ok := numberValuePresent(raw)
	if !ok || parsed < 0 || math.Trunc(parsed) != parsed || parsed >= float64(uint64(1)<<63) {
		return 0, fmt.Errorf("must be a non-negative integer")
	}
	return int64(parsed), nil
}

func stringValue(value interface{}) string {
	if parsed, ok := value.(string); ok {
		return parsed
	}
	return ""
}

func boolValue(value interface{}) bool {
	switch parsed := value.(type) {
	case bool:
		return parsed
	case string:
		return strings.EqualFold(strings.TrimSpace(parsed), "true")
	default:
		return false
	}
}

func hasAnyKey(values map[string]interface{}, keys ...string) bool {
	for _, key := range keys {
		if _, ok := values[key]; ok {
			return true
		}
	}
	return false
}

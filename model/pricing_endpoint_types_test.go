package model

import (
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// setupPricingEndpointTypesTestDB installs a private in-memory database for the
// pricing snapshots. The snapshots are process-wide with a one-minute TTL, so
// the cache must be invalidated or the test would read the previous test's data.
func setupPricingEndpointTypesTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	previousUsingSQLite := common.UsingSQLite
	previousUsingMySQL := common.UsingMySQL
	previousUsingPostgreSQL := common.UsingPostgreSQL
	previousDB := DB
	previousLogDB := LOG_DB

	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	initCol()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)

	DB = db
	LOG_DB = db
	// Vendor 是 updatePricing 的第二个预加载表。真实库一定有它；fixture 缺表会
	// 让刷新在供应商查询处失败，测试于是测不到定价构建本身。
	require.NoError(t, DB.AutoMigrate(&Channel{}, &Ability{}, &Model{}, &Vendor{}))
	InvalidatePricingCache()

	t.Cleanup(func() {
		common.UsingSQLite = previousUsingSQLite
		common.UsingMySQL = previousUsingMySQL
		common.UsingPostgreSQL = previousUsingPostgreSQL
		DB = previousDB
		LOG_DB = previousLogDB
		initCol()
		InvalidatePricingCache()

		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})

	return db
}

// A model declared with an explicit endpoint list keeps that list: it is a
// statement about the model, not about any one channel.
func TestEndpointTypesFromModelMetaReadsDeclaredEndpoints(t *testing.T) {
	endpoints := endpointTypesFromModelMeta(`{"openai":"/v1/chat/completions","anthropic":{"path":"/v1/messages"}}`)

	require.Equal(t, []constant.EndpointType{
		constant.EndpointType("anthropic"),
		constant.EndpointType("openai"),
	}, endpoints)
}

func TestEndpointTypesFromModelMetaIgnoresUnsupportedShapes(t *testing.T) {
	require.Empty(t, endpointTypesFromModelMeta(``))
	require.Empty(t, endpointTypesFromModelMeta(`not json`))
	require.Empty(t, endpointTypesFromModelMeta(`{"openai":123}`))
}

// An empty model name has no endpoints; the caller must not fall through to the
// global table and invent some.
func TestGetModelSupportEndpointTypesForGroupsRejectsEmptyModel(t *testing.T) {
	require.Empty(t, GetModelSupportEndpointTypesForGroups("", []string{"default"}))
	require.Empty(t, GetModelSupportEndpointTypesForGroups("   ", []string{"default"}))
}

// The group-scoped lookup must answer from the same snapshot the global lookup
// uses, so a model that declares its own endpoint list keeps it.
func TestGetModelSupportEndpointTypesForGroupsHonoursDeclaredEndpoints(t *testing.T) {
	db := setupPricingEndpointTypesTestDB(t)
	require.NoError(t, db.Create(&Model{
		ModelName: "declared-endpoints-model",
		Endpoints: `{"anthropic":"/v1/messages"}`,
		NameRule:  NameRuleExact,
	}).Error)
	require.NoError(t, db.Create(&Channel{
		Id:     7101,
		Name:   "openai-only",
		Key:    "sk-a",
		Type:   1,
		Status: 1,
		Group:  "default",
	}).Error)
	require.NoError(t, db.Create(&Ability{
		Group: "default", Model: "declared-endpoints-model", ChannelId: 7101, Enabled: true,
	}).Error)
	InvalidatePricingCache()

	endpoints := GetModelSupportEndpointTypesForGroups("declared-endpoints-model", []string{"default"})

	require.Equal(t, []constant.EndpointType{constant.EndpointType("anthropic")}, endpoints,
		"the declared list is a statement about the model, so it wins over the derived one")
}

package model

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// updatePricing 的两处预加载（模型元数据、供应商）用 `_ =` 丢弃了错误。查询
// 失败时它会把空元数据当成事实继续构建，发布一份"没有描述、没有供应商"的
// 定价列表覆盖掉上一份正确缓存。缓存本就是一次失败的刷新不该动的东西：失败
// 时必须保留旧缓存并记录错误。

func seedPricingMetadataTestData(t *testing.T) {
	t.Helper()
	require.NoError(t, DB.Create(&Channel{Id: 7301, Name: "meta", Key: "sk-m", Type: 1, Status: 1, Group: "default"}).Error)
	require.NoError(t, DB.Create(&Ability{Group: "default", Model: "meta-probe-model", ChannelId: 7301, Enabled: true}).Error)
	require.NoError(t, DB.Create(&Model{
		ModelName:   "meta-probe-model",
		Description: "probe description",
		Status:      1,
		NameRule:    NameRuleExact,
	}).Error)
	require.NoError(t, DB.Create(&Vendor{Id: 7301, Name: "probe-vendor"}).Error)
}

func findPricingByModel(pricing []Pricing, modelName string) (Pricing, bool) {
	for _, p := range pricing {
		if p.ModelName == modelName {
			return p, true
		}
	}
	return Pricing{}, false
}

func TestUpdatePricingKeepsPreviousCacheWhenModelQueryFails(t *testing.T) {
	db := setupPricingEndpointTypesTestDB(t)
	seedPricingMetadataTestData(t)

	updatePricing()
	pricing, ok := findPricingByModel(pricingMap, "meta-probe-model")
	require.True(t, ok, "fixture precondition: the model must be published before the query breaks")
	require.Equal(t, "probe description", pricing.Description)

	// 让元数据查询失败：删表后 Find 报错。没有防御的实现在这里会把空元数据
	// 当成事实，用一份没有描述的列表覆盖上面的缓存。
	require.NoError(t, db.Migrator().DropTable(&Model{}))
	updatePricing()

	pricing, ok = findPricingByModel(pricingMap, "meta-probe-model")
	require.True(t, ok, "a failed refresh must not wipe the published pricing list")
	require.Equal(t, "probe description", pricing.Description,
		"a failed metadata query must not be treated as 'no metadata'")
}

func TestUpdatePricingKeepsPreviousCacheWhenVendorQueryFails(t *testing.T) {
	db := setupPricingEndpointTypesTestDB(t)
	seedPricingMetadataTestData(t)

	updatePricing()
	require.NotEmpty(t, vendorsList, "fixture precondition: vendors must be published before the query breaks")

	require.NoError(t, db.Migrator().DropTable(&Vendor{}))
	updatePricing()

	require.NotEmpty(t, vendorsList, "a failed vendor query must not wipe the published vendor list")
}

// 失败的刷新不能反复打数据库：记录时间戳，按正常 TTL 重试。
func TestUpdatePricingFailureDefersTheNextRefresh(t *testing.T) {
	db := setupPricingEndpointTypesTestDB(t)
	seedPricingMetadataTestData(t)

	require.NoError(t, db.Migrator().DropTable(&Model{}))
	before := time.Now()
	updatePricing()

	updatePricingLock.Lock()
	defer updatePricingLock.Unlock()
	require.False(t, lastGetPricingTime.Before(before),
		"a failed refresh should still record the attempt so the next call does not retry immediately")
}

// createRootAccountIfNeed 忽略 DB.Create 的错误：建 root 用户失败时它仍返回
// nil，初始化流程以为账号已就绪，之后所有 root 登录都会失败且没有任何线索。
func TestCreateRootAccountIfNeedPropagatesCreateError(t *testing.T) {
	db := setupPricingEndpointTypesTestDB(t)
	// 没有 users 表：First 报错，代码会进入建号分支，Create 也必然失败。
	require.NoError(t, db.Migrator().DropTable(&User{}))

	err := createRootAccountIfNeed()
	require.Error(t, err, "a failed root account creation must not be reported as success")
}

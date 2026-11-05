package model

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestNormalizeRouteWeightsUsesLargestRemainderAndPreservesZero(t *testing.T) {
	got := NormalizeRouteWeights([]uint64{1, 1, 0})
	require.Equal(t, []uint{5000, 5000, 0}, got)
	require.Equal(t, uint(RouteWeightTotalBPS), sumRouteWeights(got))
}

func TestNormalizeRouteWeightsDoesNotOverflowUint64Numerators(t *testing.T) {
	got := NormalizeRouteWeights([]uint64{^uint64(0), 1})
	require.Equal(t, uint(RouteWeightTotalBPS), sumRouteWeights(got))
	require.Greater(t, got[0], uint(9999))
	require.Equal(t, uint(0), got[1])

	legacy := legacyVotesToBPS([]uint{^uint(0), 0})
	require.Equal(t, uint(RouteWeightTotalBPS), sumRouteWeights(legacy))
}

func TestRouteScopeMemberHashIncludesMemberIdentityAndCapacity(t *testing.T) {
	base := ChannelRouteScopeItem{
		ChannelID:      1,
		ChannelName:    "channel",
		ChannelType:    2,
		ChannelStatus:  common.ChannelStatusEnabled,
		AbilityEnabled: true,
		RoutePriority:  1,
		RouteWeightBPS: 10000,
		MaxConcurrency: 3,
		sourceModels:   []string{"raw-model"},
	}
	nameChanged := base
	nameChanged.ChannelName = "renamed"
	capacityChanged := base
	capacityChanged.MaxConcurrency = 4
	rawModelChanged := base
	rawModelChanged.sourceModels = []string{"other-model"}
	require.NotEqual(t, routeScopeMemberHash([]ChannelRouteScopeItem{base}), routeScopeMemberHash([]ChannelRouteScopeItem{nameChanged}))
	require.NotEqual(t, routeScopeMemberHash([]ChannelRouteScopeItem{base}), routeScopeMemberHash([]ChannelRouteScopeItem{capacityChanged}))
	require.NotEqual(t, routeScopeMemberHash([]ChannelRouteScopeItem{base}), routeScopeMemberHash([]ChannelRouteScopeItem{rawModelChanged}))
}

func TestRouteScopeIDRoundTripsLegacyNegativePriority(t *testing.T) {
	id := EncodeRouteScopeID("default", "legacy-model", -1)
	group, modelName, priority, err := DecodeRouteScopeID(id)
	require.NoError(t, err)
	require.Equal(t, "default", group)
	require.Equal(t, "legacy-model", modelName)
	require.Equal(t, int64(-1), priority)
}

func TestAdjustRouteWeightsEqualWaterFilling(t *testing.T) {
	got, err := AdjustRouteWeights([]uint{4000, 500, 5500}, 0, 9000, "equal_adjust", nil)
	require.NoError(t, err)
	require.Equal(t, []uint{9000, 0, 1000}, got)
}

func TestAdjustRouteWeightsEqualWaterFillingDoesNotDuplicateParticipants(t *testing.T) {
	// The first pass zeros the two small participants. The remaining delta
	// must be taken entirely from the last participant, not from a duplicated
	// entry caused by reusing indexes' backing array.
	got, err := AdjustRouteWeights([]uint{5000, 100, 100, 4800}, 0, 9000, "equal_adjust", nil)
	require.NoError(t, err)
	require.Equal(t, []uint{9000, 0, 0, 1000}, got)
}

func TestAdjustRouteWeightsRejectsLockedOnlyAdjustment(t *testing.T) {
	_, err := AdjustRouteWeights([]uint{5000, 5000}, 0, 6000, "equal_adjust", []bool{false, true})
	require.Error(t, err)
	_, err = AdjustRouteWeights([]uint{5000, 5000}, 0, 6000, "equal_adjust", []bool{true, false})
	require.Error(t, err)
}

func TestRouteScopeQueriesUseDatabaseSpecificGroupQuoting(t *testing.T) {
	previousDB, previousLogDB := DB, LOG_DB
	previousSQLite, previousPostgres := common.UsingSQLite, common.UsingPostgreSQL
	t.Cleanup(func() {
		DB, LOG_DB = previousDB, previousLogDB
		common.UsingSQLite, common.UsingPostgreSQL = previousSQLite, previousPostgres
		initCol()
	})

	common.UsingSQLite = true
	common.UsingPostgreSQL = false
	db := openTestDB(t, &Channel{}, &Ability{})
	DB, LOG_DB = db, db
	initCol()

	priority := int64(7)
	weightA, weightB := uint(6000), uint(4000)
	require.NoError(t, db.Create(&Channel{Id: 90101, Name: "scope-a", Status: common.ChannelStatusEnabled}).Error)
	require.NoError(t, db.Create(&Channel{Id: 90102, Name: "scope-b", Status: common.ChannelStatusEnabled}).Error)
	require.NoError(t, db.Create(&Ability{Group: "default", Model: "gpt-scope", ChannelId: 90101, Enabled: true, RoutePriority: &priority, RouteWeightBPS: &weightA}).Error)
	require.NoError(t, db.Create(&Ability{Group: "default", Model: "gpt-scope", ChannelId: 90102, Enabled: true, RoutePriority: &priority, RouteWeightBPS: &weightB}).Error)

	scopeID := EncodeRouteScopeID("default", "gpt-scope", priority)
	scope, err := GetChannelRouteScope(scopeID)
	require.NoError(t, err)
	require.Equal(t, uint(RouteWeightTotalBPS), scope.TotalWeightBPS)
	require.Len(t, scope.Items, 2)

	scopes, err := ListChannelRouteScopes("default", "gpt-scope")
	require.NoError(t, err)
	require.Len(t, scopes, 1)
}

func TestUpdateChannelRouteScopeRejectsStaleVersion(t *testing.T) {
	previousDB, previousLogDB := DB, LOG_DB
	previousSQLite, previousPostgres := common.UsingSQLite, common.UsingPostgreSQL
	t.Cleanup(func() {
		DB, LOG_DB = previousDB, previousLogDB
		common.UsingSQLite, common.UsingPostgreSQL = previousSQLite, previousPostgres
		initCol()
	})

	common.UsingSQLite = true
	common.UsingPostgreSQL = false
	db := openTestDB(t, &Channel{}, &Ability{})
	DB, LOG_DB = db, db
	initCol()

	priority := int64(3)
	weight := uint(10000)
	require.NoError(t, db.Create(&Channel{Id: 90201, Name: "scope-version", Status: common.ChannelStatusEnabled}).Error)
	require.NoError(t, db.Create(&Ability{Group: "default", Model: "gpt-version", ChannelId: 90201, Enabled: true, RoutePriority: &priority, RouteWeightBPS: &weight}).Error)
	scopeID := EncodeRouteScopeID("default", "gpt-version", priority)
	scope, err := GetChannelRouteScope(scopeID)
	require.NoError(t, err)

	newWeight := uint(10000)
	_, err = UpdateChannelRouteScopeWeights(scopeID, ChannelRouteScopeWeightUpdate{
		Version: "stale-version",
		Items:   []ChannelRouteScopeWeightItem{{ChannelID: 90201, RouteWeightBPS: newWeight}},
	})
	require.ErrorIs(t, err, ErrRouteScopeVersionConflict)
	require.NotEqual(t, "", scope.Version)
}

func TestRouteScopeMappingUsesEffectiveModelButWritesRawAbility(t *testing.T) {
	previousDB, previousLogDB := DB, LOG_DB
	previousSQLite, previousPostgres := common.UsingSQLite, common.UsingPostgreSQL
	t.Cleanup(func() {
		DB, LOG_DB = previousDB, previousLogDB
		common.UsingSQLite, common.UsingPostgreSQL = previousSQLite, previousPostgres
		initCol()
	})

	common.UsingSQLite = true
	common.UsingPostgreSQL = false
	db := openTestDB(t, &Channel{}, &Ability{})
	DB, LOG_DB = db, db
	initCol()

	priority := int64(4)
	weight := uint(10000)
	mapping := `{"public-model":"provider-model"}`
	require.NoError(t, db.Create(&Channel{
		Id:           90301,
		Name:         "mapped-scope",
		Status:       common.ChannelStatusEnabled,
		ModelMapping: common.GetPointer(mapping),
	}).Error)
	require.NoError(t, db.Create(&Ability{
		Group:          "default",
		Model:          "public-model",
		ChannelId:      90301,
		Enabled:        true,
		Priority:       &priority,
		Weight:         7,
		RoutePriority:  &priority,
		RouteWeightBPS: &weight,
	}).Error)

	scopeID := EncodeRouteScopeID("default", "provider-model", priority)
	scope, err := GetChannelRouteScope(scopeID)
	require.NoError(t, err)
	require.Equal(t, "provider-model", scope.EffectiveRouteModel)
	require.Len(t, scope.Items, 1)

	updatedPriority := int64(9)
	updated, err := UpdateChannelRouteScopePriority(scopeID, ChannelRouteScopePriorityUpdate{
		Version:       scope.Version,
		RoutePriority: updatedPriority,
	})
	require.NoError(t, err)
	require.Equal(t, updatedPriority, updated.RoutePriority)

	var ability Ability
	require.NoError(t, db.Where(commonGroupCol+" = ? AND model = ? AND channel_id = ?", "default", "public-model", 90301).First(&ability).Error)
	require.NotNil(t, ability.RoutePriority)
	require.Equal(t, updatedPriority, *ability.RoutePriority)
	require.True(t, ability.RoutePriorityConfigured)
	require.True(t, ability.RouteWeightBPSConfigured)
}

func TestUpdateChannelRouteScopePriorityRejectsAllZeroMergedScope(t *testing.T) {
	previousDB, previousLogDB := DB, LOG_DB
	previousSQLite, previousPostgres := common.UsingSQLite, common.UsingPostgreSQL
	t.Cleanup(func() {
		DB, LOG_DB = previousDB, previousLogDB
		common.UsingSQLite, common.UsingPostgreSQL = previousSQLite, previousPostgres
		initCol()
	})

	common.UsingSQLite = true
	common.UsingPostgreSQL = false
	db := openTestDB(t, &Channel{}, &Ability{})
	DB, LOG_DB = db, db
	initCol()

	priority := int64(2)
	zeroWeight := uint(0)
	require.NoError(t, db.Create(&Channel{
		Id:     90312,
		Name:   "zero-weight-source",
		Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&Ability{
		Group:                    "default",
		Model:                    "zero-weight-model",
		ChannelId:                90312,
		Enabled:                  true,
		RoutePriority:            &priority,
		RouteWeightBPS:           &zeroWeight,
		RoutePriorityConfigured:  true,
		RouteWeightBPSConfigured: true,
	}).Error)

	scopeID := EncodeRouteScopeID("default", "zero-weight-model", priority)
	scope, err := GetChannelRouteScope(scopeID)
	require.NoError(t, err)
	require.Equal(t, uint(0), scope.TotalWeightBPS)

	_, err = UpdateChannelRouteScopePriority(scopeID, ChannelRouteScopePriorityUpdate{
		Version:       scope.Version,
		RoutePriority: 5,
	})
	require.ErrorIs(t, err, ErrRouteScopeNoPositiveWeight)

	var ability Ability
	require.NoError(t, db.Where(commonGroupCol+" = ? AND model = ? AND channel_id = ?", "default", "zero-weight-model", 90312).First(&ability).Error)
	require.NotNil(t, ability.RoutePriority)
	require.Equal(t, priority, *ability.RoutePriority)
	require.NotNil(t, ability.RouteWeightBPS)
	require.Zero(t, *ability.RouteWeightBPS)
}

func TestRouteScopeQueriesAndUpdatesIgnoreUnrelatedInvalidMapping(t *testing.T) {
	previousDB, previousLogDB := DB, LOG_DB
	previousSQLite, previousPostgres := common.UsingSQLite, common.UsingPostgreSQL
	t.Cleanup(func() {
		DB, LOG_DB = previousDB, previousLogDB
		common.UsingSQLite, common.UsingPostgreSQL = previousSQLite, previousPostgres
		initCol()
	})

	common.UsingSQLite = true
	common.UsingPostgreSQL = false
	db := openTestDB(t, &Channel{}, &Ability{})
	DB, LOG_DB = db, db
	initCol()

	priority := int64(6)
	weight := uint(RouteWeightTotalBPS)
	badMapping := `{"broken":`
	require.NoError(t, db.Create(&Channel{
		Id:           90308,
		Name:         "unrelated-invalid-mapping",
		Status:       common.ChannelStatusEnabled,
		ModelMapping: &badMapping,
	}).Error)
	require.NoError(t, db.Create(&Ability{
		Group:     "default",
		Model:     "broken",
		ChannelId: 90308,
		Enabled:   true,
		Priority:  &priority,
		Weight:    1,
	}).Error)
	require.NoError(t, db.Create(&Channel{
		Id:     90309,
		Name:   "valid-route-scope",
		Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&Ability{
		Group:                    "default",
		Model:                    "valid-route-model",
		ChannelId:                90309,
		Enabled:                  true,
		RoutePriority:            &priority,
		RouteWeightBPS:           &weight,
		RoutePriorityConfigured:  true,
		RouteWeightBPSConfigured: true,
	}).Error)

	scopes, err := ListChannelRouteScopes("default", "")
	require.NoError(t, err)
	var validScope *ChannelRouteScope
	for index := range scopes {
		if scopes[index].EffectiveRouteModel == "valid-route-model" {
			validScope = &scopes[index]
			break
		}
	}
	require.NotNil(t, validScope)
	require.Len(t, validScope.Items, 1)

	loaded, err := GetChannelRouteScope(validScope.ID)
	require.NoError(t, err)
	require.Equal(t, "valid-route-model", loaded.EffectiveRouteModel)

	updated, err := UpdateChannelRouteScopeWeights(validScope.ID, ChannelRouteScopeWeightUpdate{
		Version: validScope.Version,
		Items:   []ChannelRouteScopeWeightItem{{ChannelID: 90309, RouteWeightBPS: RouteWeightTotalBPS}},
	})
	require.NoError(t, err)
	require.Equal(t, uint(RouteWeightTotalBPS), updated.Items[0].RouteWeightBPS)

	report, err := PreviewRouteFieldsBackfill(db)
	require.NoError(t, err)
	require.NotEmpty(t, report.MappingConflicts)
	require.Equal(t, 1, report.ScopeCount)
	_, err = ApplyRouteFieldsBackfill(db)
	require.ErrorIs(t, err, ErrRouteScopeMigrationConflicts)
}

func TestEnsureRouteFieldsBackfilledUsesMappedEffectiveScopeAndRawWrite(t *testing.T) {
	previousDB, previousLogDB := DB, LOG_DB
	previousSQLite, previousPostgres := common.UsingSQLite, common.UsingPostgreSQL
	t.Cleanup(func() {
		DB, LOG_DB = previousDB, previousLogDB
		common.UsingSQLite, common.UsingPostgreSQL = previousSQLite, previousPostgres
		initCol()
	})

	common.UsingSQLite = true
	common.UsingPostgreSQL = false
	db := openTestDB(t, &Channel{}, &Ability{})
	DB, LOG_DB = db, db
	initCol()

	priority := int64(2)
	require.NoError(t, db.Create(&Channel{
		Id:           90302,
		Name:         "mapped-backfill",
		Status:       common.ChannelStatusEnabled,
		ModelMapping: common.GetPointer(`{"public-model":"provider-model"}`),
	}).Error)
	require.NoError(t, db.Create(&Ability{
		Group:     "default",
		Model:     "public-model",
		ChannelId: 90302,
		Enabled:   true,
		Priority:  &priority,
		Weight:    0,
	}).Error)

	count, err := EnsureRouteFieldsBackfilled(db, false)
	require.NoError(t, err)
	require.Equal(t, 1, count)

	var ability Ability
	require.NoError(t, db.Where(commonGroupCol+" = ? AND model = ? AND channel_id = ?", "default", "public-model", 90302).First(&ability).Error)
	require.NotNil(t, ability.RouteWeightBPS)
	require.Equal(t, uint(RouteWeightTotalBPS), *ability.RouteWeightBPS)
	require.True(t, ability.RoutePriorityConfigured)
	require.True(t, ability.RouteWeightBPSConfigured)
}

func TestFixAbilityPreservesRouteScopeFields(t *testing.T) {
	previousDB, previousLogDB := DB, LOG_DB
	previousSQLite, previousPostgres := common.UsingSQLite, common.UsingPostgreSQL
	t.Cleanup(func() {
		DB, LOG_DB = previousDB, previousLogDB
		common.UsingSQLite, common.UsingPostgreSQL = previousSQLite, previousPostgres
		initCol()
	})

	common.UsingSQLite = true
	common.UsingPostgreSQL = false
	db := openTestDB(t, &Channel{}, &Ability{})
	DB, LOG_DB = db, db
	initCol()

	legacyPriority := int64(2)
	routePriority := int64(8)
	legacyWeight := uint(3)
	routeWeight := uint(RouteWeightTotalBPS)
	channel := &Channel{
		Id:       90303,
		Name:     "repair-preserves-route",
		Models:   "mapped-model",
		Group:    "default",
		Status:   common.ChannelStatusEnabled,
		Priority: &legacyPriority,
		Weight:   &legacyWeight,
	}
	require.NoError(t, db.Create(channel).Error)
	require.NoError(t, db.Create(&Ability{
		Group:                    "default",
		Model:                    "mapped-model",
		ChannelId:                channel.Id,
		Enabled:                  true,
		Priority:                 &legacyPriority,
		Weight:                   legacyWeight,
		RoutePriority:            &routePriority,
		RouteWeightBPS:           &routeWeight,
		RoutePriorityConfigured:  true,
		RouteWeightBPSConfigured: true,
	}).Error)

	success, failed, err := FixAbility()
	require.NoError(t, err)
	require.Equal(t, 1, success)
	require.Equal(t, 0, failed)

	var loaded Ability
	require.NoError(t, db.Where(commonGroupCol+" = ? AND model = ? AND channel_id = ?", "default", "mapped-model", channel.Id).First(&loaded).Error)
	require.NotNil(t, loaded.RoutePriority)
	require.Equal(t, routePriority, *loaded.RoutePriority)
	require.NotNil(t, loaded.RouteWeightBPS)
	require.Equal(t, routeWeight, *loaded.RouteWeightBPS)
	require.True(t, loaded.RoutePriorityConfigured)
	require.True(t, loaded.RouteWeightBPSConfigured)
}

func TestEnsureRouteFieldsBackfilledAggregatesMultipleRawAbilitiesPerChannel(t *testing.T) {
	previousDB, previousLogDB := DB, LOG_DB
	previousSQLite, previousPostgres := common.UsingSQLite, common.UsingPostgreSQL
	t.Cleanup(func() {
		DB, LOG_DB = previousDB, previousLogDB
		common.UsingSQLite, common.UsingPostgreSQL = previousSQLite, previousPostgres
		initCol()
	})

	common.UsingSQLite = true
	common.UsingPostgreSQL = false
	db := openTestDB(t, &Channel{}, &Ability{})
	DB, LOG_DB = db, db
	initCol()

	priority := int64(2)
	require.NoError(t, db.Create(&Channel{
		Id:           90303,
		Name:         "mapped-multi-raw",
		Status:       common.ChannelStatusEnabled,
		ModelMapping: common.GetPointer(`{"raw-a":"provider-model","raw-b":"provider-model"}`),
	}).Error)
	require.NoError(t, db.Create(&Channel{
		Id:     90304,
		Name:   "mapped-peer",
		Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&Ability{Group: "default", Model: "raw-a", ChannelId: 90303, Enabled: true, Priority: &priority, Weight: 90}).Error)
	require.NoError(t, db.Create(&Ability{Group: "default", Model: "raw-b", ChannelId: 90303, Enabled: true, Priority: &priority, Weight: 10}).Error)
	require.NoError(t, db.Create(&Ability{Group: "default", Model: "provider-model", ChannelId: 90304, Enabled: true, Priority: &priority, Weight: 100}).Error)

	report, err := PreviewRouteFieldsBackfill(db)
	require.NoError(t, err)
	require.Equal(t, 1, report.ScopeCount)
	require.Equal(t, 2, report.Scopes[0].CandidateCount)
	require.Equal(t, 2, report.Scopes[0].Items[0].RawAbilityCount)
	require.Equal(t, 3, report.AbilitiesToBackfill)
	require.Equal(t, uint(RouteWeightTotalBPS), report.Scopes[0].Items[0].NewRouteWeightBPS+report.Scopes[0].Items[1].NewRouteWeightBPS)

	count, err := EnsureRouteFieldsBackfilled(db, false)
	require.NoError(t, err)
	require.Equal(t, 3, count)
	var rawAbilities []Ability
	require.NoError(t, db.Where("channel_id = ?", 90303).Order("model ASC").Find(&rawAbilities).Error)
	require.Len(t, rawAbilities, 2)
	require.NotNil(t, rawAbilities[0].RouteWeightBPS)
	require.NotNil(t, rawAbilities[1].RouteWeightBPS)
	require.Equal(t, *rawAbilities[0].RouteWeightBPS, *rawAbilities[1].RouteWeightBPS)
	require.Equal(t, priority, *rawAbilities[0].RoutePriority)
	require.Equal(t, priority, *rawAbilities[1].RoutePriority)
}

func TestChannelRoutingStrictWritesDefaultsToV2State(t *testing.T) {
	t.Setenv("CHANNEL_ROUTING_V2_ENABLED", "true")
	t.Setenv("CHANNEL_ROUTING_V2_STRICT_WRITES", "")
	require.True(t, IsChannelRoutingV2StrictWrites())

	t.Setenv("CHANNEL_ROUTING_V2_STRICT_WRITES", "false")
	require.False(t, IsChannelRoutingV2StrictWrites())

	t.Setenv("CHANNEL_ROUTING_V2_ENABLED", "false")
	t.Setenv("CHANNEL_ROUTING_V2_STRICT_WRITES", "")
	require.False(t, IsChannelRoutingV2StrictWrites())
}

func TestPreviewRouteFieldsBackfillReportsExplicitWeightTotalConflict(t *testing.T) {
	previousDB, previousLogDB := DB, LOG_DB
	previousSQLite, previousPostgres := common.UsingSQLite, common.UsingPostgreSQL
	t.Cleanup(func() {
		DB, LOG_DB = previousDB, previousLogDB
		common.UsingSQLite, common.UsingPostgreSQL = previousSQLite, previousPostgres
		initCol()
	})

	common.UsingSQLite = true
	common.UsingPostgreSQL = false
	db := openTestDB(t, &Channel{}, &Ability{})
	DB, LOG_DB = db, db
	initCol()

	priority := int64(1)
	weightA, weightB := uint(3000), uint(3000)
	require.NoError(t, db.Create(&Channel{Id: 90305, Name: "conflict-a", Status: common.ChannelStatusEnabled}).Error)
	require.NoError(t, db.Create(&Channel{Id: 90306, Name: "conflict-b", Status: common.ChannelStatusEnabled}).Error)
	require.NoError(t, db.Create(&Ability{Group: "default", Model: "conflict-model", ChannelId: 90305, Enabled: true, RoutePriority: &priority, RouteWeightBPS: &weightA}).Error)
	require.NoError(t, db.Create(&Ability{Group: "default", Model: "conflict-model", ChannelId: 90306, Enabled: true, RoutePriority: &priority, RouteWeightBPS: &weightB}).Error)

	report, err := PreviewRouteFieldsBackfill(db)
	require.NoError(t, err)
	require.Equal(t, 1, report.ScopeCount)
	require.Equal(t, 1, report.SkippedScopes)
	require.NotEmpty(t, report.WeightConflicts)
	require.Contains(t, report.Scopes[0].Error, "10000")
}

func TestPreviewRouteFieldsBackfillReportsConflictingRawWeightsInCompletedScope(t *testing.T) {
	previousDB, previousLogDB := DB, LOG_DB
	previousSQLite, previousPostgres := common.UsingSQLite, common.UsingPostgreSQL
	t.Cleanup(func() {
		DB, LOG_DB = previousDB, previousLogDB
		common.UsingSQLite, common.UsingPostgreSQL = previousSQLite, previousPostgres
		initCol()
	})

	common.UsingSQLite = true
	common.UsingPostgreSQL = false
	db := openTestDB(t, &Channel{}, &Ability{})
	DB, LOG_DB = db, db
	initCol()

	priority := int64(3)
	weightA, weightB := uint(6000), uint(4000)
	mapping := `{"raw-a":"provider-model","raw-b":"provider-model"}`
	require.NoError(t, db.Create(&Channel{Id: 90307, Name: "completed-conflict", Status: common.ChannelStatusEnabled, ModelMapping: &mapping}).Error)
	for _, ability := range []Ability{
		{
			Group:                    "default",
			Model:                    "raw-a",
			ChannelId:                90307,
			Enabled:                  true,
			RoutePriority:            &priority,
			RouteWeightBPS:           &weightA,
			RoutePriorityConfigured:  true,
			RouteWeightBPSConfigured: true,
		},
		{
			Group:                    "default",
			Model:                    "raw-b",
			ChannelId:                90307,
			Enabled:                  true,
			RoutePriority:            &priority,
			RouteWeightBPS:           &weightB,
			RoutePriorityConfigured:  true,
			RouteWeightBPSConfigured: true,
		},
	} {
		require.NoError(t, db.Create(&ability).Error)
	}

	report, err := PreviewRouteFieldsBackfill(db)
	require.NoError(t, err)
	require.Equal(t, 1, report.ScopeCount)
	require.Equal(t, 1, report.SkippedScopes)
	require.NotEmpty(t, report.WeightConflicts)
	require.True(t, report.Scopes[0].Skipped)
	require.Contains(t, report.Scopes[0].Error, "manual resolution")

	_, err = ApplyRouteFieldsBackfill(db)
	require.ErrorIs(t, err, ErrRouteScopeMigrationConflicts)
}

func TestPreviewRouteFieldsBackfillCountsZeroBPSInCompletedScope(t *testing.T) {
	previousDB, previousLogDB := DB, LOG_DB
	previousSQLite, previousPostgres := common.UsingSQLite, common.UsingPostgreSQL
	t.Cleanup(func() {
		DB, LOG_DB = previousDB, previousLogDB
		common.UsingSQLite, common.UsingPostgreSQL = previousSQLite, previousPostgres
		initCol()
	})

	common.UsingSQLite = true
	common.UsingPostgreSQL = false
	db := openTestDB(t, &Channel{}, &Ability{})
	DB, LOG_DB = db, db
	initCol()

	priority := int64(5)
	zeroWeight := uint(0)
	fullWeight := uint(RouteWeightTotalBPS)
	for _, channel := range []Channel{
		{Id: 90310, Name: "completed-zero", Status: common.ChannelStatusEnabled},
		{Id: 90311, Name: "completed-full", Status: common.ChannelStatusEnabled},
	} {
		require.NoError(t, db.Create(&channel).Error)
	}
	for _, ability := range []Ability{
		{
			Group:                    "default",
			Model:                    "completed-zero-model",
			ChannelId:                90310,
			Enabled:                  true,
			RoutePriority:            &priority,
			RouteWeightBPS:           &zeroWeight,
			RoutePriorityConfigured:  true,
			RouteWeightBPSConfigured: true,
		},
		{
			Group:                    "default",
			Model:                    "completed-zero-model",
			ChannelId:                90311,
			Enabled:                  true,
			RoutePriority:            &priority,
			RouteWeightBPS:           &fullWeight,
			RoutePriorityConfigured:  true,
			RouteWeightBPSConfigured: true,
		},
	} {
		require.NoError(t, db.Create(&ability).Error)
	}

	report, err := PreviewRouteFieldsBackfill(db)
	require.NoError(t, err)
	require.Equal(t, 1, report.ScopeCount)
	require.Equal(t, 0, report.SkippedScopes)
	require.Equal(t, 1, report.ZeroBPSCount)
	require.Equal(t, uint(0), report.Scopes[0].Items[0].NewRouteWeightBPS)
	require.Equal(t, uint(RouteWeightTotalBPS), report.Scopes[0].Items[1].NewRouteWeightBPS)
}

func TestApplyRouteFieldsBackfillRollsBackWhenLaterAbilityUpdateFails(t *testing.T) {
	previousDB, previousLogDB := DB, LOG_DB
	previousSQLite, previousPostgres := common.UsingSQLite, common.UsingPostgreSQL
	t.Cleanup(func() {
		DB, LOG_DB = previousDB, previousLogDB
		common.UsingSQLite, common.UsingPostgreSQL = previousSQLite, previousPostgres
		initCol()
	})

	common.UsingSQLite = true
	common.UsingPostgreSQL = false
	db := openTestDB(t, &Channel{}, &Ability{})
	DB, LOG_DB = db, db
	initCol()

	priority := int64(1)
	require.NoError(t, db.Create(&Channel{Id: 90308, Name: "alpha", Status: common.ChannelStatusEnabled}).Error)
	require.NoError(t, db.Create(&Channel{Id: 90309, Name: "beta", Status: common.ChannelStatusEnabled}).Error)
	require.NoError(t, db.Create(&Ability{Group: "default", Model: "alpha-model", ChannelId: 90308, Enabled: true, Priority: &priority, Weight: 1}).Error)
	require.NoError(t, db.Create(&Ability{Group: "default", Model: "beta-model", ChannelId: 90309, Enabled: true, Priority: &priority, Weight: 1}).Error)

	var updates int
	const callbackName = "test:route_scope_backfill_update_failure"
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Schema != nil && tx.Statement.Schema.Name == "Ability" {
			updates++
			if updates >= 2 {
				tx.AddError(errors.New("forced route backfill update failure"))
			}
		}
	}))
	t.Cleanup(func() {
		_ = db.Callback().Update().Remove(callbackName)
	})

	report, err := ApplyRouteFieldsBackfill(db)
	require.Error(t, err)
	require.Contains(t, report.Errors, "forced route backfill update failure")
	require.GreaterOrEqual(t, updates, 2)

	var abilities []Ability
	require.NoError(t, db.Where("channel_id IN ?", []int{90308, 90309}).Order("channel_id ASC").Find(&abilities).Error)
	require.Len(t, abilities, 2)
	for _, ability := range abilities {
		require.NotNil(t, ability.RoutePriority)
		require.Zero(t, *ability.RoutePriority)
		require.NotNil(t, ability.RouteWeightBPS)
		require.Zero(t, *ability.RouteWeightBPS)
		require.False(t, ability.RoutePriorityConfigured)
		require.False(t, ability.RouteWeightBPSConfigured)
	}
}

// 列表与迁移必须用同一个 legacy 公式核对同一 scope 的份额。旧式加权选择是
// 按渠道投票：sum(weight_i + 10)，所以 N 个候选里每个渠道都带 +10 的基础票；
// 迁移按 ability 累加 (weight + 10)，同一渠道映射多个 raw ability 时凭空多出
// 基础票，预览结果与 scope 列表不一致。
func TestLegacyScopeWeightsMatchChannelVoteFormulaAcrossListAndMigration(t *testing.T) {
	previousDB, previousLogDB := DB, LOG_DB
	previousSQLite, previousPostgres := common.UsingSQLite, common.UsingPostgreSQL
	t.Cleanup(func() {
		DB, LOG_DB = previousDB, previousLogDB
		common.UsingSQLite, common.UsingPostgreSQL = previousSQLite, previousPostgres
		initCol()
	})

	common.UsingSQLite = true
	common.UsingPostgreSQL = false
	db := openTestDB(t, &Channel{}, &Ability{})
	DB, LOG_DB = db, db
	initCol()

	priority := int64(2)
	require.NoError(t, db.Create(&Channel{
		Id:           90401,
		Name:         "legacy-multi-raw",
		Status:       common.ChannelStatusEnabled,
		ModelMapping: common.GetPointer(`{"raw-a":"provider-model","raw-b":"provider-model"}`),
	}).Error)
	require.NoError(t, db.Create(&Channel{Id: 90402, Name: "legacy-peer", Status: common.ChannelStatusEnabled}).Error)
	require.NoError(t, db.Create(&Ability{Group: "default", Model: "raw-a", ChannelId: 90401, Enabled: true, Priority: &priority, Weight: 90}).Error)
	require.NoError(t, db.Create(&Ability{Group: "default", Model: "raw-b", ChannelId: 90401, Enabled: true, Priority: &priority, Weight: 10}).Error)
	require.NoError(t, db.Create(&Ability{Group: "default", Model: "provider-model", ChannelId: 90402, Enabled: true, Priority: &priority, Weight: 100}).Error)

	// 旧式选择：两个渠道各投 weight_i + 10 票。
	legacyTotal := 90 + 10 + 100 + 2*10
	expectedChannelOne := uint(100+10) * uint(RouteWeightTotalBPS) / uint(legacyTotal)

	scopes, err := buildRouteScopesFromAbilities(mustLoadAllAbilities(t, db))
	require.NoError(t, err)
	require.Len(t, scopes, 1)
	require.Equal(t, 2, len(scopes[0].Items))
	listedBPS := uint(0)
	for _, item := range scopes[0].Items {
		if item.ChannelID == 90401 {
			listedBPS = item.RouteWeightBPS
		}
	}
	require.Equal(t, expectedChannelOne, listedBPS, "列表份额应按渠道投票计算")

	report, err := PreviewRouteFieldsBackfill(db)
	require.NoError(t, err)
	require.Len(t, report.Scopes, 1)
	require.Equal(t, uint64(100+10), report.Scopes[0].Items[0].LegacyVotes, "迁移的 LegacyVotes 必须是渠道票数，不是按 ability 累加的基础票")
	require.Equal(t, expectedChannelOne, report.Scopes[0].Items[0].NewRouteWeightBPS)
	require.Equal(t, uint(RouteWeightTotalBPS)-expectedChannelOne, report.Scopes[0].Items[1].NewRouteWeightBPS)
}

func mustLoadAllAbilities(t *testing.T, db *gorm.DB) []Ability {
	t.Helper()
	var abilities []Ability
	require.NoError(t, db.Order("channel_id ASC, model ASC").Find(&abilities).Error)
	return abilities
}

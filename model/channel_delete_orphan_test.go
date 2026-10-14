package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// A leftover ability row is not harmless bookkeeping. AddAbilities inserts with
// OnConflict DoNothing against the (group, model, channel_id) unique index, so a
// row that outlives its channel makes the insert for a later channel reusing the
// same id silently do nothing. That channel then has no enabled ability and can
// never be routed to, which is why every delete path must remove both sides.
func TestDeleteChannelRemovesAbilitiesAtomically(t *testing.T) {
	prepareChannelCacheTest(t)

	const id = 90911
	tag := "delete-orphan"
	priority := int64(10)
	require.NoError(t, DB.Create(&Channel{
		Id: id, Name: "delete-orphan", Status: common.ChannelStatusEnabled,
		Group: "default", Models: "delete-orphan-model", Tag: &tag, Priority: &priority,
		Weight: common.GetPointer(uint(1)),
	}).Error)
	require.NoError(t, DB.Create(&Ability{
		Group: "default", Model: "delete-orphan-model", ChannelId: id,
		Enabled: true, Priority: &priority, Weight: 1, Tag: &tag,
	}).Error)

	channel := &Channel{Id: id}
	require.NoError(t, channel.Delete())

	var channelCount int64
	require.NoError(t, DB.Model(&Channel{}).Where("id = ?", id).Count(&channelCount).Error)
	require.Equal(t, int64(0), channelCount)

	var abilityCount int64
	require.NoError(t, DB.Model(&Ability{}).Where("channel_id = ?", id).Count(&abilityCount).Error)
	require.Equal(t, int64(0), abilityCount, "the channel's abilities must not outlive it")
}

func TestDeleteDisabledChannelRemovesAbilities(t *testing.T) {
	prepareChannelCacheTest(t)

	tag := "delete-disabled-orphan"
	priority := int64(10)
	ids := []int{90912, 90913}
	for _, id := range ids {
		require.NoError(t, DB.Create(&Channel{
			Id: id, Name: "delete-disabled-orphan",
			Status: common.ChannelStatusManuallyDisabled,
			Group:  "default", Models: "delete-disabled-orphan-model",
			Tag: &tag, Priority: &priority, Weight: common.GetPointer(uint(1)),
		}).Error)
		require.NoError(t, DB.Create(&Ability{
			Group: "default", Model: "delete-disabled-orphan-model", ChannelId: id,
			Enabled: true, Priority: &priority, Weight: 1, Tag: &tag,
		}).Error)
	}

	deleted, err := DeleteDisabledChannel()
	require.NoError(t, err)
	require.Equal(t, int64(len(ids)), deleted)

	for _, id := range ids {
		var abilityCount int64
		require.NoError(t, DB.Model(&Ability{}).Where("channel_id = ?", id).Count(&abilityCount).Error)
		require.Equal(t, int64(0), abilityCount,
			"channel %d was deleted but its abilities were left behind", id)
	}
}

// The recreated-channel scenario end to end: after a disabled channel is
// removed, a channel that reuses its id must get a working, enabled ability.
func TestRecreatedChannelAfterDisabledDeleteGetsEnabledAbility(t *testing.T) {
	prepareChannelCacheTest(t)

	const id = 90914
	tag := "delete-recreate"
	priority := int64(10)
	require.NoError(t, DB.Create(&Channel{
		Id: id, Name: "delete-recreate", Status: common.ChannelStatusManuallyDisabled,
		Group: "default", Models: "delete-recreate-model", Tag: &tag, Priority: &priority,
		Weight: common.GetPointer(uint(1)),
	}).Error)
	// A disabled ability row: this is the shape that wins the conflict and makes
	// a recreated channel unroutable when it is left behind.
	require.NoError(t, DB.Create(&Ability{
		Group: "default", Model: "delete-recreate-model", ChannelId: id,
		Enabled: false, Priority: &priority, Weight: 1, Tag: &tag,
	}).Error)

	_, err := DeleteDisabledChannel()
	require.NoError(t, err)

	recreated := &Channel{
		Id: id, Name: "delete-recreate-2", Status: common.ChannelStatusEnabled,
		Group: "default", Models: "delete-recreate-model", Tag: &tag, Priority: &priority,
		Weight: common.GetPointer(uint(1)),
	}
	require.NoError(t, DB.Create(recreated).Error)
	require.NoError(t, recreated.AddAbilities(nil))

	var abilities []Ability
	require.NoError(t, DB.Where("channel_id = ?", id).Find(&abilities).Error)
	require.Len(t, abilities, 1)
	require.True(t, abilities[0].Enabled,
		"the recreated channel has no enabled ability, so it can never be routed to")
}

// The happy path above passes whether or not the two deletes share a
// transaction, because both statements succeed. The transaction only earns its
// keep when the second one fails: without it the channel row is already gone and
// the ability row is orphaned, which is the state that later makes a channel
// reusing the id unroutable.
func TestDeleteChannelRollsBackWhenAbilityDeleteFails(t *testing.T) {
	prepareChannelCacheTest(t)

	const id = 90915
	tag := "delete-rollback"
	priority := int64(10)
	require.NoError(t, DB.Create(&Channel{
		Id: id, Name: "delete-rollback", Status: common.ChannelStatusEnabled,
		Group: "default", Models: "delete-rollback-model", Tag: &tag, Priority: &priority,
		Weight: common.GetPointer(uint(1)),
	}).Error)
	require.NoError(t, DB.Create(&Ability{
		Group: "default", Model: "delete-rollback-model", ChannelId: id,
		Enabled: true, Priority: &priority, Weight: 1, Tag: &tag,
	}).Error)

	const callbackName = "test:fail_ability_delete"
	require.NoError(t, DB.Callback().Delete().Before("gorm:delete").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Table == "abilities" {
			tx.AddError(gorm.ErrInvalidTransaction)
		}
	}))
	t.Cleanup(func() { _ = DB.Callback().Delete().Remove(callbackName) })

	channel := &Channel{Id: id}
	require.Error(t, channel.Delete(), "the injected ability-delete failure must surface")

	var channelCount, abilityCount int64
	require.NoError(t, DB.Model(&Channel{}).Where("id = ?", id).Count(&channelCount).Error)
	require.NoError(t, DB.Model(&Ability{}).Where("channel_id = ?", id).Count(&abilityCount).Error)
	require.Equal(t, int64(1), channelCount,
		"the channel row was deleted although its abilities could not be removed")
	require.Equal(t, int64(1), abilityCount,
		"the ability row must survive the rolled-back delete")
}

// The shared chunked delete splits ids at 200. A batch larger than one chunk
// must still remove every ability row, and must not stop after the first chunk.
func TestDeleteChannelsByIdsHandlesMultipleChunks(t *testing.T) {
	prepareChannelCacheTest(t)

	const count = 205
	tag := "delete-chunked"
	priority := int64(10)
	ids := make([]int, 0, count)
	for i := 0; i < count; i++ {
		id := 92000 + i
		ids = append(ids, id)
		require.NoError(t, DB.Create(&Channel{
			Id: id, Name: "delete-chunked",
			Status: common.ChannelStatusManuallyDisabled,
			Group:  "default", Models: "delete-chunked-model",
			Tag: &tag, Priority: &priority, Weight: common.GetPointer(uint(1)),
		}).Error)
		require.NoError(t, DB.Create(&Ability{
			Group: "default", Model: "delete-chunked-model", ChannelId: id,
			Enabled: true, Priority: &priority, Weight: 1, Tag: &tag,
		}).Error)
	}

	deleted, err := DeleteDisabledChannel()
	require.NoError(t, err)
	require.Equal(t, int64(count), deleted)

	var channelCount, abilityCount int64
	require.NoError(t, DB.Model(&Channel{}).Where("id in (?)", ids).Count(&channelCount).Error)
	require.NoError(t, DB.Model(&Ability{}).Where("channel_id in (?)", ids).Count(&abilityCount).Error)
	require.Equal(t, int64(0), channelCount)
	require.Equal(t, int64(0), abilityCount,
		"a batch spanning several chunks left abilities behind")
}

// A leftover ability row must not silently produce a channel with no ability.
// AddAbilities inserts with OnConflict DoNothing, so before this check the
// conflict was invisible: the channel was created, its models looked configured,
// and it could never be routed to. Failing loudly also rolls the creation back,
// so the id is not left poisoned for the next attempt.
func TestInsertChannelFailsWhenAbilitiesConflictWithOrphans(t *testing.T) {
	prepareChannelCacheTest(t)

	const id = 90916
	tag := "insert-orphan-conflict"
	priority := int64(10)

	// An orphaned, disabled ability row occupying the key the new channel needs.
	require.NoError(t, DB.Create(&Ability{
		Group: "default", Model: "insert-orphan-model", ChannelId: id,
		Enabled: false, Priority: &priority, Weight: 1, Tag: &tag,
	}).Error)

	channel := &Channel{
		Id: id, Name: "insert-orphan-conflict", Status: common.ChannelStatusEnabled,
		Group: "default", Models: "insert-orphan-model",
		Tag: &tag, Priority: &priority, Weight: common.GetPointer(uint(1)),
	}
	err := InsertChannel(channel)
	require.Error(t, err, "a conflicting ability insert must not be silent")
	require.Contains(t, err.Error(), "conflict with rows left behind by a deleted channel")

	var channelCount int64
	require.NoError(t, DB.Model(&Channel{}).Where("id = ?", id).Count(&channelCount).Error)
	require.Equal(t, int64(0), channelCount,
		"the channel must not be committed when its abilities could not be inserted")
}

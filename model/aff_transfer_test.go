package model

import (
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// TransferAffQuotaToQuota must not write the whole row back from a snapshot:
// a credit committed in between would be overwritten by the stale `quota`
// value. It applies a relative update instead, which cannot clobber a
// concurrent change to either column.
//
// This pins the mechanism rather than racing two goroutines. The interleaving
// it protects against cannot be forced deterministically on SQLite (writes
// serialise, and holding a transaction open while writing from a second
// connection deadlocks the shared cache), so the assertion is on the emitted
// statement: relative expressions, never an absolute assignment.
func TestTransferAffQuotaToQuotaEmitsRelativeUpdate(t *testing.T) {
	db := openTestDB(t, &User{})
	previous := DB
	DB = db
	t.Cleanup(func() { DB = previous })

	user := &User{
		Username: "aff-relative-probe",
		AffQuota: 2 * int(common.QuotaPerUnit),
		Quota:    0,
	}
	require.NoError(t, db.Create(user).Error)

	// Capture the statement the production code actually issues.
	var updates []string
	require.NoError(t, db.Callback().Update().After("gorm:update").
		Register("test:capture_aff_transfer", func(tx *gorm.DB) {
			if tx.Statement != nil && tx.Statement.Table == "users" {
				updates = append(updates, tx.Statement.SQL.String())
			}
		}))

	transferUser := &User{Id: user.Id}
	require.NoError(t, transferUser.TransferAffQuotaToQuota(int(common.QuotaPerUnit)))

	require.Len(t, updates, 1, "expected exactly one UPDATE from the transfer")
	emitted := updates[0]
	t.Logf("emitted: %s", emitted)

	require.Contains(t, emitted, "aff_quota`=aff_quota - ",
		"aff_quota must be decremented relative to its stored value")
	require.Contains(t, emitted, "quota`=quota + ",
		"quota must be incremented relative to its stored value")
	require.Contains(t, emitted, "aff_quota >= ",
		"the statement must carry the aff_quota sufficiency guard")
	require.NotContains(t, emitted, "aff_quota`="+strconv.Itoa(user.AffQuota),
		"an absolute assignment would clobber a concurrent credit")

	// And the effect must be correct.
	var after User
	require.NoError(t, db.First(&after, user.Id).Error)
	require.Equal(t, int(common.QuotaPerUnit), after.Quota)
	require.Equal(t, int(common.QuotaPerUnit), after.AffQuota)
}

// The guard must also reject a transfer larger than the remaining aff quota
// even when the in-memory snapshot looks sufficient.
func TestTransferAffQuotaToQuotaRejectsOverdraw(t *testing.T) {
	db := openTestDB(t, &User{})
	previous := DB
	DB = db
	t.Cleanup(func() { DB = previous })

	user := &User{
		Username: "aff-overdraw-probe",
		AffQuota: int(common.QuotaPerUnit),
		Quota:    0,
	}
	require.NoError(t, db.Create(user).Error)

	// Drain aff_quota behind the caller's back.
	require.NoError(t, db.Model(&User{}).Where("id = ?", user.Id).
		Update("aff_quota", 0).Error)

	transferUser := &User{Id: user.Id}
	err := transferUser.TransferAffQuotaToQuota(int(common.QuotaPerUnit))
	require.Error(t, err, "transfer must fail when aff_quota is no longer sufficient")

	var after User
	require.NoError(t, db.First(&after, user.Id).Error)
	require.Equal(t, 0, after.Quota, "no quota may be credited on a rejected transfer")
}

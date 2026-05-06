package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetAllUnFinishTasksFiltersTerminalStatusAndLimits(t *testing.T) {
	db := openTestDB(t, &Midjourney{})
	previousDB := DB
	DB = db
	t.Cleanup(func() {
		DB = previousDB
	})

	tasks := []Midjourney{
		{Id: 1, MjId: "success-progress-stale", Progress: "50%", Status: "SUCCESS"},
		{Id: 2, MjId: "failure-progress-stale", Progress: "50%", Status: "FAILURE"},
		{Id: 3, MjId: "active-older", Progress: "10%", Status: "IN_PROGRESS"},
		{Id: 4, MjId: "active-newer", Progress: "20%", Status: "SUBMITTED"},
	}
	require.NoError(t, db.Create(&tasks).Error)

	result := GetAllUnFinishTasks(1)
	require.Len(t, result, 1)
	require.Equal(t, "active-older", result[0].MjId)
}

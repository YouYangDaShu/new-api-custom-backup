package model

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelMonitorPersistsChecksAndHistory(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&ChannelMonitor{}, &ChannelMonitorCheck{}))
	monitor := ChannelMonitor{Name: "test monitor", Group: "default", PrimaryModel: "gpt-test", IntervalSeconds: 60, Enabled: true}
	require.NoError(t, DB.Create(&monitor).Error)
	t.Cleanup(func() { DB.Where("monitor_id = ?", monitor.ID).Delete(&ChannelMonitorCheck{}); DB.Delete(&monitor) })
	now := time.Now().Unix()
	require.NoError(t, DB.Create(&ChannelMonitorCheck{MonitorID: monitor.ID, Model: monitor.PrimaryModel, Status: ChannelMonitorStatusSuccess, LatencyMS: 42, CheckedAt: now}).Error)
	rows, err := ListChannelMonitorChecks(monitor.ID, now-1, 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, int64(42), rows[0].LatencyMS)
}

func TestListChannelMonitorChecksForMonitorsLimitsEachModelSeries(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&ChannelMonitor{}, &ChannelMonitorCheck{}))
	monitors := []ChannelMonitor{
		{Name: "series monitor a", Group: "default", PrimaryModel: "model-a", IntervalSeconds: 300, Enabled: true},
		{Name: "series monitor b", Group: "default", PrimaryModel: "model-a", IntervalSeconds: 300, Enabled: true},
	}
	require.NoError(t, DB.Create(&monitors).Error)
	t.Cleanup(func() {
		ids := []int64{monitors[0].ID, monitors[1].ID}
		DB.Where("monitor_id IN ?", ids).Delete(&ChannelMonitorCheck{})
		DB.Where("id IN ?", ids).Delete(&ChannelMonitor{})
	})

	now := time.Now().Unix()
	seriesCounts := map[string]int{
		"a/model-a": 1701,
		"a/model-b": 1701,
		"b/model-a": 1701,
	}
	checks := make([]ChannelMonitorCheck, 0, 5103)
	for key, count := range seriesCounts {
		monitorID, modelName := monitors[0].ID, "model-a"
		switch key {
		case "a/model-b":
			modelName = "model-b"
		case "b/model-a":
			monitorID = monitors[1].ID
		}
		for i := 0; i < count; i++ {
			checks = append(checks, ChannelMonitorCheck{MonitorID: monitorID, Model: modelName, Status: ChannelMonitorStatusSuccess, CheckedAt: now - int64(i)})
		}
	}
	require.Greater(t, len(checks), 5000)
	require.NoError(t, DB.CreateInBatches(&checks, 500).Error)

	rows, err := ListChannelMonitorChecksForMonitors([]int64{monitors[0].ID, monitors[1].ID}, now-30*24*60*60, 2000)
	require.NoError(t, err)
	require.Len(t, rows, len(checks))
	got := make(map[string]int)
	for _, row := range rows {
		prefix := "a/"
		if row.MonitorID == monitors[1].ID {
			prefix = "b/"
		}
		got[prefix+row.Model]++
	}
	assert.Equal(t, seriesCounts, got)
}

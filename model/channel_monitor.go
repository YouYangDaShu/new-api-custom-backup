package model

import (
	"errors"
	"sort"
	"time"

	"gorm.io/gorm"
)

const (
	ChannelMonitorStatusSuccess = "success"
	ChannelMonitorStatusFailed  = "failed"
	ChannelMonitorStatusTimeout = "timeout"

	// The configured minimum interval is 30 seconds. This bound covers every
	// scheduled check in an inclusive 30-day window for one monitor/model pair.
	ChannelMonitorMaxChecksPerSeries = 30*24*60*60/30 + 1
)

// ChannelMonitor is an operator-owned probe definition. It intentionally
// stores only routing inputs; channel credentials and upstream details remain
// in Channel and are never serialized by the public API.
type ChannelMonitor struct {
	ID              int64  `json:"id" gorm:"primaryKey"`
	Name            string `json:"name" gorm:"type:varchar(128);not null"`
	Group           string `json:"group" gorm:"column:group_name;type:varchar(128);not null;index"`
	PrimaryModel    string `json:"primary_model" gorm:"type:varchar(128);not null"`
	ExtraModelsJSON string `json:"-" gorm:"type:text"`
	IntervalSeconds int    `json:"interval_seconds" gorm:"not null"`
	Enabled         bool   `json:"enabled" gorm:"not null"`
	SortOrder       int    `json:"sort_order" gorm:"not null;index"`
	LastCheckedAt   int64  `json:"last_checked_at" gorm:"index"`
	CreatedAt       int64  `json:"created_at"`
	UpdatedAt       int64  `json:"updated_at"`
}

func (ChannelMonitor) TableName() string { return "channel_monitors" }

type ChannelMonitorCheck struct {
	ID         int64  `json:"id" gorm:"primaryKey"`
	MonitorID  int64  `json:"monitor_id" gorm:"not null;index:idx_monitor_checks"`
	Model      string `json:"model" gorm:"type:varchar(128);not null;index:idx_monitor_checks"`
	Status     string `json:"status" gorm:"type:varchar(32);not null"`
	LatencyMS  int64  `json:"latency_ms"`
	ErrorCode  string `json:"error_code,omitempty" gorm:"type:varchar(64)"`
	ErrorBrief string `json:"error_brief,omitempty" gorm:"type:varchar(255)"`
	CheckedAt  int64  `json:"checked_at" gorm:"not null;index:idx_monitor_checks"`
}

func (ChannelMonitorCheck) TableName() string { return "channel_monitor_checks" }

func (m *ChannelMonitor) BeforeCreate(_ *gorm.DB) error {
	now := time.Now().Unix()
	if m.CreatedAt == 0 {
		m.CreatedAt = now
	}
	if m.UpdatedAt == 0 {
		m.UpdatedAt = now
	}
	return nil
}

func (m *ChannelMonitor) BeforeUpdate(_ *gorm.DB) error {
	m.UpdatedAt = time.Now().Unix()
	return nil
}

func GetChannelMonitor(id int64) (*ChannelMonitor, error) {
	if id <= 0 {
		return nil, errors.New("invalid monitor id")
	}
	var monitor ChannelMonitor
	if err := DB.First(&monitor, id).Error; err != nil {
		return nil, err
	}
	return &monitor, nil
}

func ListChannelMonitors() ([]ChannelMonitor, error) {
	var rows []ChannelMonitor
	err := DB.Order("sort_order asc, id asc").Find(&rows).Error
	return rows, err
}

func NextChannelMonitorSortOrder() int {
	var max int
	DB.Model(&ChannelMonitor{}).Select("COALESCE(MAX(sort_order), -1)").Scan(&max)
	return max + 1
}

func ListChannelMonitorChecks(monitorID int64, since int64, limit int) ([]ChannelMonitorCheck, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	q := DB.Where("monitor_id = ?", monitorID)
	if since > 0 {
		q = q.Where("checked_at >= ?", since)
	}
	var rows []ChannelMonitorCheck
	err := q.Order("checked_at desc").Limit(limit).Find(&rows).Error
	return rows, err
}

func ListChannelMonitorChecksForMonitors(monitorIDs []int64, since int64, limit int) ([]ChannelMonitorCheck, error) {
	if len(monitorIDs) == 0 {
		return []ChannelMonitorCheck{}, nil
	}
	if limit <= 0 || limit > ChannelMonitorMaxChecksPerSeries {
		limit = ChannelMonitorMaxChecksPerSeries
	}
	type monitorSeries struct {
		MonitorID int64
		Model     string
	}
	seriesQuery := DB.Model(&ChannelMonitorCheck{}).Select("monitor_id, model").Where("monitor_id IN ?", monitorIDs)
	if since > 0 {
		seriesQuery = seriesQuery.Where("checked_at >= ?", since)
	}
	var series []monitorSeries
	if err := seriesQuery.Group("monitor_id, model").Scan(&series).Error; err != nil {
		return nil, err
	}

	var rows []ChannelMonitorCheck
	for _, item := range series {
		q := DB.Where("monitor_id = ? AND model = ?", item.MonitorID, item.Model)
		if since > 0 {
			q = q.Where("checked_at >= ?", since)
		}
		var checks []ChannelMonitorCheck
		if err := q.Order("checked_at desc").Limit(limit).Find(&checks).Error; err != nil {
			return nil, err
		}
		rows = append(rows, checks...)
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].CheckedAt > rows[j].CheckedAt })
	return rows, nil
}

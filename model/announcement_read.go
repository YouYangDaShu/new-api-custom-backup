package model

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/console_setting"
)

// UserAnnouncementRead stores permanent per-user read state for system announcements/notice.
// Occupancy is small: one row per (user, announcement key), written only on confirm.
type UserAnnouncementRead struct {
	Id              int    `json:"id" gorm:"primaryKey;autoIncrement"`
	UserId          int    `json:"user_id" gorm:"not null;uniqueIndex:uk_user_announcement_key;index"`
	AnnouncementKey string `json:"announcement_key" gorm:"type:varchar(191);not null;uniqueIndex:uk_user_announcement_key"`
	ReadAt          int64  `json:"read_at" gorm:"not null;index"`
}

func AnnouncementKeyFromItem(item map[string]interface{}) string {
	if item == nil {
		return ""
	}
	if id, ok := item["id"]; ok && id != nil {
		switch v := id.(type) {
		case string:
			if v != "" {
				return "id:" + v
			}
		case float64:
			return fmt.Sprintf("id:%d", int64(v))
		case int:
			return fmt.Sprintf("id:%d", v)
		case int64:
			return fmt.Sprintf("id:%d", v)
		default:
			s := fmt.Sprint(v)
			if s != "" && s != "<nil>" {
				return "id:" + s
			}
		}
	}
	fingerprint := fmt.Sprintf("%v|%v|%v|%v|%v",
		item["publishDate"],
		item["content"],
		item["extra"],
		item["type"],
		item["title"],
	)
	sum := sha1.Sum([]byte(fingerprint))
	return "hash:" + hex.EncodeToString(sum[:])[:16]
}

func NoticeAnnouncementKey(notice string) string {
	notice = common.GetStringIfEmpty(notice, "")
	if notice == "" {
		return ""
	}
	sum := sha1.Sum([]byte(notice))
	return "notice:" + hex.EncodeToString(sum[:])[:16]
}

type AnnouncementDTO struct {
	Key         string `json:"key"`
	Type        string `json:"type,omitempty"`
	Title       string `json:"title,omitempty"`
	Content     string `json:"content"`
	Extra       string `json:"extra,omitempty"`
	PublishDate string `json:"publishDate,omitempty"`
	Source      string `json:"source"` // announcement | notice
}

func ListAllAnnouncementDTOs() []AnnouncementDTO {
	items := make([]AnnouncementDTO, 0, 32)

	// System notice (single long message) treated as one item.
	common.OptionMapRWMutex.RLock()
	notice := ""
	if common.OptionMap != nil {
		notice = strings.TrimSpace(common.OptionMap["Notice"])
	}
	common.OptionMapRWMutex.RUnlock()
	if notice != "" {
		key := NoticeAnnouncementKey(notice)
		if key != "" {
			items = append(items, AnnouncementDTO{
				Key:     key,
				Type:    "default",
				Title:   "系统通知",
				Content: notice,
				Source:  "notice",
			})
		}
	}

	if console_setting.GetConsoleSetting().AnnouncementsEnabled {
		for _, ann := range console_setting.GetAnnouncements() {
			content, _ := ann["content"].(string)
			if content == "" {
				continue
			}
			key := AnnouncementKeyFromItem(ann)
			if key == "" {
				continue
			}
			dto := AnnouncementDTO{
				Key:     key,
				Content: content,
				Source:  "announcement",
			}
			if t, ok := ann["type"].(string); ok {
				dto.Type = t
			}
			if title, ok := ann["title"].(string); ok {
				dto.Title = title
			}
			if extra, ok := ann["extra"].(string); ok {
				dto.Extra = extra
			}
			if pd, ok := ann["publishDate"].(string); ok {
				dto.PublishDate = pd
			}
			items = append(items, dto)
		}
	}

	// Newest first (notice without date stays first if present).
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Source == "notice" && items[j].Source != "notice" {
			return true
		}
		if items[j].Source == "notice" && items[i].Source != "notice" {
			return false
		}
		ti, errI := time.Parse(time.RFC3339, items[i].PublishDate)
		tj, errJ := time.Parse(time.RFC3339, items[j].PublishDate)
		if errI != nil && errJ != nil {
			return false
		}
		if errI != nil {
			return false
		}
		if errJ != nil {
			return true
		}
		return ti.After(tj)
	})

	// Cap forced queue size to avoid popup spam.
	if len(items) > 20 {
		items = items[:20]
	}
	return items
}

func GetReadAnnouncementKeys(userId int) (map[string]struct{}, error) {
	out := make(map[string]struct{})
	if userId <= 0 {
		return out, nil
	}
	var rows []UserAnnouncementRead
	err := DB.Select("announcement_key").Where("user_id = ?", userId).Find(&rows).Error
	if err != nil {
		return out, err
	}
	for _, r := range rows {
		out[r.AnnouncementKey] = struct{}{}
	}
	return out, nil
}

func GetUnreadAnnouncementsForUser(userId int) ([]AnnouncementDTO, error) {
	all := ListAllAnnouncementDTOs()
	if len(all) == 0 {
		return []AnnouncementDTO{}, nil
	}
	readSet, err := GetReadAnnouncementKeys(userId)
	if err != nil {
		return nil, err
	}
	unread := make([]AnnouncementDTO, 0, len(all))
	for _, item := range all {
		if _, ok := readSet[item.Key]; ok {
			continue
		}
		unread = append(unread, item)
	}
	return unread, nil
}

func MarkAnnouncementsRead(userId int, keys []string) error {
	if userId <= 0 || len(keys) == 0 {
		return nil
	}
	now := common.GetTimestamp()
	// Deduplicate keys.
	seen := make(map[string]struct{}, len(keys))
	uniq := make([]string, 0, len(keys))
	for _, k := range keys {
		if k == "" {
			continue
		}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		uniq = append(uniq, k)
	}
	if len(uniq) == 0 {
		return nil
	}

	// Avoid re-inserting already-read keys.
	readSet, err := GetReadAnnouncementKeys(userId)
	if err != nil {
		return err
	}
	rows := make([]UserAnnouncementRead, 0, len(uniq))
	for _, k := range uniq {
		if _, ok := readSet[k]; ok {
			continue
		}
		rows = append(rows, UserAnnouncementRead{
			UserId:          userId,
			AnnouncementKey: k,
			ReadAt:          now,
		})
	}
	if len(rows) == 0 {
		return nil
	}
	return DB.Create(&rows).Error
}

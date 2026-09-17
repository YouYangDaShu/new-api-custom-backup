package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

const (
	maxAnnouncementReadBodyBytes = 64 << 10
	maxAnnouncementReadKeys      = 128
)

// GetUnreadAnnouncements returns permanent-unread announcements for the current user.
func GetUnreadAnnouncements(c *gin.Context) {
	userId := c.GetInt("id")
	if userId <= 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "未登录"})
		return
	}
	items, err := model.GetUnreadAnnouncementsForUser(userId)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"items": items,
			"total": len(items),
		},
	})
}

type markAnnouncementsReadRequest struct {
	Keys []string `json:"keys"`
	All  bool     `json:"all"`
}

// MarkAnnouncementsRead permanently marks announcement keys as read for the current user.
func MarkAnnouncementsRead(c *gin.Context) {
	userId := c.GetInt("id")
	if userId <= 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "未登录"})
		return
	}
	var req markAnnouncementsReadRequest
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxAnnouncementReadBodyBytes)
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "参数错误"})
		return
	}

	if len(req.Keys) > maxAnnouncementReadKeys {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "公告数量超出限制"})
		return
	}

	keys := req.Keys
	if req.All {
		unread, err := model.GetUnreadAnnouncementsForUser(userId)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
			return
		}
		keys = make([]string, 0, len(unread))
		for _, item := range unread {
			keys = append(keys, item.Key)
		}
	} else {
		// Only accept keys for currently configured announcements.
		allowed := make(map[string]struct{})
		for _, item := range model.ListAllAnnouncementDTOs() {
			allowed[item.Key] = struct{}{}
		}
		filtered := make([]string, 0, len(keys))
		for _, key := range keys {
			if _, ok := allowed[key]; ok {
				filtered = append(filtered, key)
			}
		}
		keys = filtered
	}
	if len(keys) == 0 {
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": gin.H{"marked": 0}})
		return
	}
	marked, err := model.MarkAnnouncementsRead(userId, keys)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    gin.H{"marked": marked},
	})
}

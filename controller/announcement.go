package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
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
		common.ApiError(c, err)
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
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "参数错误"})
		return
	}

	keys := req.Keys
	if req.All {
		unread, err := model.GetUnreadAnnouncementsForUser(userId)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		keys = make([]string, 0, len(unread))
		for _, item := range unread {
			keys = append(keys, item.Key)
		}
	}
	if len(keys) == 0 {
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": gin.H{"marked": 0}})
		return
	}
	// Safety cap
	if len(keys) > 50 {
		keys = keys[:50]
	}
	if err := model.MarkAnnouncementsRead(userId, keys); err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    gin.H{"marked": len(keys)},
	})
}

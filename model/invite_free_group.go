package model

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const (
	InviteFreeGroupCodeStatusEnabled = 1
	InviteFreeGroupCodeStatusUsed    = 2

	// InviteFreeGroupAccessDuration is 24 hours per redeemed code
	InviteFreeGroupAccessDuration = 24 * 3600
)

// InviteFreeGroupCode is a one-time, user-bound code issued after a successful
// affiliate registration. It grants access to the invite free group and never
// changes the user's wallet quota.
type InviteFreeGroupCode struct {
	Id               int            `json:"id"`
	RecipientUserId  int            `json:"recipient_user_id" gorm:"index;uniqueIndex:idx_invite_free_group_recipient"`
	InvitationUserId int            `json:"invitation_user_id" gorm:"index;uniqueIndex:idx_invite_free_group_recipient"`
	Role             string         `json:"role" gorm:"type:varchar(16)"`
	Key              string         `json:"key" gorm:"type:char(32);uniqueIndex"`
	Status           int            `json:"status" gorm:"default:1;index"`
	CreatedTime      int64          `json:"created_time" gorm:"bigint"`
	RedeemedTime     int64          `json:"redeemed_time" gorm:"bigint"`
	ExpiresTime      int64          `json:"expires_time" gorm:"bigint;default:0;index"`
	UsedUserId       int            `json:"used_user_id" gorm:"index"`
	DeletedAt        gorm.DeletedAt `json:"-" gorm:"index"`
}

func IssueInviteFreeGroupCodesForReferral(inviterID, inviteeID int) error {
	if inviterID <= 0 || inviteeID <= 0 || inviterID == inviteeID {
		return errors.New("invalid invite referral users")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		for _, item := range []struct {
			recipient int
			role      string
		}{
			{recipient: inviterID, role: "inviter"},
			{recipient: inviteeID, role: "invitee"},
		} {
			var count int64
			if err := tx.Model(&InviteFreeGroupCode{}).
				Where("recipient_user_id = ? AND invitation_user_id = ?", item.recipient, inviteeID).
				Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				continue
			}
			code := &InviteFreeGroupCode{
				RecipientUserId:  item.recipient,
				InvitationUserId: inviteeID,
				Role:             item.role,
				Key:              common.GetUUID(),
				Status:           InviteFreeGroupCodeStatusEnabled,
				CreatedTime:      common.GetTimestamp(),
			}
			if err := tx.Create(code).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func GetInviteFreeGroupCodesForUser(userID int) ([]InviteFreeGroupCode, error) {
	if userID <= 0 {
		return nil, errors.New("invalid user id")
	}
	var codes []InviteFreeGroupCode
	err := DB.Where("recipient_user_id = ?", userID).Order("id desc").Find(&codes).Error
	return codes, err
}

func HasInviteFreeGroupAccess(userID int) (bool, error) {
	if userID <= 0 {
		return false, nil
	}
	now := common.GetTimestamp()
	var count int64
	err := DB.Model(&InviteFreeGroupCode{}).
		Where("recipient_user_id = ? AND used_user_id = ? AND status = ? AND (expires_time > ? OR (expires_time = 0 AND redeemed_time > 0 AND redeemed_time + ? > ?))",
			userID, userID, InviteFreeGroupCodeStatusUsed, now, InviteFreeGroupAccessDuration, now).
		Count(&count).Error
	return count > 0, err
}

func GetInviteFreeGroupAccessExpiresAt(userID int) (int64, error) {
	if userID <= 0 {
		return 0, nil
	}
	var maxExpires int64
	err := DB.Model(&InviteFreeGroupCode{}).
		Where("recipient_user_id = ? AND used_user_id = ? AND status = ?", userID, userID, InviteFreeGroupCodeStatusUsed).
		Select("COALESCE(MAX(CASE WHEN expires_time > 0 THEN expires_time ELSE redeemed_time + ? END), 0)", InviteFreeGroupAccessDuration).
		Scan(&maxExpires).Error
	return maxExpires, err
}

func RedeemInviteFreeGroupCode(key string, userID int) error {
	if key == "" {
		return errors.New("未提供解锁码")
	}
	if userID <= 0 {
		return errors.New("无效的 user id")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var code InviteFreeGroupCode
		if err := lockForUpdate(tx).Where("`key` = ?", key).First(&code).Error; err != nil {
			return errors.New("无效的解锁码")
		}
		if code.Status != InviteFreeGroupCodeStatusEnabled {
			return errors.New("该解锁码已被使用")
		}
		if code.RecipientUserId != userID {
			return errors.New("该解锁码不属于当前用户")
		}

		var currentMaxExpires int64
		if err := tx.Model(&InviteFreeGroupCode{}).
			Where("recipient_user_id = ? AND used_user_id = ? AND status = ?", userID, userID, InviteFreeGroupCodeStatusUsed).
			Select("COALESCE(MAX(CASE WHEN expires_time > 0 THEN expires_time ELSE redeemed_time + ? END), 0)", InviteFreeGroupAccessDuration).
			Scan(&currentMaxExpires).Error; err != nil {
			return err
		}

		now := common.GetTimestamp()
		baseTime := now
		if currentMaxExpires > now {
			baseTime = currentMaxExpires
		}
		newExpiresTime := baseTime + InviteFreeGroupAccessDuration

		result := tx.Model(&InviteFreeGroupCode{}).
			Where("id = ? AND status = ? AND recipient_user_id = ?", code.Id, InviteFreeGroupCodeStatusEnabled, userID).
			Updates(map[string]any{
				"status":        InviteFreeGroupCodeStatusUsed,
				"redeemed_time": now,
				"used_user_id":  userID,
				"expires_time":  newExpiresTime,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errors.New("该解锁码已被使用")
		}
		return nil
	})
}

func (code InviteFreeGroupCode) IsUsed() bool {
	return code.Status == InviteFreeGroupCodeStatusUsed
}

func (code InviteFreeGroupCode) String() string {
	return fmt.Sprintf("InviteFreeGroupCode<%d>", code.Id)
}

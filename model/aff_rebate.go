package model

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// AffRebateClaim is the main-DB idempotency ledger for invite top-up rebates.
// A source key is claimed in the same transaction as the wallet and rebate
// updates, so a failed transaction cannot leave a paid claim behind.
type AffRebateClaim struct {
	Id         int    `json:"id" gorm:"primaryKey"`
	SourceKey  string `json:"source_key" gorm:"type:varchar(191);uniqueIndex;not null"`
	InviterId  int    `json:"inviter_id" gorm:"index;not null"`
	InviteeId  int    `json:"invitee_id" gorm:"index;not null"`
	Reward     int    `json:"reward" gorm:"not null"`
	TopupQuota int    `json:"topup_quota" gorm:"not null"`
	CreatedAt  int64  `json:"created_at" gorm:"bigint;index"`
}

func (AffRebateClaim) TableName() string {
	return "aff_rebate_claims"
}

var ErrAffRebateAlreadyPaid = errors.New("aff rebate already paid for source")

// claimAffRebateSource inserts the unique event claim. A duplicate is a
// successful no-op; other database errors fail the enclosing top-up.
func claimAffRebateSource(tx *gorm.DB, sourceKey string, inviterId, inviteeId, reward, topupQuota int) (bool, error) {
	if sourceKey == "" {
		return true, nil
	}
	result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&AffRebateClaim{
		SourceKey:  sourceKey,
		InviterId:  inviterId,
		InviteeId:  inviteeId,
		Reward:     reward,
		TopupQuota: topupQuota,
		CreatedAt:  common.GetTimestamp(),
	})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

// rewardInviterOnTopupTx applies an invitee top-up rebate in the caller's
// transaction. Missing inviters, disabled/invalid rates, and zero rewards are
// deliberate no-ops so the principal top-up remains successful.
func rewardInviterOnTopupTx(tx *gorm.DB, inviteeId, topupQuota int, sourceKey string) (bool, error) {
	rate := common.AffTopupRate
	if inviteeId <= 0 || topupQuota <= 0 || rate <= 0 || rate > 1 || math.IsNaN(rate) || math.IsInf(rate, 0) {
		return false, nil
	}

	var invitee User
	if err := tx.Select("id", "inviter_id").First(&invitee, inviteeId).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	if invitee.InviterId <= 0 || invitee.InviterId == inviteeId {
		return false, nil
	}

	reward := common.QuotaFromFloat(float64(topupQuota) * rate)
	if reward <= 0 {
		return false, nil
	}
	if reward > topupQuota {
		reward = topupQuota
	}

	claimed, err := claimAffRebateSource(tx, strings.TrimSpace(sourceKey), invitee.InviterId, inviteeId, reward, topupQuota)
	if err != nil {
		return false, err
	}
	if !claimed {
		return false, ErrAffRebateAlreadyPaid
	}

	result := tx.Model(&User{}).Where("id = ?", invitee.InviterId).Updates(map[string]any{
		"aff_quota":   gorm.Expr("aff_quota + ?", reward),
		"aff_history": gorm.Expr("aff_history + ?", reward),
	})
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected != 1 {
		return false, gorm.ErrRecordNotFound
	}
	return true, nil
}

// RewardInviterOnTopupWithSource preserves the standalone API for callers
// outside a top-up transaction. Payment and redemption paths use the Tx
// helper above so principal and rebate commit atomically.
func RewardInviterOnTopupWithSource(inviteeId, topupQuota int, sourceKey string) {
	var paid bool
	err := DB.Transaction(func(tx *gorm.DB) error {
		var err error
		paid, err = rewardInviterOnTopupTx(tx, inviteeId, topupQuota, sourceKey)
		return err
	})
	if errors.Is(err, ErrAffRebateAlreadyPaid) {
		return
	}
	if err != nil {
		common.SysError(fmt.Sprintf("aff topup rebate failed invitee=%d source=%s: %v", inviteeId, sourceKey, err))
		return
	}
	if paid {
		logAffRebate(inviteeId, topupQuota, sourceKey)
	}
}

func logAffRebate(inviteeId, topupQuota int, sourceKey string) {
	var invitee User
	if err := DB.Select("inviter_id").First(&invitee, inviteeId).Error; err != nil || invitee.InviterId <= 0 {
		return
	}
	reward := common.QuotaFromFloat(float64(topupQuota) * common.AffTopupRate)
	if reward <= 0 {
		return
	}
	if reward > topupQuota {
		reward = topupQuota
	}
	content := fmt.Sprintf("邀请充值返利 %s（被邀请用户 #%d 充值 %s，比例 %.2f%%）", logger.LogQuota(reward), inviteeId, logger.LogQuota(topupQuota), common.AffTopupRate*100)
	if sourceKey != "" {
		content += fmt.Sprintf(" [aff_src:%s]", sourceKey)
	}
	RecordLog(invitee.InviterId, LogTypeSystem, content)
}

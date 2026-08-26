package model

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// AffRebateClaim is a main-DB unique claim ticket for invite topup rebates.
// Insert-first with unique source_key makes concurrent webhooks safe without
// relying on log-table scans (which are non-atomic and fail-open).
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

func isUniqueConstraintErr(err error) bool {
	if err == nil {
		return false
	}
	// GORM / driver messages vary by dialect; match common unique-violation text.
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique") ||
		strings.Contains(msg, "duplicate") ||
		strings.Contains(msg, "constraint failed") ||
		strings.Contains(msg, "1062")
}

// tryClaimAffRebateSource inserts a unique claim row.
// Returns (true, nil) if this caller owns the claim and should pay the rebate.
// Returns (false, nil) if source was already claimed.
// Returns (false, err) on unexpected DB errors (caller should fail closed / retry).
func tryClaimAffRebateSource(sourceKey string, inviterId, inviteeId, reward, topupQuota int) (claimed bool, err error) {
	sourceKey = strings.TrimSpace(sourceKey)
	if sourceKey == "" {
		// No business key: cannot atomically dedupe; caller pays without claim.
		return true, nil
	}
	row := &AffRebateClaim{
		SourceKey:  sourceKey,
		InviterId:  inviterId,
		InviteeId:  inviteeId,
		Reward:     reward,
		TopupQuota: topupQuota,
		CreatedAt:  common.GetTimestamp(),
	}
	if e := DB.Create(row).Error; e != nil {
		if isUniqueConstraintErr(e) {
			return false, nil
		}
		return false, e
	}
	return true, nil
}

// releaseAffRebateClaim removes a claim when credit failed after insert, so a
// later retry can re-claim. Best-effort; logs on failure.
func releaseAffRebateClaim(sourceKey string) {
	sourceKey = strings.TrimSpace(sourceKey)
	if sourceKey == "" {
		return
	}
	if err := DB.Where("source_key = ?", sourceKey).Delete(&AffRebateClaim{}).Error; err != nil {
		common.SysError(fmt.Sprintf("release aff rebate claim failed source=%s: %v", sourceKey, err))
	}
}

// ErrAffRebateAlreadyPaid is returned when a source was already claimed (tests / callers).
var ErrAffRebateAlreadyPaid = errors.New("aff rebate already paid for source")

// HasAffRebateClaim reports whether sourceKey already has a main-DB claim.
func HasAffRebateClaim(sourceKey string) bool {
	sourceKey = strings.TrimSpace(sourceKey)
	if sourceKey == "" {
		return false
	}
	var n int64
	if err := DB.Model(&AffRebateClaim{}).Where("source_key = ?", sourceKey).Limit(1).Count(&n).Error; err != nil {
		return false
	}
	return n > 0
}

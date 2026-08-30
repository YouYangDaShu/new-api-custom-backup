package model

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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

// tryClaimAffRebateSource inserts a unique claim row within the caller's
// transaction. It uses INSERT ... ON CONFLICT DO NOTHING (portable across
// SQLite/MySQL/PostgreSQL, same pattern as the lottery event ledger) so only a
// real duplicate key reads as "already claimed"; any other DB error is
// returned for the caller to fail closed. Running inside the same transaction
// as the credit removes the old double-pay window where the credit committed
// but the claim row was later deleted.
// Returns (true, nil) if this caller owns the claim and should pay the rebate.
// Returns (false, nil) if source was already claimed.
// Returns (false, err) on unexpected DB errors (caller should fail closed / retry).
func tryClaimAffRebateSource(tx *gorm.DB, sourceKey string, inviterId, inviteeId, reward, topupQuota int) (claimed bool, err error) {
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
	result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(row)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
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

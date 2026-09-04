package model

import (
	"errors"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// EconomicBillingOperation is the durable ledger boundary for a single
// money-changing effect. The unique operation key is shared by retries and
// recovery workers; the mutation and the done marker are committed together.
type EconomicBillingOperation struct {
	ID               int64  `gorm:"primaryKey"`
	OperationKey     string `gorm:"column:operation_key;type:varchar(191);uniqueIndex;not null"`
	RequestID        string `gorm:"column:request_id;type:varchar(64);index"`
	OperationType    string `gorm:"column:operation_type;type:varchar(32);index"`
	FundingSource    string `gorm:"column:funding_source;type:varchar(32);index"`
	PreConsumedQuota int64  `gorm:"column:pre_consumed_quota"`
	ActualQuota      int64  `gorm:"column:actual_quota"`
	Delta            int64  `gorm:"column:delta"`
	UsageSource      string `gorm:"column:usage_source;type:varchar(64)"`
	Phase            string `gorm:"column:phase;type:varchar(32);index"`
	Status           string `gorm:"column:status;type:varchar(32);index"`
	Result           string `gorm:"column:result;type:text"`
	TerminalReason   string `gorm:"column:terminal_reason;type:text"`
	LastError        string `gorm:"column:last_error;type:text"`
	CreatedAt        int64  `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt        int64  `gorm:"column:updated_at;autoUpdateTime"`
}

const (
	EconomicOperationInProgress = "in_progress"
	EconomicOperationDone       = "done"
	EconomicOperationFailed     = "failed"
	EconomicOperationUnknown    = "unknown"
)

func GetEconomicBillingOperation(key string) (*EconomicBillingOperation, error) {
	if DB == nil || key == "" {
		return nil, errors.New("invalid economic operation key")
	}
	var op EconomicBillingOperation
	if err := DB.Where("operation_key = ?", key).First(&op).Error; err != nil {
		return nil, err
	}
	return &op, nil
}

func beginEconomicOperation(tx *gorm.DB, key string, fields EconomicBillingOperation) (*EconomicBillingOperation, bool, error) {
	if key == "" {
		return nil, false, errors.New("empty economic operation key")
	}
	var op EconomicBillingOperation
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("operation_key = ?", key).First(&op).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		fields.OperationKey = key
		fields.Status = EconomicOperationInProgress
		if err := tx.Create(&fields).Error; err != nil {
			return nil, false, err
		}
		return &fields, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	if op.Status == EconomicOperationDone {
		return &op, false, nil
	}
	if op.Status == EconomicOperationUnknown {
		return &op, false, fmt.Errorf("economic operation %s is unknown/manual", key)
	}
	if op.Status == EconomicOperationInProgress {
		// A committed in-progress row is never replayed blindly. It is only
		// observable while its owner transaction is active; after rollback the
		// insert disappears and a retry can safely claim it.
		return &op, false, fmt.Errorf("economic operation %s is in progress", key)
	}
	if err := tx.Model(&op).Updates(map[string]any{"status": EconomicOperationInProgress, "last_error": ""}).Error; err != nil {
		return nil, false, err
	}
	op.Status = EconomicOperationInProgress
	return &op, true, nil
}

func markEconomicFailure(key string, fields EconomicBillingOperation, err error) {
	if DB == nil || key == "" {
		return
	}
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	// Only a row that is still retryable may be marked failed. In particular,
	// never overwrite a committed done/unknown result after an independent
	// transaction has completed.
	result := DB.Model(&EconomicBillingOperation{}).
		Where("operation_key = ? AND status = ?", key, EconomicOperationFailed).
		Updates(map[string]any{"phase": "failed", "last_error": msg})
	if result.Error != nil || result.RowsAffected > 0 {
		return
	}
	fields.OperationKey = key
	fields.Status = EconomicOperationFailed
	fields.Phase = "failed"
	fields.LastError = msg
	// The original insert may have rolled back, so create a durable retryable
	// record. A concurrent successful retry wins the unique key and is never
	// overwritten here.
	_ = DB.Create(&fields).Error
}

func markEconomicUnknown(key string, fields EconomicBillingOperation, err error) {
	if DB == nil || key == "" {
		return
	}
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	result := DB.Model(&EconomicBillingOperation{}).
		Where("operation_key = ? AND status = ?", key, EconomicOperationInProgress).
		Updates(map[string]any{"status": EconomicOperationUnknown, "phase": "manual", "terminal_reason": "commit outcome unknown", "last_error": msg})
	if result.Error != nil || result.RowsAffected > 0 {
		return
	}
	fields.OperationKey = key
	fields.Status = EconomicOperationUnknown
	fields.Phase = "manual"
	fields.TerminalReason = "commit outcome unknown"
	fields.LastError = msg
	_ = DB.Create(&fields).Error
}

func applyEconomicOperation(key string, fields EconomicBillingOperation, mutate func(*gorm.DB) error) error {
	var mutationErr error
	callbackCompleted := false
	mutationStarted := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		op, apply, err := beginEconomicOperation(tx, key, fields)
		if err != nil {
			return err
		}
		if !apply {
			if op.Status == EconomicOperationDone {
				return nil
			}
			return fmt.Errorf("economic operation %s is not replayable", key)
		}
		mutationStarted = true
		if err := mutate(tx); err != nil {
			mutationErr = err
			// Returning the mutation error is intentional: GORM rolls back the
			// whole transaction, including any partial economic mutation.
			return err
		}
		if err := tx.Model(op).Updates(map[string]any{"status": EconomicOperationDone, "phase": "committed", "result": "applied", "last_error": ""}).Error; err != nil {
			return err
		}
		callbackCompleted = true
		return nil
	})
	if mutationErr != nil {
		markEconomicFailure(key, fields, mutationErr)
		return mutationErr
	}
	if err != nil {
		if callbackCompleted {
			// A commit error is ambiguous: the mutation and done marker may have
			// reached durable storage. Fail closed and require manual recovery.
			markEconomicUnknown(key, fields, err)
		} else if mutationStarted {
			// The callback returned an error before commit, so the transaction is
			// known to have rolled back and can be retried safely.
			markEconomicFailure(key, fields, err)
		}
	}
	return err
}

func ApplyWalletQuotaOperation(key string, userID, delta int, requestID, source string, pre, actual int64) error {
	return ApplyWalletQuotaOperationType(key, userID, delta, requestID, source, pre, actual, "settlement")
}

func ApplyWalletQuotaOperationType(key string, userID, delta int, requestID, source string, pre, actual int64, operationType string) error {
	if delta == 0 {
		return nil
	}
	fields := EconomicBillingOperation{RequestID: requestID, OperationType: operationType, FundingSource: source, PreConsumedQuota: pre, ActualQuota: actual, Delta: int64(delta), Phase: "mutation"}
	return applyEconomicOperation(key, fields, func(tx *gorm.DB) error {
		result := tx.Model(&User{}).Where("id = ?", userID).Update("quota", gorm.Expr("quota + ?", delta))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return fmt.Errorf("user %d not found", userID)
		}
		return nil
	})
}

func ApplyTokenQuotaOperation(key string, tokenID int, delta int, requestID, source string, pre, actual int64) error {
	return ApplyTokenQuotaOperationType(key, tokenID, delta, requestID, source, pre, actual, "settlement")
}

func ApplyTokenQuotaOperationType(key string, tokenID int, delta int, requestID, source string, pre, actual int64, operationType string) error {
	if delta == 0 {
		return nil
	}
	fields := EconomicBillingOperation{RequestID: requestID, OperationType: operationType, FundingSource: source, PreConsumedQuota: pre, ActualQuota: actual, Delta: int64(delta), Phase: "mutation"}
	return applyEconomicOperation(key, fields, func(tx *gorm.DB) error {
		result := tx.Model(&Token{}).Where("id = ?", tokenID).Updates(map[string]any{
			"remain_quota": gorm.Expr("remain_quota + ?", delta),
			"used_quota":   gorm.Expr("used_quota - ?", delta),
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return fmt.Errorf("token %d not found", tokenID)
		}
		return nil
	})
}

func ApplySubscriptionQuotaOperation(key string, subscriptionID int, delta int64, requestID, source string, pre, actual int64) error {
	return ApplySubscriptionQuotaOperationType(key, subscriptionID, delta, requestID, source, pre, actual, "settlement")
}

func ApplySubscriptionQuotaOperationType(key string, subscriptionID int, delta int64, requestID, source string, pre, actual int64, operationType string) error {
	if delta == 0 {
		return nil
	}
	fields := EconomicBillingOperation{RequestID: requestID, OperationType: operationType, FundingSource: source, PreConsumedQuota: pre, ActualQuota: actual, Delta: delta, Phase: "mutation"}
	return applyEconomicOperation(key, fields, func(tx *gorm.DB) error {
		var sub UserSubscription
		if err := lockForUpdate(tx).Where("id = ?", subscriptionID).First(&sub).Error; err != nil {
			return err
		}
		newUsed := sub.AmountUsed + delta
		if newUsed < 0 {
			newUsed = 0
		}
		if sub.AmountTotal > 0 && newUsed > sub.AmountTotal {
			return fmt.Errorf("subscription used exceeds total, used=%d total=%d", newUsed, sub.AmountTotal)
		}
		return tx.Model(&sub).Update("amount_used", newUsed).Error
	})
}

func ApplySubscriptionRefundOperation(key, requestID, source string, pre, actual int64) error {
	fields := EconomicBillingOperation{RequestID: requestID, OperationType: "refund", FundingSource: source, PreConsumedQuota: pre, ActualQuota: actual, Delta: -pre, Phase: "mutation"}
	return applyEconomicOperation(key, fields, func(tx *gorm.DB) error {
		var record SubscriptionPreConsumeRecord
		if err := lockForUpdate(tx).Where("request_id = ?", requestID).First(&record).Error; err != nil {
			return err
		}
		if record.Status == "refunded" {
			return nil
		}
		var sub UserSubscription
		if err := lockForUpdate(tx).Where("id = ?", record.UserSubscriptionId).First(&sub).Error; err != nil {
			return err
		}
		newUsed := sub.AmountUsed - record.PreConsumed
		if newUsed < 0 {
			newUsed = 0
		}
		if err := tx.Model(&sub).Update("amount_used", newUsed).Error; err != nil {
			return err
		}
		return tx.Model(&record).Update("status", "refunded").Error
	})
}

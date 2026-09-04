package model

import (
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	TaskBillingOperationInProgress = "in_progress"
	TaskBillingOperationDone       = "done"
	TaskBillingOperationPending    = "pending"
	TaskBillingOperationManual     = "manual"
)

// TaskBillingOperation is the durable idempotency record for a terminal task
// money movement. An in-progress row is deliberately retained across process
// crashes: a replacement worker must not replay an operation whose outcome is
// unknown. Once its operation lease expires, Begin marks it manual for
// reconciliation instead of risking a second debit/refund.
type TaskBillingOperation struct {
	ID               int64  `gorm:"primaryKey"`
	OperationKey     string `gorm:"column:operation_key;type:varchar(191);uniqueIndex;not null"`
	TaskID           int64  `gorm:"column:task_id;index;not null"`
	State            string `gorm:"column:state;type:varchar(16);index;not null"`
	Owner            string `gorm:"column:owner;type:varchar(128);index"`
	LeaseUntil       int64  `gorm:"column:lease_until;index"`
	LastError        string `gorm:"column:last_error;type:text"`
	PreConsumedQuota int64  `gorm:"column:pre_consumed_quota"`
	ActualQuota      int64  `gorm:"column:actual_quota"`
	FundingSource    string `gorm:"column:funding_source;type:varchar(32)"`
	OperationType    string `gorm:"column:operation_type;type:varchar(32)"`
	Phase            string `gorm:"column:phase;type:varchar(32)"`
	Result           string `gorm:"column:result;type:text"`
	UsageSource      string `gorm:"column:usage_source;type:varchar(64)"`
	TerminalReason   string `gorm:"column:terminal_reason;type:text"`
	CreatedAt        int64  `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt        int64  `gorm:"column:updated_at;autoUpdateTime"`
}

// BeginTaskBillingOperation claims the stable operation key. The returned
// state is one of in_progress (caller owns it), done, in_flight, or manual.
func BeginTaskBillingOperation(operationKey string, taskID int64, owner string, now, leaseUntil int64) (string, error) {
	if operationKey == "" || taskID == 0 || owner == "" {
		return "", errors.New("invalid task billing operation claim")
	}
	returnState := ""
	err := DB.Transaction(func(tx *gorm.DB) error {
		var op TaskBillingOperation
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("operation_key = ?", operationKey).First(&op).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			op = TaskBillingOperation{OperationKey: operationKey, TaskID: taskID, State: TaskBillingOperationInProgress, Owner: owner, LeaseUntil: leaseUntil}
			if err := tx.Create(&op).Error; err != nil {
				return err
			}
			returnState = TaskBillingOperationInProgress
			return nil
		}
		if err != nil {
			return err
		}
		switch op.State {
		case TaskBillingOperationDone:
			returnState = TaskBillingOperationDone
		case TaskBillingOperationManual:
			returnState = TaskBillingOperationManual
		case TaskBillingOperationPending:
			if err := tx.Model(&op).Updates(map[string]any{
				"state": TaskBillingOperationInProgress, "owner": owner, "lease_until": leaseUntil,
				"last_error": "",
			}).Error; err != nil {
				return err
			}
			returnState = TaskBillingOperationInProgress
		case TaskBillingOperationInProgress:
			if op.Owner == owner {
				returnState = TaskBillingOperationInProgress
			} else if op.LeaseUntil > now {
				returnState = "in_flight"
			} else {
				if err := tx.Model(&op).Updates(map[string]any{
					"state": TaskBillingOperationManual, "owner": "", "lease_until": 0,
					"last_error": "operation lease expired; outcome requires reconciliation",
				}).Error; err != nil {
					return err
				}
				returnState = TaskBillingOperationManual
			}
		default:
			returnState = TaskBillingOperationManual
		}
		return nil
	})
	return returnState, err
}

func RenewTaskBillingOperation(operationKey, owner string, leaseUntil int64) (bool, error) {
	if operationKey == "" || owner == "" {
		return false, nil
	}
	result := DB.Model(&TaskBillingOperation{}).
		Where("operation_key = ? AND state = ? AND owner = ?", operationKey, TaskBillingOperationInProgress, owner).
		Updates(map[string]any{"lease_until": leaseUntil})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func CompleteTaskBillingOperation(operationKey, owner string) (bool, error) {
	if operationKey == "" || owner == "" {
		return false, nil
	}
	result := DB.Model(&TaskBillingOperation{}).
		Where("operation_key = ? AND state = ? AND owner = ?", operationKey, TaskBillingOperationInProgress, owner).
		Updates(map[string]any{"state": TaskBillingOperationDone, "owner": "", "lease_until": 0, "last_error": ""})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func FailTaskBillingOperation(operationKey, owner string, err error) error {
	if operationKey == "" || owner == "" {
		return nil
	}
	lastError := ""
	if err != nil {
		lastError = err.Error()
	}
	return DB.Model(&TaskBillingOperation{}).
		Where("operation_key = ? AND state = ? AND owner = ?", operationKey, TaskBillingOperationInProgress, owner).
		Updates(map[string]any{"state": TaskBillingOperationPending, "owner": "", "lease_until": 0, "last_error": lastError}).Error
}

// MarkTaskBillingOperationManual is fail-closed when the economic mutation may
// have committed but the completion marker could not be written.
func MarkTaskBillingOperationManual(operationKey, owner, reason string) error {
	if operationKey == "" {
		return nil
	}
	return DB.Model(&TaskBillingOperation{}).
		Where("operation_key = ? AND state = ? AND (owner = ? OR owner = '')", operationKey, TaskBillingOperationInProgress, owner).
		Updates(map[string]any{"state": TaskBillingOperationManual, "owner": "", "lease_until": 0, "last_error": reason, "phase": "unknown", "terminal_reason": reason}).Error
}

package model

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestEconomicBillingOperationConcurrentSettlementIsSingleEffect(t *testing.T) {
	if DB == nil {
		t.Skip("DB not initialized")
	}
	require.NoError(t, DB.AutoMigrate(&User{}, &EconomicBillingOperation{}))
	key := "ledger-concurrent-settlement"
	DB.Where("operation_key = ?", key).Delete(&EconomicBillingOperation{})
	user := &User{Username: "ledger-user", Password: "password", Quota: 100}
	require.NoError(t, DB.Create(user).Error)
	t.Cleanup(func() {
		DB.Delete(&User{}, user.Id)
		DB.Where("operation_key = ?", key).Delete(&EconomicBillingOperation{})
	})

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = ApplyWalletQuotaOperation(key, user.Id, -25, "req-ledger", "wallet", 100, 125)
		}(i)
	}
	wg.Wait()
	for _, err := range errs {
		require.NoError(t, err)
	}
	var got User
	require.NoError(t, DB.First(&got, user.Id).Error)
	assert.Equal(t, 75, got.Quota)
	op, err := GetEconomicBillingOperation(key)
	require.NoError(t, err)
	assert.Equal(t, EconomicOperationDone, op.Status)
	assert.Equal(t, int64(-25), op.Delta)
}

func TestEconomicBillingOperationFailureIsDurableAndRetryable(t *testing.T) {
	if DB == nil {
		t.Skip("DB not initialized")
	}
	key := "ledger-failure-retry"
	DB.Where("operation_key = ?", key).Delete(&EconomicBillingOperation{})
	user := &User{Username: "ledger-fail-user", Password: "password", Quota: 100, AffCode: "ledger-fail-aff"}
	require.NoError(t, DB.Create(user).Error)
	t.Cleanup(func() { DB.Delete(&User{}, user.Id) })
	fields := EconomicBillingOperation{RequestID: "req-fail", OperationType: "settlement", FundingSource: "wallet"}
	err := applyEconomicOperation(key, fields, func(tx *gorm.DB) error {
		if err := tx.Model(&User{}).Where("id = ?", user.Id).Update("quota", gorm.Expr("quota + ?", 25)).Error; err != nil {
			return err
		}
		return assert.AnError
	})
	assert.Error(t, err)
	var after User
	require.NoError(t, DB.First(&after, user.Id).Error)
	assert.Equal(t, 100, after.Quota)
	op, getErr := GetEconomicBillingOperation(key)
	require.NoError(t, getErr)
	assert.Equal(t, EconomicOperationFailed, op.Status)
	require.NoError(t, applyEconomicOperation(key, fields, func(tx *gorm.DB) error {
		return tx.Model(&User{}).Where("id = ?", user.Id).Update("quota", gorm.Expr("quota + ?", 25)).Error
	}))
	require.NoError(t, DB.First(&after, user.Id).Error)
	assert.Equal(t, 125, after.Quota)
	op, getErr = GetEconomicBillingOperation(key)
	require.NoError(t, getErr)
	assert.Equal(t, EconomicOperationDone, op.Status)
}

func TestEconomicBillingOperationUnknownIsFailClosed(t *testing.T) {
	key := "ledger-unknown-manual"
	DB.Where("operation_key = ?", key).Delete(&EconomicBillingOperation{})
	require.NoError(t, DB.Create(&EconomicBillingOperation{OperationKey: key, Status: EconomicOperationUnknown}).Error)
	called := false
	err := applyEconomicOperation(key, EconomicBillingOperation{RequestID: "req-unknown"}, func(_ *gorm.DB) error {
		called = true
		return nil
	})
	assert.Error(t, err)
	assert.False(t, called)
}

func TestEconomicBillingOperationCommitAmbiguityIsManual(t *testing.T) {
	key := "ledger-commit-ambiguity"
	DB.Where("operation_key = ?", key).Delete(&EconomicBillingOperation{})
	fields := EconomicBillingOperation{OperationKey: key, RequestID: "req-ambiguous", OperationType: "settlement"}
	require.NoError(t, DB.Create(&EconomicBillingOperation{OperationKey: key, Status: EconomicOperationInProgress}).Error)
	markEconomicUnknown(key, fields, assert.AnError)
	op, err := GetEconomicBillingOperation(key)
	require.NoError(t, err)
	assert.Equal(t, EconomicOperationUnknown, op.Status)
	called := false
	err = applyEconomicOperation(key, fields, func(_ *gorm.DB) error {
		called = true
		return nil
	})
	assert.Error(t, err)
	assert.False(t, called)
}

func TestEconomicQuotaOperationRejectsMissingTargets(t *testing.T) {
	for _, key := range []string{"ledger-missing-user", "ledger-missing-token"} {
		DB.Where("operation_key = ?", key).Delete(&EconomicBillingOperation{})
	}
	err := ApplyWalletQuotaOperation("ledger-missing-user", 999999, -1, "req-missing", "wallet", 1, 1)
	assert.Error(t, err)
	op, getErr := GetEconomicBillingOperation("ledger-missing-user")
	require.NoError(t, getErr)
	assert.Equal(t, EconomicOperationFailed, op.Status)
	err = ApplyTokenQuotaOperation("ledger-missing-token", 999999, -1, "req-missing", "wallet", 1, 1)
	assert.Error(t, err)
	op, getErr = GetEconomicBillingOperation("ledger-missing-token")
	require.NoError(t, getErr)
	assert.Equal(t, EconomicOperationFailed, op.Status)
}

func TestEconomicQuotaOperationRecordsRefundType(t *testing.T) {
	user := &User{Username: "ledger-refund-user", Password: "password", Quota: 10, AffCode: "ledger-refund-aff"}
	require.NoError(t, DB.Create(user).Error)
	t.Cleanup(func() { DB.Delete(&User{}, user.Id) })
	key := "ledger-refund-type"
	DB.Where("operation_key = ?", key).Delete(&EconomicBillingOperation{})
	require.NoError(t, ApplyWalletQuotaOperationType(key, user.Id, 5, "req-refund", "wallet", 5, 0, "refund"))
	op, err := GetEconomicBillingOperation(key)
	require.NoError(t, err)
	assert.Equal(t, "refund", op.OperationType)
}

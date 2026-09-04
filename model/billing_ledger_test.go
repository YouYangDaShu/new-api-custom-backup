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
	err := applyEconomicOperation(key, EconomicBillingOperation{RequestID: "req-fail", OperationType: "settlement", FundingSource: "wallet"}, func(_ *gorm.DB) error {
		return assert.AnError
	})
	assert.Error(t, err)
	op, getErr := GetEconomicBillingOperation(key)
	require.NoError(t, getErr)
	assert.Equal(t, EconomicOperationFailed, op.Status)
	DB.Where("operation_key = ?", key).Delete(&EconomicBillingOperation{})
}

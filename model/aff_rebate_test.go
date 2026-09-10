package model

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupAffiliateRebateFixture(t *testing.T) (inviter *User, invitee *User) {
	t.Helper()
	require.NoError(t, DB.AutoMigrate(&User{}, &AffRebateClaim{}))

	oldRate := common.AffTopupRate
	common.AffTopupRate = 0.1
	t.Cleanup(func() {
		common.AffTopupRate = oldRate
		DB.Where("source_key LIKE ?", "test-aff-rebate:%").Delete(&AffRebateClaim{})
		DB.Where("username LIKE ?", "test-aff-rebate-%").Delete(&User{})
	})

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	inviter = &User{
		Username: "test-aff-rebate-inviter-" + suffix,
		Password: "password",
		Status:   common.UserStatusEnabled,
		AffCode:  "test-aff-rebate-inviter-code-" + suffix,
	}
	require.NoError(t, DB.Create(inviter).Error)
	invitee = &User{
		Username:  "test-aff-rebate-invitee-" + suffix,
		Password:  "password",
		Status:    common.UserStatusEnabled,
		InviterId: inviter.Id,
		AffCode:   "test-aff-rebate-invitee-code-" + suffix,
	}
	require.NoError(t, DB.Create(invitee).Error)
	return inviter, invitee
}

func TestCreditTopUpQuotaWithSourcePaysAffiliateRebateOnce(t *testing.T) {
	inviter, invitee := setupAffiliateRebateFixture(t)

	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		return creditTopUpQuotaWithSource(tx, invitee.Id, 1000, nil, "test-aff-rebate:topup-1")
	}))
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		return creditTopUpQuotaWithSource(tx, invitee.Id, 500, nil, "test-aff-rebate:topup-1")
	}))

	var gotInviter, gotInvitee User
	require.NoError(t, DB.First(&gotInviter, inviter.Id).Error)
	require.NoError(t, DB.First(&gotInvitee, invitee.Id).Error)
	assert.Equal(t, 100, gotInviter.AffQuota)
	assert.Equal(t, 100, gotInviter.AffHistoryQuota)
	assert.Equal(t, 1000, gotInvitee.Quota)

	var claims []AffRebateClaim
	require.NoError(t, DB.Where("source_key = ?", "test-aff-rebate:topup-1").Find(&claims).Error)
	assert.Len(t, claims, 1)
}

func TestCreditTopUpQuotaWithSourceRollsBackPrincipalAndClaim(t *testing.T) {
	inviter, invitee := setupAffiliateRebateFixture(t)

	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := creditTopUpQuotaWithSource(tx, invitee.Id, 1000, nil, "test-aff-rebate:rollback"); err != nil {
			return err
		}
		return errors.New("forced test rollback")
	})
	require.Error(t, err)

	var gotInviter, gotInvitee User
	require.NoError(t, DB.First(&gotInviter, inviter.Id).Error)
	require.NoError(t, DB.First(&gotInvitee, invitee.Id).Error)
	assert.Zero(t, gotInviter.AffQuota)
	assert.Zero(t, gotInviter.AffHistoryQuota)
	assert.Zero(t, gotInvitee.Quota)

	var claimCount int64
	require.NoError(t, DB.Model(&AffRebateClaim{}).Where("source_key = ?", "test-aff-rebate:rollback").Count(&claimCount).Error)
	assert.Zero(t, claimCount)
}

func TestCreditTopUpQuotaWithSourceKeepsPrincipalWhenRebateDisabled(t *testing.T) {
	_, invitee := setupAffiliateRebateFixture(t)
	common.AffTopupRate = 0

	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		return creditTopUpQuotaWithSource(tx, invitee.Id, 1000, nil, "test-aff-rebate:disabled")
	}))

	var got User
	require.NoError(t, DB.First(&got, invitee.Id).Error)
	assert.Equal(t, 1000, got.Quota)

	var claimCount int64
	require.NoError(t, DB.Model(&AffRebateClaim{}).Where("source_key = ?", "test-aff-rebate:disabled").Count(&claimCount).Error)
	assert.Zero(t, claimCount)
}

func TestCreditTopUpQuotaWithSourceKeepsPrincipalWithoutInviter(t *testing.T) {
	oldRate := common.AffTopupRate
	common.AffTopupRate = 0.1
	t.Cleanup(func() { common.AffTopupRate = oldRate })

	invitee := &User{
		Username: "test-aff-rebate-no-inviter",
		Password: "password",
		Status:   common.UserStatusEnabled,
		AffCode:  fmt.Sprintf("test-aff-rebate-no-inviter-%d", time.Now().UnixNano()),
	}
	require.NoError(t, DB.Create(invitee).Error)
	t.Cleanup(func() { DB.Delete(&User{}, invitee.Id) })

	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		return creditTopUpQuotaWithSource(tx, invitee.Id, 1000, nil, "test-aff-rebate:no-inviter")
	}))

	var got User
	require.NoError(t, DB.First(&got, invitee.Id).Error)
	assert.Equal(t, 1000, got.Quota)
	assert.Zero(t, got.AffQuota)
}

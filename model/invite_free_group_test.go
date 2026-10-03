package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestInviteFreeGroupCodeIsBoundAndUnlocksOnlyItsRecipient(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&User{}, &InviteFreeGroupCode{}))
	inviter := &User{Username: "ifg-inviter-" + common.GetUUID(), Password: "password", Status: 1, AffCode: common.GetUUID()}
	invitee := &User{Username: "ifg-invitee-" + common.GetUUID(), Password: "password", Status: 1, AffCode: common.GetUUID()}
	require.NoError(t, DB.Create(inviter).Error)
	require.NoError(t, DB.Create(invitee).Error)
	t.Cleanup(func() {
		DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Where("recipient_user_id IN ?", []int{inviter.Id, invitee.Id}).Delete(&InviteFreeGroupCode{})
		DB.Unscoped().Delete(inviter)
		DB.Unscoped().Delete(invitee)
	})

	require.NoError(t, IssueInviteFreeGroupCodesForReferral(inviter.Id, invitee.Id))
	inviterCodes, err := GetInviteFreeGroupCodesForUser(inviter.Id)
	require.NoError(t, err)
	inviteeCodes, err := GetInviteFreeGroupCodesForUser(invitee.Id)
	require.NoError(t, err)
	require.Len(t, inviterCodes, 1)
	require.Len(t, inviteeCodes, 1)

	allowed, err := HasInviteFreeGroupAccess(inviter.Id)
	require.NoError(t, err)
	require.False(t, allowed)
	require.Error(t, RedeemInviteFreeGroupCode(inviterCodes[0].Key, invitee.Id))
	require.NoError(t, RedeemInviteFreeGroupCode(inviterCodes[0].Key, inviter.Id))
	allowed, err = HasInviteFreeGroupAccess(inviter.Id)
	require.NoError(t, err)
	require.True(t, allowed)
	expiresAt, err := GetInviteFreeGroupAccessExpiresAt(inviter.Id)
	require.NoError(t, err)
	require.Greater(t, expiresAt, common.GetTimestamp())
	require.Error(t, RedeemInviteFreeGroupCode(inviterCodes[0].Key, inviter.Id))

	allowed, err = HasInviteFreeGroupAccess(invitee.Id)
	require.NoError(t, err)
	require.False(t, allowed)
	require.NoError(t, RedeemInviteFreeGroupCode(inviteeCodes[0].Key, invitee.Id))
	allowed, err = HasInviteFreeGroupAccess(invitee.Id)
	require.NoError(t, err)
	require.True(t, allowed)

	// Test stacking: issue a second referral code to inviter
	thirdUser := &User{Username: "ifg-third-" + common.GetUUID(), Password: "password", Status: 1, AffCode: common.GetUUID()}
	require.NoError(t, DB.Create(thirdUser).Error)
	defer DB.Unscoped().Delete(thirdUser)
	require.NoError(t, IssueInviteFreeGroupCodesForReferral(inviter.Id, thirdUser.Id))
	inviterCodes2, err := GetInviteFreeGroupCodesForUser(inviter.Id)
	require.NoError(t, err)
	require.Len(t, inviterCodes2, 2)
	// Redeem the second code
	require.NoError(t, RedeemInviteFreeGroupCode(inviterCodes2[0].Key, inviter.Id))
	expiresAt2, err := GetInviteFreeGroupAccessExpiresAt(inviter.Id)
	require.NoError(t, err)
	require.InDelta(t, expiresAt+InviteFreeGroupAccessDuration, expiresAt2, 5)

	// Test expiration: simulate past expiry time
	DB.Model(&InviteFreeGroupCode{}).Where("recipient_user_id = ?", inviter.Id).Update("expires_time", common.GetTimestamp()-100)
	allowed, err = HasInviteFreeGroupAccess(inviter.Id)
	require.NoError(t, err)
	require.False(t, allowed)
}

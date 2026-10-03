package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestInsertWithInviterIssuesInviteFreeGroupCodesForBothSides(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&User{}, &InviteFreeGroupCode{}))
	inviter := &User{Username: "ifg-hook-inviter-" + common.GetUUID(), Password: "password", Status: 1, AffCode: common.GetUUID()}
	require.NoError(t, DB.Create(inviter).Error)
	t.Cleanup(func() {
		DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Where("recipient_user_id IN ?", []int{inviter.Id}).Delete(&InviteFreeGroupCode{})
		DB.Unscoped().Delete(inviter)
	})
	invitee := &User{Username: "ifg-hook-invitee-" + common.GetUUID(), Password: "password"}
	require.NoError(t, invitee.Insert(inviter.Id))
	t.Cleanup(func() { DB.Unscoped().Delete(&User{Id: invitee.Id}) })

	inviterCodes, err := GetInviteFreeGroupCodesForUser(inviter.Id)
	require.NoError(t, err)
	inviteeCodes, err := GetInviteFreeGroupCodesForUser(invitee.Id)
	require.NoError(t, err)
	require.Len(t, inviterCodes, 1)
	require.Len(t, inviteeCodes, 1)
	require.Equal(t, "inviter", inviterCodes[0].Role)
	require.Equal(t, "invitee", inviteeCodes[0].Role)
	require.Equal(t, invitee.Id, inviterCodes[0].InvitationUserId)
}

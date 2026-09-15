package model

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

func (user *User) EffectiveConcurrencyLimit() int {
	if user.ConcurrencyOverride {
		return user.Concurrency
	}
	return common.GetUserConcurrencyDefault()
}

func (user *User) ConcurrencySource() string {
	if user.ConcurrencyOverride {
		return "custom"
	}
	return "default"
}

// UpdateUserConcurrency is the only persistence path for the user concurrency
// policy. Keeping it separate from the generic profile update prevents a
// client from changing the limit through an unrelated user payload.
func UpdateUserConcurrency(userID, limit int, override bool) (*User, error) {
	if userID <= 0 {
		return nil, errors.New("invalid user id")
	}
	if limit < 0 || limit > common.MaxUserConcurrency {
		return nil, fmt.Errorf("concurrency must be between 0 and %d", common.MaxUserConcurrency)
	}
	if err := DB.Model(&User{}).Where("id = ?", userID).Updates(map[string]any{
		"concurrency":          limit,
		"concurrency_override": override,
	}).Error; err != nil {
		return nil, err
	}
	user, err := GetUserById(userID, false)
	if err != nil {
		return nil, err
	}
	if err := updateUserCache(*user); err != nil {
		return nil, err
	}
	return user, nil
}

// UpdateUsersConcurrency updates a validated set of users in one database
// operation and refreshes each affected cache after the commit.
func UpdateUsersConcurrency(userIDs []int, limit int, override bool) error {
	if len(userIDs) == 0 {
		return errors.New("no users selected")
	}
	if limit < 0 || limit > common.MaxUserConcurrency {
		return fmt.Errorf("concurrency must be between 0 and %d", common.MaxUserConcurrency)
	}
	result := DB.Model(&User{}).Where("id IN ?", userIDs).Updates(map[string]any{
		"concurrency":          limit,
		"concurrency_override": override,
	})
	if result.Error != nil {
		return result.Error
	}
	if int(result.RowsAffected) != len(userIDs) {
		return gorm.ErrRecordNotFound
	}
	for _, userID := range userIDs {
		if err := PublishUserAuthCache(userID); err != nil {
			return err
		}
	}
	return nil
}

// ResetUserConcurrencyToDefault resets only non-admin users. It returns the
// affected IDs so callers can refresh their user-cache entries after commit.
func ResetUserConcurrencyToDefault(limit int) ([]int, error) {
	if limit < 1 || limit > common.MaxUserConcurrency {
		return nil, fmt.Errorf("concurrency default must be between 1 and %d", common.MaxUserConcurrency)
	}
	var ids []int
	if err := DB.Transaction(func(tx *gorm.DB) error {
		var users []User
		if err := tx.Select("id").Where("role < ?", common.RoleAdminUser).Find(&users).Error; err != nil {
			return err
		}
		ids = make([]int, 0, len(users))
		for _, user := range users {
			ids = append(ids, user.Id)
		}
		return tx.Model(&User{}).Where("role < ?", common.RoleAdminUser).Updates(map[string]any{
			"concurrency":          limit,
			"concurrency_override": false,
		}).Error
	}); err != nil {
		return nil, err
	}
	for _, id := range ids {
		if err := PublishUserAuthCache(id); err != nil {
			return nil, err
		}
	}
	return ids, nil
}

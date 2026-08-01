package model

// GetUsersByIdsUnscoped returns active and soft-deleted users for batch
// permission checks. Callers must validate that every requested ID is present.
func GetUsersByIdsUnscoped(ids []int) ([]*User, error) {
	var users []*User
	err := DB.Unscoped().
		Where("id IN ?", ids).
		Omit("password").
		Find(&users).Error
	return users, err
}

// BatchUpdateUserGroup updates only the ownership group, so callers do not
// need to send complete user records and risk overwriting unrelated fields.
func BatchUpdateUserGroup(ids []int, group string) error {
	return DB.Model(&User{}).Where("id IN ?", ids).Update("group", group).Error
}

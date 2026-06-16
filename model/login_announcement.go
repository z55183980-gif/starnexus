package model

import (
	"fmt"

	"github.com/QuantumNous/new-api/setting/console_setting"
)

func announcementItemID(item map[string]interface{}) (int, bool) {
	raw, ok := item["id"]
	if !ok || raw == nil {
		return 0, false
	}
	switch v := raw.(type) {
	case float64:
		return int(v), true
	case int:
		return v, true
	case int64:
		return int(v), true
	default:
		return 0, false
	}
}

func isLoginAnnouncementTarget(item map[string]interface{}) bool {
	target, _ := item["target"].(string)
	return target == "login" || target == "both"
}

func containsInt(values []int, target int) bool {
	for _, v := range values {
		if v == target {
			return true
		}
	}
	return false
}

func GetUnreadLoginAnnouncements(userID int) ([]map[string]interface{}, error) {
	if userID <= 0 {
		return nil, fmt.Errorf("invalid user id")
	}

	cs := console_setting.GetConsoleSetting()
	if !cs.AnnouncementsEnabled {
		return []map[string]interface{}{}, nil
	}

	setting, err := GetUserSetting(userID, false)
	if err != nil {
		return nil, err
	}

	readIDs := setting.ReadLoginAnnouncementIds
	all := console_setting.GetAnnouncements()
	unread := make([]map[string]interface{}, 0, len(all))
	for _, item := range all {
		if !isLoginAnnouncementTarget(item) {
			continue
		}
		id, ok := announcementItemID(item)
		if !ok || id <= 0 {
			unread = append(unread, item)
			continue
		}
		if !containsInt(readIDs, id) {
			unread = append(unread, item)
		}
	}
	return unread, nil
}

func MarkLoginAnnouncementsRead(userID int, ids []int) error {
	if userID <= 0 {
		return fmt.Errorf("invalid user id")
	}
	if len(ids) == 0 {
		return nil
	}

	user, err := GetUserById(userID, false)
	if err != nil {
		return err
	}

	setting := user.GetSetting()
	merged := append([]int{}, setting.ReadLoginAnnouncementIds...)
	for _, id := range ids {
		if id <= 0 || containsInt(merged, id) {
			continue
		}
		merged = append(merged, id)
	}
	setting.ReadLoginAnnouncementIds = merged
	user.SetSetting(setting)
	return user.Update(false)
}

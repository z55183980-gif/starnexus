package model

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/constant"
)

func TestListUserOpenAIVideoTasksByPlatformCursorFiltersNativeTasks(t *testing.T) {
	setupZQBAPIRegistryTestDB(t)
	platform := constant.TaskPlatform(fmt.Sprint(constant.ChannelTypeZQBAPI))
	tasks := []*Task{
		{TaskID: "openai-1", UserId: 7, Platform: platform, Properties: Properties{OpenAIVideo: true}},
		{TaskID: "native-1", UserId: 7, Platform: platform},
		{TaskID: "openai-2", UserId: 7, Platform: platform, Properties: Properties{OpenAIVideo: true}},
		{TaskID: "other-user", UserId: 8, Platform: platform, Properties: Properties{OpenAIVideo: true}},
	}
	for _, task := range tasks {
		if err := DB.Create(task).Error; err != nil {
			t.Fatal(err)
		}
	}

	listed, hasMore, err := ListUserOpenAIVideoTasksByPlatformCursor(7, platform, 0, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].TaskID != "openai-1" || !hasMore {
		t.Fatalf("first page = tasks:%+v hasMore:%v", listed, hasMore)
	}
	listed, hasMore, err = ListUserOpenAIVideoTasksByPlatformCursor(7, platform, listed[0].ID, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].TaskID != "openai-2" || hasMore {
		t.Fatalf("second page = tasks:%+v hasMore:%v", listed, hasMore)
	}
}

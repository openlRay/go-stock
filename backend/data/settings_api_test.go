package data

import "testing"

func TestFeishuBotMemorySettingRoundTrip(t *testing.T) {
	withAIConfigTestDB(t)

	config := &SettingConfig{Settings: &Settings{FeishuBotMemoryEnable: true}}
	if got := UpdateConfig(config); got != "保存成功！" {
		t.Fatalf("UpdateConfig(enable memory) = %q", got)
	}
	if got := GetSettingConfig().FeishuBotMemoryEnable; !got {
		t.Fatal("FeishuBotMemoryEnable was not persisted as true")
	}

	// 显式 false 是有效配置，必须通过字段 map 写入，不能被零值更新规则跳过。
	config.FeishuBotMemoryEnable = false
	if got := UpdateConfig(config); got != "保存成功！" {
		t.Fatalf("UpdateConfig(disable memory) = %q", got)
	}
	if got := GetSettingConfig().FeishuBotMemoryEnable; got {
		t.Fatal("FeishuBotMemoryEnable was not persisted as false")
	}
}

package setting

import (
	"encoding/json"

	"github.com/QuantumNous/new-api/common"
)

// SupportChannelsConfig is stored as JSON in options.SupportChannels.
type SupportChannelsConfig struct {
	Enabled       bool             `json:"enabled"`
	PanelTitle    string           `json:"panelTitle"`
	PanelSubtitle string           `json:"panelSubtitle"`
	Channels      []SupportChannel `json:"channels"`
}

type SupportChannel struct {
	ID           string `json:"id"`
	Label        string `json:"label"`
	Type         string `json:"type"`
	Enabled      bool   `json:"enabled"`
	URL          string `json:"url,omitempty"`
	WidgetID     string `json:"widgetId,omitempty"`
	ImageURL     string `json:"imageUrl,omitempty"`
	OpenInNewTab bool   `json:"openInNewTab,omitempty"`
}

var defaultSupportChannelsConfig = SupportChannelsConfig{
	Enabled:       true,
	PanelTitle:    "StarNexus · Online support",
	PanelSubtitle: "7×24 hour round-the-clock online customer service",
	Channels: []SupportChannel{
		{
			ID:       "chatway",
			Label:    "Online support",
			Type:     "chatway",
			Enabled:  true,
			WidgetID: "XAUFzylpFcj9",
		},
		{
			ID:           "whatsapp",
			Label:        "WhatsApp",
			Type:         "link",
			Enabled:      true,
			URL:          "https://wa.me/85255183980",
			OpenInNewTab: true,
		},
		{
			ID:           "telegram",
			Label:        "Telegram",
			Type:         "link",
			Enabled:      true,
			URL:          "https://t.me/accattc",
			OpenInNewTab: true,
		},
		{
			ID:       "wechat",
			Label:    "WeChat",
			Type:     "qrcode",
			Enabled:  false,
			ImageURL: "",
		},
		{
			ID:           "zalo",
			Label:        "Zalo",
			Type:         "link",
			Enabled:      false,
			URL:          "",
			OpenInNewTab: true,
		},
	},
}

var SupportChannels = defaultSupportChannelsConfig

func UpdateSupportChannelsByJsonString(jsonString string) error {
	if jsonString == "" {
		SupportChannels = defaultSupportChannelsConfig
		return nil
	}
	parsed := SupportChannelsConfig{}
	if err := json.Unmarshal([]byte(jsonString), &parsed); err != nil {
		return err
	}
	SupportChannels = parsed
	return nil
}

func SupportChannels2JsonString() string {
	jsonBytes, err := json.Marshal(SupportChannels)
	if err != nil {
		common.SysLog("error marshalling support channels: " + err.Error())
		return "{}"
	}
	return string(jsonBytes)
}

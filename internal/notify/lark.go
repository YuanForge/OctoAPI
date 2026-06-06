package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

const larkWebhook = "https://open.larksuite.com/open-apis/bot/v2/hook/a367d5fd-3a7c-4c73-b8ed-be22e19b4c32"

// SendLarkChannelDisabled 通知运营：渠道因余额不足被停用
func SendLarkChannelDisabled(channelName string, channelID int64, reason string) error {
	content := fmt.Sprintf(
		"渠道【%s】(ID: %d) 因余额不足已被自动停用。\n原因: %s\n请及时处理。",
		channelName, channelID, reason,
	)
	return sendLarkCard("⚠️ 渠道自动停用通知", "red", content)
}

// SendLarkUpstreamBalanceLow 通知运营：上游平台余额低于配置阈值。
func SendLarkUpstreamBalanceLow(platformName string, platformID int64, amount float64, currency string, threshold float64, syncedAt time.Time) error {
	if currency == "" {
		currency = "CNY"
	}
	content := fmt.Sprintf(
		"上游平台【%s】(ID: %d) 余额 %.4f %s，已小于等于告警阈值 %.4f %s。\n同步时间: %s\n请及时处理。",
		platformName,
		platformID,
		amount,
		currency,
		threshold,
		currency,
		syncedAt.Format("2006-01-02 15:04:05"),
	)
	return sendLarkCard("⚠️ 上游余额告警", "red", content)
}

func sendLarkCard(title, template, content string) error {
	card := map[string]interface{}{
		"msg_type": "interactive",
		"card": map[string]interface{}{
			"config": map[string]interface{}{
				"wide_screen_mode": true,
			},
			"header": map[string]interface{}{
				"template": template,
				"title": map[string]interface{}{
					"content": title,
					"tag":     "plain_text",
				},
			},
			"elements": []interface{}{
				map[string]interface{}{
					"tag": "div",
					"text": map[string]interface{}{
						"content": content,
						"tag":     "lark_md",
					},
				},
			},
		},
	}

	body, err := json.Marshal(card)
	if err != nil {
		return err
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(larkWebhook, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("Lark通知失败: %s", resp.Status)
		return fmt.Errorf("Lark通知失败: %s", resp.Status)
	}
	return nil
}

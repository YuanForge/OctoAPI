package notify

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"time"
)

const larkWebhook = "https://open.larksuite.com/open-apis/bot/v2/hook/a367d5fd-3a7c-4c73-b8ed-be22e19b4c32"

// SendLarkChannelDisabled 通知运营：渠道因余额不足被停用
func SendLarkChannelDisabled(channelName string, channelID int64, reason string) error {
	card := map[string]interface{}{
		"msg_type": "interactive",
		"card": map[string]interface{}{
			"config": map[string]interface{}{
				"wide_screen_mode": true,
			},
			"elements": []interface{}{
				map[string]interface{}{
					"alt": map[string]interface{}{
						"content": "",
						"tag":     "plain_text",
					},
					"img_key": "img_v2_bfd72a81-1533-4699-995d-12a675708d0g",
					"tag":     "img",
				},
				map[string]interface{}{
					"tag": "div",
					"text": map[string]interface{}{
						"content": "渠道【" + channelName + "】(ID: " + string(rune(channelID)) + ") 因余额不足已被自动停用。\n原因: " + reason + "\n请及时处理。",
						"tag":     "lark_md",
					},
				},
				map[string]interface{}{
					"actions": []interface{}{
						map[string]interface{}{
							"tag": "button",
							"text": map[string]interface{}{
								"content": "立即推荐好书",
								"tag":     "plain_text",
							},
							"type": "primary",
							"url":  "https://open.larksuite.com/",
						},
						map[string]interface{}{
							"tag": "button",
							"text": map[string]interface{}{
								"content": "查看活动指南",
								"tag":     "plain_text",
							},
							"type": "default",
							"url":  "https://open.larksuite.com/",
						},
					},
					"tag": "action",
				},
			},
			"header": map[string]interface{}{
				"template": "turquoise",
				"title": map[string]interface{}{
					"content": "📚晒挚爱好书，赢读书礼金",
					"tag":     "plain_text",
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
	}
	return nil
}

package feishu

import (
	"encoding/json"
	"fmt"
	"net/url"
)

const messageAPI = "https://open.feishu.cn/open-apis/im/v1/messages"

// SendCard 以应用身份往群里发一张卡片（card 是卡片 JSON 2.0 对象），返回 message_id。
// uuid 相同的请求飞书 1 小时内只发一次，重试不会发出两张。
func SendCard(chatID string, card interface{}, uuid string) (string, error) {
	token, err := GetToken()
	if err != nil {
		return "", err
	}
	content, err := json.Marshal(card)
	if err != nil {
		return "", err
	}
	body := map[string]interface{}{
		"receive_id": chatID,
		"msg_type":   "interactive",
		"content":    string(content),
	}
	if uuid != "" {
		body["uuid"] = uuid
	}
	data, err := doFeishuRequest("POST", messageAPI+"?"+url.Values{"receive_id_type": {"chat_id"}}.Encode(), body, token)
	if err != nil {
		return "", err
	}
	var sent struct {
		MessageID string `json:"message_id"`
	}
	if err := json.Unmarshal(data, &sent); err != nil || sent.MessageID == "" {
		return "", fmt.Errorf("parse send message response: %v", err)
	}
	return sent.MessageID, nil
}

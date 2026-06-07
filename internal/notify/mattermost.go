package notify

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

type MattermostNotifier struct {
	webhookURL string
	client     *http.Client
}

func NewMattermost(webhookURL string) *MattermostNotifier {
	return &MattermostNotifier{webhookURL: webhookURL, client: &http.Client{Timeout: 8 * time.Second}}
}

func (m *MattermostNotifier) Name() string { return "mattermost" }

func (m *MattermostNotifier) Send(ctx context.Context, msg Message) error {
	text := msg.Body
	if text == "" {
		text = fmt.Sprintf("%s %s — %s", msg.SeverityEmoji(), msg.AssetName, msg.Title)
	}
	return postJSON(ctx, m.client, m.webhookURL, map[string]any{"text": text})
}

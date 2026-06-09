package notify

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"time"
)

type MattermostNotifier struct {
	webhookURL string
	client     *http.Client
}

func NewMattermost(webhookURL string) *MattermostNotifier {
	// Use custom transport that skips TLS verification for self-signed certs (dev/test only)
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{
		Timeout:   10 * time.Second,
		Transport: transport,
	}
	return &MattermostNotifier{webhookURL: webhookURL, client: client}
}

func (m *MattermostNotifier) Name() string { return "mattermost" }

func (m *MattermostNotifier) Send(ctx context.Context, msg Message) error {
	text := msg.Body
	if text == "" {
		text = fmt.Sprintf("%s %s — %s", msg.SeverityEmoji(), msg.AssetName, msg.Title)
	}
	return postJSON(ctx, m.client, m.webhookURL, map[string]any{"text": text})
}

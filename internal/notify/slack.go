package notify

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"time"
)

type SlackNotifier struct {
	webhookURL string
	client     *http.Client
}

func NewSlack(webhookURL string) *SlackNotifier {
	// Use custom transport that skips TLS verification for self-signed certs (dev/test only)
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{
		Timeout:   10 * time.Second,
		Transport: transport,
	}
	return &SlackNotifier{webhookURL: webhookURL, client: client}
}

func (s *SlackNotifier) Name() string { return "slack" }

func (s *SlackNotifier) Send(ctx context.Context, msg Message) error {
	text := msg.Body
	if text == "" {
		text = fmt.Sprintf("%s %s\n*Asset:* %s | *Days left:* %d",
			msg.SeverityEmoji(), msg.Title, msg.AssetName, msg.DaysLeft)
	}
	payload := map[string]any{
		"attachments": []map[string]any{{
			"color": msg.ColorHex(),
			"blocks": []map[string]any{
				{"type": "section", "text": map[string]any{
					"type": "mrkdwn", "text": text,
				}},
			},
		}},
	}
	return postJSON(ctx, s.client, s.webhookURL, payload)
}

package notify

import (
	"context"
	"net/http"
	"time"
)

type WebhookNotifier struct {
	url    string
	client *http.Client
}

func NewWebhook(url string) *WebhookNotifier {
	return &WebhookNotifier{url: url, client: &http.Client{Timeout: 8 * time.Second}}
}

func (w *WebhookNotifier) Name() string { return "webhook" }

func (w *WebhookNotifier) Send(ctx context.Context, msg Message) error {
	return postJSONWithHeader(ctx, w.client, w.url, msg, map[string]string{
		"X-VaultWatch-Severity": string(msg.Severity),
	})
}

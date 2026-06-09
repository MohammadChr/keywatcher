package notify

import (
	"context"
	"crypto/tls"
	"net/http"
	"time"
)

type WebhookNotifier struct {
	url    string
	client *http.Client
}

func NewWebhook(url string) *WebhookNotifier {
	// Use custom transport that skips TLS verification for self-signed certs (dev/test only)
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{
		Timeout:   10 * time.Second,
		Transport: transport,
	}
	return &WebhookNotifier{url: url, client: client}
}

func (w *WebhookNotifier) Name() string { return "webhook" }

func (w *WebhookNotifier) Send(ctx context.Context, msg Message) error {
	return postJSONWithHeader(ctx, w.client, w.url, msg, map[string]string{
		"X-VaultWatch-Severity": string(msg.Severity),
	})
}

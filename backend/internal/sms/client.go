// Package sms sends text messages through Africa's Talking, which covers most African networks.
// Without an API key it runs in test mode and writes messages to the server log instead.
package sms

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Config struct {
	Env      string // sandbox | production
	Username string
	APIKey   string
	SenderID string // optional alphanumeric sender ID approved by Africa's Talking
}

type Client struct {
	cfg  Config
	url  string
	http *http.Client
	log  *slog.Logger
}

func New(cfg Config, log *slog.Logger) *Client {
	u := "https://api.sandbox.africastalking.com/version1/messaging"
	if cfg.Env == "production" {
		u = "https://api.africastalking.com/version1/messaging"
	}
	return &Client{cfg: cfg, url: u, http: &http.Client{Timeout: 15 * time.Second}, log: log}
}

func (c *Client) TestMode() bool { return c.cfg.APIKey == "" || c.cfg.Username == "" }

// Send delivers one message to an international-format number (e.g. 2547XXXXXXXX).
func (c *Client) Send(ctx context.Context, phone, message string) error {
	to := "+" + strings.TrimPrefix(phone, "+")
	if c.TestMode() {
		c.log.Warn("TEST SMS (not sent: add Africa's Talking keys to .env)", "to", to, "message", message)
		return nil
	}
	form := url.Values{}
	form.Set("username", c.cfg.Username)
	form.Set("to", to)
	form.Set("message", message)
	if c.cfg.SenderID != "" {
		form.Set("from", c.cfg.SenderID)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("apiKey", c.cfg.APIKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var out struct {
		SMSMessageData struct {
			Message    string `json:"Message"`
			Recipients []struct {
				Status     string `json:"status"`
				StatusCode int    `json:"statusCode"`
			} `json:"Recipients"`
		} `json:"SMSMessageData"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return fmt.Errorf("sms: unexpected response (%d)", resp.StatusCode)
	}
	rs := out.SMSMessageData.Recipients
	if len(rs) == 0 || (rs[0].StatusCode != 100 && rs[0].StatusCode != 101 && rs[0].StatusCode != 102) {
		return fmt.Errorf("sms not accepted: %s", out.SMSMessageData.Message)
	}
	return nil
}

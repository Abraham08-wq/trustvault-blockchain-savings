// Package risk asks the AI service to score withdrawal requests, with a local rules fallback.
package risk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type Request struct {
	GroupID              string  `json:"group_id"`
	Amount               int64   `json:"amount"`
	GroupBalance         int64   `json:"group_balance"`
	RequesterRole        string  `json:"requester_role"`
	RequesterTenureDays  float64 `json:"requester_tenure_days"`
	RequesterContributed int64   `json:"requester_total_contributed"`
	AvgWithdrawal        float64 `json:"group_avg_withdrawal"`
	WithdrawalsLast7Days int     `json:"withdrawals_last_7_days"`
	RecipientIsRequester bool    `json:"recipient_is_requester"`
	RecipientIsMember    bool    `json:"recipient_is_member"`
	HourOfDay            int     `json:"hour_of_day"`
}

type Result struct {
	Score   float64  `json:"score"`
	Level   string   `json:"level"` // low | medium | high
	Reasons []string `json:"reasons"`
	Source  string   `json:"source"` // ai | fallback
}

type Client struct {
	baseURL string
	http    *http.Client
}

func NewClient(baseURL string) *Client {
	return &Client{baseURL: baseURL, http: &http.Client{Timeout: 3 * time.Second}}
}

// Score never fails: if the AI service is down, conservative local rules are used.
func (c *Client) Score(ctx context.Context, req Request) Result {
	res, err := c.callAI(ctx, req)
	if err != nil {
		r := Fallback(req)
		r.Reasons = append(r.Reasons, "AI service unavailable; rules-only check used")
		return r
	}
	res.Source = "ai"
	return res
}

func (c *Client) callAI(ctx context.Context, req Request) (Result, error) {
	body, _ := json.Marshal(req)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/score", bytes.NewReader(body))
	if err != nil {
		return Result{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("ai service status %d", resp.StatusCode)
	}
	var out Result
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return Result{}, err
	}
	if out.Level != "low" && out.Level != "medium" && out.Level != "high" {
		return Result{}, fmt.Errorf("invalid level %q", out.Level)
	}
	return out, nil
}

// Healthy reports whether the AI service answers its health check.
func (c *Client) Healthy(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/health", nil)
	if err != nil {
		return false
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// Fallback applies simple, conservative rules.
func Fallback(r Request) Result {
	score := 0.0
	var reasons []string
	if r.GroupBalance > 0 {
		share := float64(r.Amount) / float64(r.GroupBalance)
		if share >= 0.8 {
			score += 0.5
			reasons = append(reasons, "withdrawal is 80% or more of the group balance")
		} else if share >= 0.5 {
			score += 0.3
			reasons = append(reasons, "withdrawal is half or more of the group balance")
		}
	}
	if r.RecipientIsRequester && (r.RequesterRole == "treasurer" || r.RequesterRole == "chair") {
		score += 0.25
		reasons = append(reasons, "official is paying money to themselves")
	}
	if !r.RecipientIsMember {
		score += 0.2
		reasons = append(reasons, "recipient is not a group member")
	}
	if r.WithdrawalsLast7Days >= 3 {
		score += 0.2
		reasons = append(reasons, "many withdrawals in the last 7 days")
	}
	if r.HourOfDay < 5 || r.HourOfDay >= 23 {
		score += 0.1
		reasons = append(reasons, "requested at an unusual hour")
	}
	if score > 1 {
		score = 1
	}
	level := "low"
	switch {
	case score >= 0.6:
		level = "high"
	case score >= 0.3:
		level = "medium"
	}
	if reasons == nil {
		reasons = []string{}
	}
	return Result{Score: score, Level: level, Reasons: reasons, Source: "fallback"}
}

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type InfraiClient struct {
	baseURL string
	key     string
	http    *http.Client
	sleep   func(time.Duration)
}

type InfraiError struct {
	Status int
	Code   string
	Detail any
}

func (e *InfraiError) Error() string { return fmt.Sprintf("Infrai %s (HTTP %d)", e.Code, e.Status) }

type envelope struct {
	OK       bool            `json:"ok"`
	Data     json.RawMessage `json:"data"`
	Error    json.RawMessage `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

func newInfraiClient(baseURL, key string) *InfraiClient {
	return &InfraiClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		key:     key,
		http:    &http.Client{Timeout: 10 * time.Second},
		sleep:   time.Sleep,
	}
}

func (c *InfraiClient) createChannel(ctx context.Context, channel, requestID string) error {
	return c.post(ctx, "/v1/realtime/channel/create", map[string]any{
		"channel": channel,
	}, requestID, nil)
}

func (c *InfraiClient) deleteChannel(ctx context.Context, channel, requestID string) error {
	return c.request(ctx, http.MethodDelete, "/v1/realtime/channel/delete/"+url.PathEscape(channel), nil, requestID, nil)
}

func (c *InfraiClient) channelExists(ctx context.Context, channel string) (bool, error) {
	err := c.request(ctx, http.MethodGet, "/v1/realtime/channel/get/"+url.PathEscape(channel), nil, "", nil)
	var infraiErr *InfraiError
	if errors.As(err, &infraiErr) && infraiErr.Status == http.StatusNotFound {
		return false, nil
	}
	return err == nil, err
}

func (c *InfraiClient) recordMetrics(ctx context.Context, snapshot GameSnapshot, requestID string) error {
	return c.post(ctx, "/v1/metrics/batch", map[string]any{
		"points":          pointsFor(snapshot),
		"idempotency_key": requestID + "-metrics",
	}, requestID+"-metrics", nil)
}

func (c *InfraiClient) publishFrame(ctx context.Context, channel string, frame DashboardFrame, requestID string) error {
	return c.post(ctx, "/v1/realtime/publish", map[string]any{
		"channel":    channel,
		"event":      "game.ops.snapshot",
		"data":       frame,
		"account_id": frame.AccountID,
	}, requestID+"-publish", nil)
}

func (c *InfraiClient) issueDashboardToken(ctx context.Context, channel, clientID string) (json.RawMessage, error) {
	var data json.RawMessage
	err := c.post(ctx, "/v1/realtime/token/issue", map[string]any{
		"client_id":    clientID,
		"channels":     []string{channel},
		"capabilities": []string{"subscribe"},
		"ttl_seconds":  900,
	}, clientID+"-token", &data)
	return data, err
}

func (c *InfraiClient) post(ctx context.Context, path string, body any, requestID string, out *json.RawMessage) error {
	return c.request(ctx, http.MethodPost, path, body, requestID, out)
}

func (c *InfraiClient) request(ctx context.Context, method, path string, body any, requestID string, out *json.RawMessage) error {
	var payload []byte
	var err error
	if body != nil {
		payload, err = json.Marshal(body)
	}
	if err != nil {
		return err
	}
	for attempt := 0; attempt < 4; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.key)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if requestID != "" {
			req.Header.Set("Idempotency-Key", requestID)
		}

		res, err := c.http.Do(req)
		if err != nil {
			return err
		}
		raw, readErr := io.ReadAll(res.Body)
		res.Body.Close()
		if readErr != nil {
			return readErr
		}

		var env envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			return fmt.Errorf("decode Infrai envelope: %w", err)
		}
		if res.StatusCode == http.StatusTooManyRequests && attempt < 3 {
			c.sleep(retryDelay(res.Header.Get("Retry-After"), attempt))
			continue
		}
		if !env.OK {
			var detail map[string]any
			if err := json.Unmarshal(env.Error, &detail); err != nil {
				return fmt.Errorf("decode Infrai error: %w", err)
			}
			code, _ := detail["code"].(string)
			return &InfraiError{Status: res.StatusCode, Code: code, Detail: detail}
		}
		if res.StatusCode >= 500 {
			return fmt.Errorf("Infrai transport status %d", res.StatusCode)
		}
		if out != nil {
			*out = env.Data
		}
		return nil
	}
	return errors.New("Infrai retry budget exhausted")
}

func retryDelay(header string, attempt int) time.Duration {
	if seconds, err := strconv.Atoi(header); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	return time.Duration(1<<attempt) * 250 * time.Millisecond
}

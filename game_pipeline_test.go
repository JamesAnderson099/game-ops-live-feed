package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

func TestFrameStatus(t *testing.T) {
	tests := []struct {
		name     string
		events   int
		queue    int
		expected string
	}{
		{name: "balanced flow", events: 80, queue: 25, expected: "nominal"},
		{name: "queue exceeds absolute threshold", events: 200, queue: 101, expected: "attention"},
		{name: "queue overwhelms event volume", events: 20, queue: 41, expected: "attention"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := frameFor(GameSnapshot{LiveEvents: tt.events, ModerationQueue: tt.queue})
			if got.Status != tt.expected {
				t.Fatalf("status = %q, want %q", got.Status, tt.expected)
			}
		})
	}
}

func TestSnapshotWritesMetricsBeforePublishing(t *testing.T) {
	var paths []string
	var authorizations []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		authorizations = append(authorizations, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "data": map[string]any{"accepted": true}})
	}))
	defer upstream.Close()

	client := newInfraiClient(upstream.URL, "test-key")
	client.http = upstream.Client()
	snapshot := GameSnapshot{AccountID: "studio-7", PlayerAssets: 12, LiveEvents: 9, ModerationQueue: 3, CapturedAt: time.Unix(1_700_000_000, 0)}
	if err := client.recordMetrics(context.Background(), snapshot, "batch-17"); err != nil {
		t.Fatal(err)
	}
	if err := client.publishFrame(context.Background(), channel, frameFor(snapshot), "batch-17"); err != nil {
		t.Fatal(err)
	}

	if want := []string{"/v1/metrics/batch", "/v1/realtime/publish"}; !reflect.DeepEqual(paths, want) {
		t.Fatalf("paths = %v, want %v", paths, want)
	}
	if want := []string{"Bearer test-key", "Bearer test-key"}; !reflect.DeepEqual(authorizations, want) {
		t.Fatalf("authorization headers = %v, want same key twice", authorizations)
	}
}

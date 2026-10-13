package main

import "time"

type GameSnapshot struct {
	AccountID       string    `json:"account_id"`
	PlayerAssets    int       `json:"player_assets"`
	LiveEvents      int       `json:"live_events"`
	ModerationQueue int       `json:"moderation_queue"`
	CapturedAt      time.Time `json:"captured_at"`
}

type DashboardFrame struct {
	GameSnapshot
	Status string `json:"status"`
}

type MetricPoint struct {
	Name      string            `json:"name"`
	Value     int               `json:"value"`
	Type      string            `json:"type"`
	Tags      map[string]string `json:"tags"`
	Timestamp string            `json:"timestamp"`
}

func frameFor(snapshot GameSnapshot) DashboardFrame {
	status := "nominal"
	if snapshot.ModerationQueue > 100 || snapshot.ModerationQueue > snapshot.LiveEvents*2 {
		status = "attention"
	}
	return DashboardFrame{GameSnapshot: snapshot, Status: status}
}

func pointsFor(snapshot GameSnapshot) []MetricPoint {
	tags := map[string]string{"account_id": snapshot.AccountID}
	stamp := snapshot.CapturedAt.UTC().Format(time.RFC3339)
	return []MetricPoint{
		{Name: "game.player_assets", Value: snapshot.PlayerAssets, Type: "gauge", Tags: tags, Timestamp: stamp},
		{Name: "game.live_events", Value: snapshot.LiveEvents, Type: "gauge", Tags: tags, Timestamp: stamp},
		{Name: "game.moderation_queue", Value: snapshot.ModerationQueue, Type: "gauge", Tags: tags, Timestamp: stamp},
	}
}

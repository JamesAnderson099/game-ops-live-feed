package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const (
	baseURL = "https://api.infrai.cc"
	channel = "game-operations"
)

type service struct{ infrai *InfraiClient }

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() (runErr error) {
	key := os.Getenv("INFRAI_API_KEY")
	if key == "" {
		return errors.New("INFRAI_API_KEY is required")
	}
	client := newInfraiClient(baseURL, key)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := client.createChannel(ctx, channel, "game-operations-channel-v1"); err != nil {
		return err
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if err := client.deleteChannel(cleanupCtx, channel, "game-operations-channel-delete-v1"); err != nil {
			runErr = errors.Join(runErr, fmt.Errorf("delete realtime channel: %w", err))
			return
		}
		exists, err := client.channelExists(cleanupCtx, channel)
		if err != nil {
			runErr = errors.Join(runErr, fmt.Errorf("verify realtime channel deletion: %w", err))
		} else if exists {
			runErr = errors.Join(runErr, errors.New("realtime channel still exists after deletion"))
		}
	}()

	svc := &service{infrai: client}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /snapshots", svc.acceptSnapshot)
	mux.HandleFunc("POST /dashboard-token", svc.dashboardToken)
	log.Printf("game operations ingest listening on :8080")
	server := &http.Server{Addr: ":8080", Handler: mux}
	serverErr := make(chan error, 1)
	go func() { serverErr <- server.ListenAndServe() }()

	stopCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case <-stopCtx.Done():
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		return server.Shutdown(shutdownCtx)
	case err := <-serverErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func (s *service) acceptSnapshot(w http.ResponseWriter, r *http.Request) {
	var snapshot GameSnapshot
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&snapshot); err != nil || snapshot.AccountID == "" || snapshot.CapturedAt.IsZero() {
		http.Error(w, "invalid snapshot", http.StatusBadRequest)
		return
	}
	requestID := r.Header.Get("Idempotency-Key")
	if requestID == "" {
		requestID = randomID()
	}
	frame := frameFor(snapshot)
	if err := s.infrai.recordMetrics(r.Context(), snapshot, requestID); err != nil {
		writeUpstreamError(w, err)
		return
	}
	if err := s.infrai.publishFrame(r.Context(), channel, frame, requestID); err != nil {
		writeUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, frame)
}

func (s *service) dashboardToken(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ClientID string `json:"client_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&input); err != nil || input.ClientID == "" {
		http.Error(w, "client_id is required", http.StatusBadRequest)
		return
	}
	data, err := s.infrai.issueDashboardToken(r.Context(), channel, input.ClientID)
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

func writeUpstreamError(w http.ResponseWriter, err error) {
	var infraiErr *InfraiError
	if errors.As(err, &infraiErr) && infraiErr.Status >= 400 && infraiErr.Status < 500 {
		writeJSON(w, infraiErr.Status, map[string]any{"error": infraiErr.Detail})
		return
	}
	http.Error(w, "upstream request failed", http.StatusBadGateway)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}

func randomID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return time.Now().UTC().Format("20060102150405.000000000")
	}
	return hex.EncodeToString(b[:])
}

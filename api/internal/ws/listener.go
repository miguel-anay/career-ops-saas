package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// notifyPayload is the minimal structure of the NOTIFY payload from workers.
// Full envelope: {"event": "...", "scan_run_id": "...", "ts": "...", "data": {...}}
type notifyPayload struct {
	Event     string          `json:"event"`
	ScanRunID string          `json:"scan_run_id"`
	Ts        string          `json:"ts"`
	Data      json.RawMessage `json:"data"`
}

// StartListener acquires a dedicated pgx connection (not from pool — LISTEN needs
// a persistent connection) and starts a goroutine that listens on the scan_progress
// channel. On NOTIFY, it parses the payload and calls hub.Broadcast.
//
// Reconnects on disconnect with exponential backoff. Returns nil immediately; the
// listener runs as a background goroutine.
func StartListener(ctx context.Context, pool *pgxpool.Pool, hub *Hub) error {
	go func() {
		var conn *pgx.Conn
		var err error

		backoff := time.Second
		const maxBackoff = 30 * time.Second

		for {
			select {
			case <-ctx.Done():
				if conn != nil {
					conn.Close(ctx)
				}
				return
			default:
			}

			// Acquire a dedicated connection (not from pool — LISTEN is per-connection).
			connStr := pool.Config().ConnString()
			conn, err = pgx.Connect(ctx, connStr)
			if err != nil {
				slog.Error("ws listener: connect failed, retrying", "error", err, "backoff", backoff)
				select {
				case <-ctx.Done():
					return
				case <-time.After(backoff):
				}
				backoff = min(backoff*2, maxBackoff)
				continue
			}

			slog.Info("ws listener: connected, listening on scan_progress")
			backoff = time.Second // reset on successful connect

			if _, err = conn.Exec(ctx, "LISTEN scan_progress"); err != nil {
				slog.Error("ws listener: LISTEN failed", "error", err)
				conn.Close(ctx)
				continue
			}

			if err = listenLoop(ctx, conn, hub); err != nil {
				slog.Error("ws listener: loop exited, reconnecting", "error", err)
			}
			conn.Close(ctx)
		}
	}()

	return nil
}

// listenLoop blocks, waiting for notifications on the scan_progress channel.
// Returns when the context is cancelled or the connection is lost.
func listenLoop(ctx context.Context, conn *pgx.Conn, hub *Hub) error {
	for {
		notification, err := conn.WaitForNotification(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil // context cancelled — clean exit
			}
			return fmt.Errorf("wait for notification: %w", err)
		}

		var payload notifyPayload
		if err := json.Unmarshal([]byte(notification.Payload), &payload); err != nil {
			slog.Warn("ws listener: failed to parse NOTIFY payload", "error", err, "raw", notification.Payload)
			continue
		}

		scanRunID, err := uuid.Parse(payload.ScanRunID)
		if err != nil {
			slog.Warn("ws listener: invalid scan_run_id in NOTIFY payload", "error", err, "raw", payload.ScanRunID)
			continue
		}

		// Pass the raw NOTIFY payload directly — no re-wrapping needed.
		// The worker already sends the correct envelope format.
		hub.Broadcast(scanRunID, []byte(notification.Payload))
	}
}

func min(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

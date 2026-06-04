package ws

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/miguel-anay/career-ops-saas/api/internal/auth"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		// Origin validation is handled by CORS middleware globally.
		// For WebSocket upgrade, allow all origins here (trust CORS middleware).
		return true
	},
}

// ScanProgressHandler returns an HTTP handler that upgrades connections to WebSocket
// and streams scan progress events for a given scan_run_id.
//
// Query params:
//   - token: JWT access token (Authorization header not supported during WS upgrade)
//   - scan_run_id: UUID of the scan run to subscribe to
//
// GET /ws/scan?token=<jwt>&scan_run_id=<uuid>
func ScanProgressHandler(hub *Hub, jwtSecret string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Validate JWT from query param (headers are not reliable during WS upgrades).
		tokenStr := r.URL.Query().Get("token")
		if tokenStr == "" {
			http.Error(w, `{"error":"missing token"}`, http.StatusUnauthorized)
			return
		}

		claims, err := auth.VerifyToken(tokenStr, jwtSecret)
		if err != nil {
			http.Error(w, `{"error":"invalid or expired token"}`, http.StatusUnauthorized)
			return
		}
		_ = claims // user_id available for future per-user authorization

		scanRunIDStr := r.URL.Query().Get("scan_run_id")
		if scanRunIDStr == "" {
			http.Error(w, `{"error":"missing scan_run_id"}`, http.StatusBadRequest)
			return
		}

		scanRunID, err := uuid.Parse(scanRunIDStr)
		if err != nil {
			http.Error(w, `{"error":"invalid scan_run_id"}`, http.StatusBadRequest)
			return
		}

		// Upgrade to WebSocket.
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			slog.Error("ws upgrade failed", "error", err)
			return
		}
		defer conn.Close()

		connID := uuid.New().String()
		ch := make(chan []byte, 64)

		hub.Register(scanRunID, connID, ch)
		defer hub.Unregister(scanRunID, connID)

		// Pump messages from channel to the WebSocket client.
		for {
			select {
			case msg, ok := <-ch:
				if !ok {
					// Channel was closed by hub (scan run completed or connection evicted).
					return
				}
				conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
				if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
					slog.Debug("ws write failed", "error", err, "conn_id", connID)
					return
				}

			case <-r.Context().Done():
				return
			}
		}
	}
}

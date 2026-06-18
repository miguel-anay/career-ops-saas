package ws_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/miguel-anay/career-ops-saas/api/internal/ws"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// startHub creates a Hub, runs it in the background, and returns a cancel func.
func startHub(t *testing.T) (*ws.Hub, context.CancelFunc) {
	t.Helper()
	hub := ws.NewHub()
	ctx, cancel := context.WithCancel(context.Background())
	go hub.Run(ctx)
	// Give the hub goroutine a moment to start selecting.
	time.Sleep(10 * time.Millisecond)
	return hub, cancel
}

func TestHub_RegisterAndBroadcast(t *testing.T) {
	hub, cancel := startHub(t)
	defer cancel()

	scanRunID := uuid.New()
	connID := "conn-1"
	ch := make(chan []byte, 4)

	hub.Register(scanRunID, connID, ch)
	// Give the hub time to process the register message.
	time.Sleep(10 * time.Millisecond)

	payload := []byte(`{"event":"scan.job_found"}`)
	hub.Broadcast(scanRunID, payload)

	select {
	case got := <-ch:
		assert.Equal(t, payload, got)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for broadcast message")
	}
}

func TestHub_UnregisterRemovesClient(t *testing.T) {
	hub, cancel := startHub(t)
	defer cancel()

	scanRunID := uuid.New()
	connID := "conn-2"
	ch := make(chan []byte, 4)

	hub.Register(scanRunID, connID, ch)
	time.Sleep(10 * time.Millisecond)

	hub.Unregister(scanRunID, connID)
	time.Sleep(10 * time.Millisecond)

	// Broadcast after unregister — should not panic and channel should not receive.
	require.NotPanics(t, func() {
		hub.Broadcast(scanRunID, []byte(`{"event":"scan.completed"}`))
		time.Sleep(20 * time.Millisecond)
	})

	// Channel was closed by hub on unregister — it should be drained/closed already.
	// Either closed (select returns zero value, ok=false) or empty.
	select {
	case _, ok := <-ch:
		// Channel was closed by hub — ok == false is expected.
		assert.False(t, ok, "channel should be closed after unregister")
	default:
		// Channel is empty and open — also acceptable (message never sent).
	}
}

func TestHub_BroadcastToCorrectUser(t *testing.T) {
	hub, cancel := startHub(t)
	defer cancel()

	scanRunA := uuid.New()
	scanRunB := uuid.New()

	chA := make(chan []byte, 4)
	chB := make(chan []byte, 4)

	hub.Register(scanRunA, "conn-a", chA)
	hub.Register(scanRunB, "conn-b", chB)
	time.Sleep(10 * time.Millisecond)

	payloadA := []byte(`{"event":"scan.job_found","run_id":"a"}`)
	hub.Broadcast(scanRunA, payloadA)

	// Only chA should receive the message.
	select {
	case got := <-chA:
		assert.Equal(t, payloadA, got)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for message on chA")
	}

	// chB should not receive anything.
	select {
	case msg := <-chB:
		t.Fatalf("chB should not receive a message, got: %s", msg)
	case <-time.After(50 * time.Millisecond):
		// Expected — no message for B.
	}
}

func TestHub_BroadcastUnknownScanRun(t *testing.T) {
	hub, cancel := startHub(t)
	defer cancel()

	unknownID := uuid.New()

	require.NotPanics(t, func() {
		hub.Broadcast(unknownID, []byte(`{"event":"scan.completed"}`))
		time.Sleep(20 * time.Millisecond)
	})
}

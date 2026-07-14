package middleware

import (
	"bufio"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeHijackableWriter implements http.ResponseWriter + http.Hijacker so
// tests can verify Hijack() is forwarded through the logging wrapper.
type fakeHijackableWriter struct {
	http.ResponseWriter
	hijacked bool
}

func (f *fakeHijackableWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	f.hijacked = true
	return nil, nil, nil
}

// TestResponseWriter_ImplementsHijacker proves issue #68: the wrapper must
// satisfy http.Hijacker at compile time, or any WebSocket upgrade behind
// this middleware fails with "response does not implement http.Hijacker".
func TestResponseWriter_ImplementsHijacker(t *testing.T) {
	var _ http.Hijacker = (*responseWriter)(nil)
}

func TestResponseWriter_HijackForwardsToUnderlying(t *testing.T) {
	fake := &fakeHijackableWriter{ResponseWriter: httptest.NewRecorder()}
	wrapped := &responseWriter{ResponseWriter: fake, status: http.StatusOK}

	_, _, err := wrapped.Hijack()
	require.NoError(t, err)
	assert.True(t, fake.hijacked, "Hijack() must forward to the underlying ResponseWriter's Hijacker")
}

func TestResponseWriter_HijackErrorsWhenUnderlyingNotHijackable(t *testing.T) {
	wrapped := &responseWriter{ResponseWriter: httptest.NewRecorder(), status: http.StatusOK}

	_, _, err := wrapped.Hijack()
	require.Error(t, err)
}

// TestLogger_PreservesHijackerThroughMiddleware proves the real regression:
// a handler running behind Logger (as /ws/scan does in main.go) must still
// see an http.Hijacker on the ResponseWriter it receives.
func TestLogger_PreservesHijackerThroughMiddleware(t *testing.T) {
	var gotHijacker bool
	handler := Logger(slog.Default())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, gotHijacker = w.(http.Hijacker)
	}))

	fake := &fakeHijackableWriter{ResponseWriter: httptest.NewRecorder()}
	handler.ServeHTTP(fake, httptest.NewRequest(http.MethodGet, "/ws/scan", nil))

	assert.True(t, gotHijacker, "handlers wrapped by Logger must still see an http.Hijacker for WS upgrades")
}

package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/miguel-anay/career-ops-saas/api/internal/auth"
)

type contextKey string

const userIDKey contextKey = "user_id"

// Authenticator returns middleware that validates the Bearer JWT from the
// Authorization header and stores the user_id UUID in the request context.
// Returns 401 with a JSON error if the token is missing, invalid, or expired.
func Authenticator(jwtSecret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
				respondUnauthorized(w, "missing or malformed authorization header")
				return
			}

			tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
			claims, err := auth.VerifyToken(tokenStr, jwtSecret)
			if err != nil {
				respondUnauthorized(w, "invalid or expired token")
				return
			}

			userID, err := uuid.Parse(claims.UserID)
			if err != nil {
				respondUnauthorized(w, "invalid user_id in token")
				return
			}

			ctx := context.WithValue(r.Context(), userIDKey, userID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetUserID retrieves the user UUID from the context set by the Authenticator middleware.
func GetUserID(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(userIDKey).(uuid.UUID)
	return id, ok
}

// SetUserID stores a userID in the context. Used in tests to simulate authenticated requests.
func SetUserID(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, userIDKey, id)
}

func respondUnauthorized(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

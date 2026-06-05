package auth

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testSecret = "test-secret-key-for-unit-tests"
const testRefreshSecret = "test-refresh-secret-key-for-unit-tests"

func TestIssueAccessToken_RoundTrip(t *testing.T) {
	userID := uuid.New().String()
	plan := "free"

	token, err := IssueAccessToken(userID, plan, testSecret)
	require.NoError(t, err)
	require.NotEmpty(t, token)

	claims, err := VerifyToken(token, testSecret)
	require.NoError(t, err)
	assert.Equal(t, userID, claims.UserID)
	assert.Equal(t, plan, claims.Plan)
	assert.WithinDuration(t, time.Now().Add(time.Hour), claims.ExpiresAt.Time, 5*time.Second)
}

func TestIssueRefreshToken_RoundTrip(t *testing.T) {
	userID := uuid.New().String()

	token, err := IssueRefreshToken(userID, testRefreshSecret)
	require.NoError(t, err)
	require.NotEmpty(t, token)

	claims, err := VerifyToken(token, testRefreshSecret)
	require.NoError(t, err)
	assert.Equal(t, userID, claims.UserID)
	assert.WithinDuration(t, time.Now().Add(7*24*time.Hour), claims.ExpiresAt.Time, 5*time.Second)
}

func TestVerifyToken_WrongSecret(t *testing.T) {
	userID := uuid.New().String()
	token, err := IssueAccessToken(userID, "free", testSecret)
	require.NoError(t, err)

	_, err = VerifyToken(token, "wrong-secret")
	assert.Error(t, err)
}

func TestVerifyToken_InvalidToken(t *testing.T) {
	_, err := VerifyToken("not.a.valid.jwt", testSecret)
	assert.Error(t, err)
}

func TestVerifyToken_EmptyToken(t *testing.T) {
	_, err := VerifyToken("", testSecret)
	assert.Error(t, err)
}

func TestIssueAccessToken_DifferentPlans(t *testing.T) {
	plans := []string{"free", "pro", "unlimited"}
	userID := uuid.New().String()

	for _, plan := range plans {
		t.Run(plan, func(t *testing.T) {
			token, err := IssueAccessToken(userID, plan, testSecret)
			require.NoError(t, err)

			claims, err := VerifyToken(token, testSecret)
			require.NoError(t, err)
			assert.Equal(t, plan, claims.Plan)
		})
	}
}

func TestTokensAreDistinct(t *testing.T) {
	userID := uuid.New().String()

	access, err := IssueAccessToken(userID, "free", testSecret)
	require.NoError(t, err)

	refresh, err := IssueRefreshToken(userID, testRefreshSecret)
	require.NoError(t, err)

	// Cross-validation must fail: access token should not verify with refresh secret and vice versa.
	_, err = VerifyToken(access, testRefreshSecret)
	assert.Error(t, err, "access token must not be valid with refresh secret")

	_, err = VerifyToken(refresh, testSecret)
	assert.Error(t, err, "refresh token must not be valid with access secret")
}

package digest

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// T-302: validation must short-circuit BEFORE platform.WithTenantTx ever
// touches the pool. A nil pool proves no DB call was attempted — a real
// pool dereference inside WithTenantTx would panic instead of returning
// a clean validation error.

func TestCreateDigest_EmptyTitle(t *testing.T) {
	svc := NewService(nil)
	_, err := svc.CreateDigest(context.Background(), uuid.New(), "", "some content")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "title")
}

func TestCreateDigest_EmptyContentMd(t *testing.T) {
	svc := NewService(nil)
	_, err := svc.CreateDigest(context.Background(), uuid.New(), "My Project", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "content_md")
}

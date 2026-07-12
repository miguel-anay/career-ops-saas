package profile

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---- T-280/281: mergeProfile ----

func TestMergeProfile_OverrideKeyReplacesWholeTopLevelKey(t *testing.T) {
	base := []byte(`{"target_roles":{"primary":["Backend Engineer"]},"narrative":"old"}`)
	overrides := []byte(`{"target_roles":{"primary":["Staff Engineer"]}}`)

	merged, err := mergeProfile(base, overrides)
	require.NoError(t, err)

	require.Contains(t, merged, "target_roles")
	assert.JSONEq(t, `{"primary":["Staff Engineer"]}`, string(merged["target_roles"]))
}

func TestMergeProfile_NonOverriddenKeysPassThrough(t *testing.T) {
	base := []byte(`{"target_roles":{"primary":["Backend Engineer"]},"narrative":"old narrative"}`)
	overrides := []byte(`{"target_roles":{"primary":["Staff Engineer"]}}`)

	merged, err := mergeProfile(base, overrides)
	require.NoError(t, err)

	require.Contains(t, merged, "narrative")
	assert.JSONEq(t, `"old narrative"`, string(merged["narrative"]))
}

func TestMergeProfile_EmptyNilInputsDoNotPanic(t *testing.T) {
	merged, err := mergeProfile(nil, nil)
	require.NoError(t, err)
	assert.Empty(t, merged)

	merged, err = mergeProfile([]byte(`{}`), []byte(`{}`))
	require.NoError(t, err)
	assert.Empty(t, merged)

	merged, err = mergeProfile([]byte(`{"a":1}`), nil)
	require.NoError(t, err)
	assert.Contains(t, merged, "a")
}

// ---- T-282/283: fieldPath allowlist ----

func TestApplyOverride_RejectsFieldPathOutsideAllowlist(t *testing.T) {
	svc := NewService(nil)
	_, err := svc.ApplyOverride(context.Background(), uuid.New(), "not_a_real_field", json.RawMessage(`{}`))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidFieldPath)
}

func TestAllowedFieldPaths_MatchesDesignAllowlist(t *testing.T) {
	for _, fp := range []string{"target_roles", "salary_target", "narrative", "candidate", "deal_breakers", "comp_targets"} {
		assert.True(t, isAllowedFieldPath(fp), "field path %q must be allowlisted", fp)
	}
	assert.False(t, isAllowedFieldPath("not_a_real_field"))
	assert.False(t, isAllowedFieldPath(""))
}

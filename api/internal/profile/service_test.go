package profile

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sqlc-dev/pqtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/miguel-anay/career-ops-saas/api/internal/db"
)

// ---- T-280/281: mergeProfile ----

func TestMergeProfile_OverrideKeyReplacesWholeTopLevelKey(t *testing.T) {
	base := []byte(`{"target_roles":{"primary":["Backend Engineer"]},"narrative":"old"}`)
	overrides := []byte(`{"target_roles":{"primary":["Staff Engineer"]}}`)

	merged := mergeProfile(base, overrides)

	require.Contains(t, merged, "target_roles")
	assert.JSONEq(t, `{"primary":["Staff Engineer"]}`, string(merged["target_roles"]))
}

func TestMergeProfile_NonOverriddenKeysPassThrough(t *testing.T) {
	base := []byte(`{"target_roles":{"primary":["Backend Engineer"]},"narrative":"old narrative"}`)
	overrides := []byte(`{"target_roles":{"primary":["Staff Engineer"]}}`)

	merged := mergeProfile(base, overrides)

	require.Contains(t, merged, "narrative")
	assert.JSONEq(t, `"old narrative"`, string(merged["narrative"]))
}

func TestMergeProfile_EmptyNilInputsDoNotPanic(t *testing.T) {
	merged := mergeProfile(nil, nil)
	assert.Empty(t, merged)

	merged = mergeProfile([]byte(`{}`), []byte(`{}`))
	assert.Empty(t, merged)

	merged = mergeProfile([]byte(`{"a":1}`), nil)
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
	for _, fp := range []string{"target_roles", "salary_target", "narrative", "candidate", "deal_breakers", "comp_targets", "scoring_rules"} {
		assert.True(t, isAllowedFieldPath(fp), "field path %q must be allowlisted", fp)
	}
	assert.False(t, isAllowedFieldPath("not_a_real_field"))
	assert.False(t, isAllowedFieldPath(""))
}

// ---- ProfileEditView JSON shape: old_value/new_value must serialize as
// plain JSON values, not the sqlc pqtype.NullRawMessage/sql.NullTime wrapper
// structs (which have no custom MarshalJSON and would otherwise leak as
// {"RawMessage":...,"Valid":true}). ----

func TestProfileEditView_SerializesPlainValuesNotSQLWrappers(t *testing.T) {
	edit := db.ProfileEdit{
		ID:        uuid.New(),
		FieldPath: "narrative",
		OldValue:  pqtype.NullRawMessage{RawMessage: json.RawMessage(`"old"`), Valid: true},
		NewValue:  pqtype.NullRawMessage{RawMessage: json.RawMessage(`"new"`), Valid: true},
		Source:    "manual",
		Status:    "accepted",
		CreatedAt: time.Now(),
	}

	b, err := json.Marshal(toProfileEditView(edit))
	require.NoError(t, err)

	var decoded map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(b, &decoded))
	assert.JSONEq(t, `"old"`, string(decoded["old_value"]))
	assert.JSONEq(t, `"new"`, string(decoded["new_value"]))
	assert.NotContains(t, string(b), "RawMessage")
	assert.NotContains(t, string(b), `"Valid"`)
}

func TestProfileEditView_OmitsInvalidOldValue(t *testing.T) {
	edit := db.ProfileEdit{
		ID:        uuid.New(),
		FieldPath: "narrative",
		OldValue:  pqtype.NullRawMessage{Valid: false},
		NewValue:  pqtype.NullRawMessage{RawMessage: json.RawMessage(`"new"`), Valid: true},
		Source:    "manual",
		Status:    "accepted",
		CreatedAt: time.Now(),
	}

	b, err := json.Marshal(toProfileEditView(edit))
	require.NoError(t, err)

	var decoded map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(b, &decoded))
	_, hasOldValue := decoded["old_value"]
	assert.False(t, hasOldValue, "old_value must be omitted, not present as null/wrapper, when there was no prior value")
}

package jobs_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/miguel-anay/career-ops-saas/api/internal/db"
	"github.com/miguel-anay/career-ops-saas/api/internal/jobs"
	"github.com/miguel-anay/career-ops-saas/api/internal/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// ---------------------------------------------------------------------------
// TestUserBCannotReadUserAJob (T-75 — SC-06 cross-tenant isolation)
//
// Verifies that when user B's JWT is injected via middleware.SetUserID and
// they request a job that belongs to user A, the handler returns 404.
//
// The service mock returns ErrNotFound when the requesting user_id does not
// match the job's owner — mirroring the ownership check in Service.GetByID
// and what RLS enforces at DB level (ADR-3).
// ---------------------------------------------------------------------------

func TestUserBCannotReadUserAJob(t *testing.T) {
	svc := &MockService{}
	h := jobs.NewHandler(svc)

	userA := uuid.New()
	userB := uuid.New()
	jobID := uuid.New()

	// User A's job exists in the service — but only visible to user A.
	// When user B requests it the service returns ErrNotFound (ownership check
	// plus what RLS enforces at the DB layer per ADR-3).
	svc.On("GetByID", mock.Anything, userB, jobID).Return(nil, jobs.ErrNotFound)

	req := httptest.NewRequest(http.MethodGet, "/api/jobs/"+jobID.String(), nil)
	ctx := newChiCtx(map[string]string{"id": jobID.String()})
	// Inject user B's identity — simulates a valid JWT for user B.
	ctx = middleware.SetUserID(ctx, userB)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.GetByID(rec, req)

	// Handler must return 404 — user B cannot observe that user A's job exists.
	assert.Equal(t, http.StatusNotFound, rec.Code, "cross-tenant job access must return 404")

	svc.AssertExpectations(t)
	_ = userA // userA UUID documented for clarity — not passed to this handler call
}

// ---------------------------------------------------------------------------
// TestUserBCannotListUserAJobs (SC-06 — list endpoint cross-tenant isolation)
//
// Even if user B guesses that user A has jobs, the List endpoint must return
// only user B's jobs (empty here). The service is scoped to the calling user_id.
// ---------------------------------------------------------------------------

func TestUserBCannotListUserAJobs(t *testing.T) {
	svc := &MockService{}
	h := jobs.NewHandler(svc)

	userA := uuid.New()
	userB := uuid.New()

	// User A has jobs in the system — user B's list call returns nothing.
	userAJob := db.Job{
		ID:     uuid.New(),
		UserID: userA,
		Title:  "Secret Listing",
	}
	_ = userAJob

	// Service called with user B's ID returns empty slice (RLS filters at DB).
	svc.On("List", mock.Anything, userB, 1, 20).Return([]db.Job{}, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/jobs", nil)
	req = req.WithContext(middleware.SetUserID(req.Context(), userB))
	rec := httptest.NewRecorder()

	h.List(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	svc.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// TestCrossTenantJobAccess_DirectUUID
//
// User B sends a request with the exact UUID of user A's job.
// The service (and DB via RLS) must treat this as not found.
// ---------------------------------------------------------------------------

func TestCrossTenantJobAccess_DirectUUID(t *testing.T) {
	svc := &MockService{}
	h := jobs.NewHandler(svc)

	userB := uuid.New()

	// A well-known UUID that belongs to user A in production.
	knownJobID := uuid.MustParse("a2000000-0000-0000-0000-000000000020")

	svc.On("GetByID", mock.Anything, userB, knownJobID).Return(nil, jobs.ErrNotFound)

	req := httptest.NewRequest(http.MethodGet, "/api/jobs/"+knownJobID.String(), nil)
	ctx := newChiCtx(map[string]string{"id": knownJobID.String()})
	ctx = middleware.SetUserID(ctx, userB)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.GetByID(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code, "direct UUID access to another tenant's job must return 404")
	svc.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// TestAuthenticatedUserCanReadOwnJob
//
// Positive test: user A requests their own job and receives it.
// Confirms the handler+service+middleware chain works correctly for the owner.
// ---------------------------------------------------------------------------

func TestAuthenticatedUserCanReadOwnJob(t *testing.T) {
	svc := &MockService{}
	h := jobs.NewHandler(svc)

	userA := uuid.New()
	jobID := uuid.New()
	expectedJob := &db.Job{
		ID:     jobID,
		UserID: userA,
		Title:  "Staff Engineer",
		Status: db.JobStatusTNew,
	}

	svc.On("GetByID", mock.Anything, userA, jobID).Return(expectedJob, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/jobs/"+jobID.String(), nil)
	ctx := newChiCtx(map[string]string{"id": jobID.String()})
	ctx = middleware.SetUserID(ctx, userA)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.GetByID(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	svc.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// TestUnauthenticatedRequestReturns401
//
// No user_id in context → any endpoint must return 401.
// Validates the missing-auth guard across both handler methods.
// ---------------------------------------------------------------------------

func TestUnauthenticatedRequestReturns401_GetByID(t *testing.T) {
	svc := &MockService{}
	h := jobs.NewHandler(svc)

	jobID := uuid.New()
	req := httptest.NewRequest(http.MethodGet, "/api/jobs/"+jobID.String(), nil)
	ctx := newChiCtx(map[string]string{"id": jobID.String()})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.GetByID(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	svc.AssertNotCalled(t, "GetByID")
}

func TestUnauthenticatedRequestReturns401_List(t *testing.T) {
	svc := &MockService{}
	h := jobs.NewHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/jobs", nil)
	// No SetUserID — context has no user.
	rec := httptest.NewRecorder()

	h.List(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	svc.AssertNotCalled(t, "List")
}

// newChiCtx is defined in handler_test.go in the same package — reuse it.
// Redefine only what is needed here to avoid redeclaration across test files.
// NOTE: newChiCtx is already defined in handler_test.go in jobs_test package.
// We reference it directly. Go test compilation merges _test.go files in the
// same package, so this is fine.


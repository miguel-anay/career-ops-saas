# Archive Report — pgboss-queue-unification

**Archived:** 2026-07-04 · **Verdict:** shipped (TBD-light cycle: explore + proposal + apply, no formal spec/design/tasks)

## Cycle

- Unified the Go API and Node worker on the real pg-boss v10 schema: Go enqueues by hand-replicating pg-boss's `insertJob` SQL (`api/internal/queue/boss.go`), pg-boss pinned to exact 10.4.2, queue pre-registration enforced loudly.
- Shipped as PR #22, merged 2026-06-25.
- Battle-tested post-merge: the v10 batch-delivery regression (#42, work() callbacks receive job arrays) was diagnosed and fixed on top of this foundation without touching the enqueue contract.

## Spec promotion

None — infrastructure change, no capability spec to promote. The enqueue contract is documented in-code (`boss.go` header comments).

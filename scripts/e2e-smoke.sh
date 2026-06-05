#!/bin/bash
set -e

BASE=${API_URL:-http://localhost:8080}
WORKER=${WORKER_URL:-http://localhost:3001}
WEB=${WEB_URL:-http://localhost:3000}

echo "=== career-ops-saas smoke test ==="

# 1. API health check
echo "[1/5] API health..."
curl -sf $BASE/health || (echo "FAIL: API not responding" && exit 1)
echo "PASS"

# 2. Worker health check
echo "[2/5] Worker health..."
curl -sf $WORKER/health || (echo "FAIL: Worker not responding" && exit 1)
echo "PASS"

# 3. Web health check
echo "[3/5] Web health..."
curl -sf $WEB/ -o /dev/null || (echo "FAIL: Web not responding" && exit 1)
echo "PASS"

# 4. Auth redirect works
echo "[4/5] Auth redirect..."
STATUS=$(curl -s -o /dev/null -w "%{http_code}" $BASE/auth/google)
if [ "$STATUS" != "302" ]; then
  echo "FAIL: Expected 302, got $STATUS"
  exit 1
fi
echo "PASS"

# 5. Unauthenticated API returns 401
echo "[5/5] 401 on unauth..."
STATUS=$(curl -s -o /dev/null -w "%{http_code}" $BASE/api/jobs)
if [ "$STATUS" != "401" ]; then
  echo "FAIL: Expected 401, got $STATUS"
  exit 1
fi
echo "PASS"

echo ""
echo "=== All smoke tests passed ==="

#!/usr/bin/env bash
# demo-up.sh — start ros-ocp-backend infrastructure for a local full-stack demo.
# See docs/demo/local-full-stack.md §1. Idempotent: safe to re-run.
set -euo pipefail

: "${DEMO_CLUSTER_UUID:=550e8400-e29b-41d4-a716-446655440001}"

cd "$(dirname "$0")/../.."

cp -n .env.example .env 2>/dev/null || true

docker compose -f scripts/docker-compose.yml up -d db-ros kafka zookeeper kafka-create-topics nginx unleash-edge

echo "Waiting for db-ros..."
for _ in $(seq 1 30); do
  if pg_isready -h localhost -p 15432 >/dev/null 2>&1; then break; fi
  sleep 2
done
pg_isready -h localhost -p 15432

go run rosocp.go db migrate up
echo "OK: infra up. Start services per docs/demo/local-full-stack.md §4."

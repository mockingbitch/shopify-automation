#!/usr/bin/env bash
# Starts the local stack for trying the product by hand: API, worker (with
# scheduler), the FAKE Shopee and the dashboard dev server. Logs and PID
# files go to .local-logs/. Stop with scripts/local-down.sh.
set -euo pipefail
cd "$(dirname "$0")/.."
mkdir -p .local-logs
set -a; . ./.env; set +a
docker compose -f deploy/docker-compose.yml up -d postgres redis mailpit s3 >/dev/null
go run ./cmd/migrate up >/dev/null
for c in api worker shopeefake; do go build -o ".local-logs/$c" "./cmd/$c"; done

start() { # name, command...
  local name=$1; shift
  if [[ -f ".local-logs/$name.pid" ]] && kill -0 "$(cat ".local-logs/$name.pid")" 2>/dev/null; then
    echo "$name already running"; return
  fi
  nohup "$@" >".local-logs/$name.log" 2>&1 &
  echo $! >".local-logs/$name.pid"
  echo "$name started (log: .local-logs/$name.log)"
}
start api .local-logs/api
start worker .local-logs/worker -scheduler
start shopeefake .local-logs/shopeefake
(cd web && [[ -d node_modules ]] || npm ci >/dev/null)
start web bash -c 'cd web && exec npm run dev'

echo
echo "Dashboard:  http://localhost:3100   (allow ~15 s for the first start)"
echo "Emails:     http://localhost:8025   (Mailpit)"
echo "FAKE Shopee: http://127.0.0.1:8099  (credentials in .local-logs/shopeefake.log)"

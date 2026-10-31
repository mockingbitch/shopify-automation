#!/usr/bin/env bash
# Stops what scripts/local-up.sh started (only those processes, by PID).
set -uo pipefail
cd "$(dirname "$0")/.."
for name in web shopeefake worker api; do
  f=".local-logs/$name.pid"
  [[ -f $f ]] || continue
  pid=$(cat "$f")
  if kill -0 "$pid" 2>/dev/null; then
    # The dashboard runs under npm: stop its whole process group.
    if [[ $name == web ]]; then pkill -TERM -P "$pid" 2>/dev/null; fi
    kill "$pid" && echo "$name stopped"
  fi
  rm -f "$f"
done

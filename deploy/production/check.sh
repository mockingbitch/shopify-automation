#!/usr/bin/env bash
# Validates the production reference without real secrets:
#   1. docker compose config with production.env.example and placeholder secrets
#   2. nginx -t on the production nginx config with a throwaway self-signed cert
# Nothing is written to the repository; everything lives in a temp dir.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
nginx_image="${NGINX_TEST_IMAGE:-nginx:alpine}"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

echo "==> docker compose config"
cp -r "$here/." "$work/production"
rm -rf "$work/production/secrets"
cp -r "$here/secrets.example" "$work/production/secrets"
mkdir -p "$work/web/public" "$work/tls"
: > "$work/web/csp.conf"
sed -e "s|^WEB_DIST_DIR=.*|WEB_DIST_DIR=$work/web|" \
    -e "s|^TLS_DIR=.*|TLS_DIR=$work/tls|" \
    "$here/production.env.example" > "$work/production.env"
docker compose -f "$work/production/docker-compose.yml" --env-file "$work/production.env" config --quiet
docker compose -f "$work/production/docker-compose.yml" --env-file "$work/production.env" --profile clamav config --quiet
echo "    ok (default and clamav profile)"

echo "==> nginx -t ($nginx_image)"
openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj "/CN=app.example.com" \
  -keyout "$work/tls/privkey.pem" -out "$work/tls/fullchain.pem" >/dev/null 2>&1
chmod 0644 "$work/tls/privkey.pem"
# Use the real CSP of the local web build when there is one (same command as release.yml).
index="$here/../../web/.output/public/index.html"
if [[ -f "$index" ]] && command -v node >/dev/null; then
  csp="$(node "$here/../../web/scripts/csp.mjs" "$index")"
else
  csp="default-src 'self'; frame-ancestors 'none'"
fi
printf 'add_header Content-Security-Policy "%s" always;\n' "$csp" > "$work/web/csp.conf"
docker run --rm --entrypoint nginx \
  --add-host api:127.0.0.1 --ulimit nofile=65536:65536 \
  -v "$here/nginx/nginx.conf:/etc/nginx/nginx.conf:ro" \
  -v "$here/nginx/conf.d:/etc/nginx/conf.d:ro" \
  -v "$here/nginx/snippets:/etc/nginx/snippets:ro" \
  -v "$work/web/csp.conf:/etc/nginx/csp/csp.conf:ro" \
  -v "$work/tls:/etc/nginx/tls:ro" \
  "$nginx_image" -t
echo "    ok"

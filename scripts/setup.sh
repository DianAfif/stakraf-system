#!/usr/bin/env bash
set -euo pipefail
umask 077
STAKRAF_SETUP_DOMAIN=console.stakraf.com
while [ "$#" -gt 0 ]; do
  case "$1" in --domain) STAKRAF_SETUP_DOMAIN="${2:?Missing domain}"; shift 2 ;; *) echo 'Usage: bash scripts/setup.sh [--domain console.example.com]' >&2; exit 1 ;; esac
done
[[ "$STAKRAF_SETUP_DOMAIN" =~ ^[a-zA-Z0-9][a-zA-Z0-9.-]*\.[a-zA-Z]{2,}$ ]] || { echo 'Enter a public hostname without a scheme or path.' >&2; exit 1; }
STAKRAF_SETUP_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$STAKRAF_SETUP_ROOT"
command -v docker >/dev/null && command -v openssl >/dev/null || { echo 'Docker Engine, Compose, and openssl are required.' >&2; exit 1; }
docker compose version >/dev/null
if [ ! -f .env ]; then
  STAKRAF_SETUP_DB_SECRET="$(openssl rand -hex 32)"
  STAKRAF_SETUP_CODE="$(openssl rand -hex 24)"
  printf 'STAKRAF_DOMAIN=%s\nPOSTGRES_DB=stakraf\nPOSTGRES_USER=stakraf\nPOSTGRES_PASSWORD=%s\nSTAKRAF_SIGNUP_MODE=first_user\nSTAKRAF_SETUP_TOKEN=%s\n' "$STAKRAF_SETUP_DOMAIN" "$STAKRAF_SETUP_DB_SECRET" "$STAKRAF_SETUP_CODE" > .env
  unset STAKRAF_SETUP_DB_SECRET STAKRAF_SETUP_CODE
  echo 'Created a private .env with generated database and setup secrets.'
else
  echo 'Using existing .env. Confirm STAKRAF_DOMAIN matches your DNS hostname.'
fi
docker compose -f docker-compose.production.yml config --quiet
docker compose -f docker-compose.production.yml up --build -d
echo "Open https://$STAKRAF_SETUP_DOMAIN after its DNS points to this server. Ports 80/443 must be reachable for HTTPS setup."
echo 'For the first account, use STAKRAF_SETUP_TOKEN from your private .env as the setup code. Registrations close after the first account.'

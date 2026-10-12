#!/usr/bin/env bash
# Run the downloaded installer after reviewing it. Enrollment tokens are read
# interactively, never embedded in commands or written into service configuration.
set -euo pipefail
umask 077
STAKRAF_INSTALL_URL=''
STAKRAF_INSTALL_HTTP=false
STAKRAF_INSTALL_RECONNECT=false
STAKRAF_INSTALL_TOKEN_FILE=''
while [ "$#" -gt 0 ]; do
  case "$1" in
    --url) STAKRAF_INSTALL_URL="${2:?Missing URL}"; shift 2 ;;
    --allow-http) STAKRAF_INSTALL_HTTP=true; shift ;;
    --reconnect) STAKRAF_INSTALL_RECONNECT=true; shift ;;
    --token-file) STAKRAF_INSTALL_TOKEN_FILE="${2:?Missing token file}"; shift 2 ;;
    *) echo "Usage: sudo bash installer.sh --url https://console.stakraf.com [--reconnect] [--allow-http]" >&2; exit 1 ;;
  esac
done
STAKRAF_INSTALL_URL="${STAKRAF_INSTALL_URL%/}"
[[ "$STAKRAF_INSTALL_URL" =~ ^https?://[a-zA-Z0-9.-]+(:[0-9]+)?$ ]] || { echo 'Use a console origin without a path, credentials, or query.' >&2; exit 1; }
if [[ "$STAKRAF_INSTALL_URL" == http://* ]] && [ "$STAKRAF_INSTALL_HTTP" != true ]; then echo 'HTTPS is required. --allow-http is for an isolated lab only.' >&2; exit 1; fi
[ "$(id -u)" -eq 0 ] || { echo 'Run this installer with sudo.' >&2; exit 1; }
[ "$(uname -s)" = Linux ] || { echo 'The connector requires Linux.' >&2; exit 1; }
for program in curl sha256sum systemctl docker useradd usermod runuser install getent stat; do command -v "$program" >/dev/null || { echo "Install $program first." >&2; exit 1; }; done
[ -d /run/systemd/system ] || { echo 'A running systemd host is required.' >&2; exit 1; }
docker info >/dev/null 2>&1 || { echo 'Docker must be installed and running before connecting this server.' >&2; exit 1; }
getent group docker >/dev/null || { echo 'Configure a docker group with access to the local Docker socket first.' >&2; exit 1; }
case "$(uname -m)" in x86_64) STAKRAF_INSTALL_ARCH=amd64 ;; aarch64|arm64) STAKRAF_INSTALL_ARCH=arm64 ;; *) echo 'Supported architectures: x86_64 and arm64.' >&2; exit 1 ;; esac
STAKRAF_INSTALL_TMP="$(mktemp -d /tmp/stakraf-install.XXXXXX)"
trap 'rm -f "$STAKRAF_INSTALL_TMP/agent" "$STAKRAF_INSTALL_TMP/checksum"; rmdir "$STAKRAF_INSTALL_TMP"' EXIT
STAKRAF_INSTALL_ASSET="stakraf-agent-linux-$STAKRAF_INSTALL_ARCH"
STAKRAF_CURL_PROTOCOL='=https'
[ "$STAKRAF_INSTALL_HTTP" != true ] || STAKRAF_CURL_PROTOCOL='=http,https'
curl --fail --silent --show-error --proto "$STAKRAF_CURL_PROTOCOL" --connect-timeout 10 --max-time 120 "$STAKRAF_INSTALL_URL/api/connector/$STAKRAF_INSTALL_ASSET" -o "$STAKRAF_INSTALL_TMP/agent"
curl --fail --silent --show-error --proto "$STAKRAF_CURL_PROTOCOL" --connect-timeout 10 --max-time 30 "$STAKRAF_INSTALL_URL/api/connector/$STAKRAF_INSTALL_ASSET.sha256" -o "$STAKRAF_INSTALL_TMP/checksum"
STAKRAF_EXPECTED_SUM="$(awk '{print $1}' "$STAKRAF_INSTALL_TMP/checksum")"
[[ "$STAKRAF_EXPECTED_SUM" =~ ^[a-f0-9]{64}$ ]] || { echo 'Invalid download checksum.' >&2; exit 1; }
[ "$(sha256sum "$STAKRAF_INSTALL_TMP/agent" | awk '{print $1}')" = "$STAKRAF_EXPECTED_SUM" ] || { echo 'Agent checksum verification failed.' >&2; exit 1; }
id stakraf >/dev/null 2>&1 || useradd --system --user-group --no-create-home --home-dir /var/lib/stakraf --shell /usr/sbin/nologin stakraf
usermod -aG docker stakraf
install -d -o stakraf -g stakraf -m 0700 /var/lib/stakraf
install -d -o root -g stakraf -m 0750 /etc/stakraf
if [ "$STAKRAF_INSTALL_RECONNECT" = true ] && [ -f /var/lib/stakraf/agent.json ]; then
  systemctl stop stakraf-agent.service || true
  mv /var/lib/stakraf/agent.json "/var/lib/stakraf/agent.json.revoked.$(date +%s)"
fi
if [ -f /etc/stakraf/agent.env ] && ! grep -Fxq "STAKRAF_API_URL=$STAKRAF_INSTALL_URL" /etc/stakraf/agent.env && [ "$STAKRAF_INSTALL_RECONNECT" != true ]; then
  echo 'This server is connected to another console. Disconnect it there, then reinstall with --reconnect.' >&2; exit 1
fi
install -o root -g root -m 0755 "$STAKRAF_INSTALL_TMP/agent" /usr/local/bin/stakraf-agent
runuser -u stakraf -- docker info >/dev/null 2>&1 || { echo 'The stakraf service user cannot access Docker. Check docker group/socket permissions.' >&2; exit 1; }
printf 'STAKRAF_API_URL=%s\nSTAKRAF_STATE_FILE=/var/lib/stakraf/agent.json\nSTAKRAF_ALLOW_HTTP=%s\n' "$STAKRAF_INSTALL_URL" "$STAKRAF_INSTALL_HTTP" > /etc/stakraf/agent.env
chown root:stakraf /etc/stakraf/agent.env
chmod 0640 /etc/stakraf/agent.env
if [ ! -f /var/lib/stakraf/agent.json ]; then
  if [ -n "$STAKRAF_INSTALL_TOKEN_FILE" ]; then
    [ "$(stat -c %a "$STAKRAF_INSTALL_TOKEN_FILE")" = 600 ] && [ "$(stat -c %u "$STAKRAF_INSTALL_TOKEN_FILE")" = 0 ] || { echo 'Token file must be root-owned with mode 0600.' >&2; exit 1; }
    STAKRAF_INSTALL_TOKEN="$(< "$STAKRAF_INSTALL_TOKEN_FILE")"
  else
    printf 'Paste the one-time enrollment token from your console (input hidden): ' > /dev/tty
    IFS= read -r -s STAKRAF_INSTALL_TOKEN < /dev/tty
    printf '\n' > /dev/tty
  fi
  [[ "$STAKRAF_INSTALL_TOKEN" =~ ^she_[a-f0-9]{48}$ ]] || { echo 'Invalid enrollment token format.' >&2; exit 1; }
  runuser -u stakraf -- env STAKRAF_API_URL="$STAKRAF_INSTALL_URL" STAKRAF_STATE_FILE=/var/lib/stakraf/agent.json STAKRAF_ALLOW_HTTP="$STAKRAF_INSTALL_HTTP" STAKRAF_ENROLLMENT_TOKEN="$STAKRAF_INSTALL_TOKEN" /usr/local/bin/stakraf-agent --enroll-only
  unset STAKRAF_INSTALL_TOKEN
fi
runuser -u stakraf -- env STAKRAF_API_URL="$STAKRAF_INSTALL_URL" STAKRAF_STATE_FILE=/var/lib/stakraf/agent.json STAKRAF_ALLOW_HTTP="$STAKRAF_INSTALL_HTTP" /usr/local/bin/stakraf-agent --check >/dev/null
cat > /etc/systemd/system/stakraf-agent.service <<'UNIT'
[Unit]
Description=Stakraf server connector
After=network-online.target docker.service
Wants=network-online.target
Requires=docker.service
StartLimitIntervalSec=0

[Service]
Type=simple
User=stakraf
Group=stakraf
SupplementaryGroups=docker
EnvironmentFile=/etc/stakraf/agent.env
ExecStart=/usr/local/bin/stakraf-agent
Restart=always
RestartSec=10
WorkingDirectory=/var/lib/stakraf
UMask=0077
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/stakraf
PrivateTmp=true

[Install]
WantedBy=multi-user.target
UNIT
systemctl daemon-reload
systemctl enable stakraf-agent.service
systemctl restart stakraf-agent.service
sleep 2
systemctl is-active --quiet stakraf-agent.service || { echo 'Connector did not start. Run: sudo journalctl -u stakraf-agent -n 50 --no-pager' >&2; exit 1; }
echo 'Connector installed. Your server should appear online in the console shortly.'
echo 'Service: sudo systemctl status stakraf-agent'
echo 'Logs: sudo journalctl -u stakraf-agent -f'

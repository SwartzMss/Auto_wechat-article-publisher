#!/usr/bin/env bash
set -euo pipefail

# Deployment script for Auto WeChat Article Publisher (Go + Vite).
# - Expects a pre-built backend binary (use scripts/build.sh first).
# - Syncs frontend dist to STATIC_ROOT.
# - Installs/updates nginx site + systemd service.

ROOT="$(cd -- "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

DEFAULT_ENV_FILE="$ROOT/config/deploy.env"
if [ -f "${DEPLOY_ENV_FILE:-$DEFAULT_ENV_FILE}" ]; then
  # shellcheck source=/dev/null
  . "${DEPLOY_ENV_FILE:-$DEFAULT_ENV_FILE}"
fi

log() { printf '[deploy] %s\n' "$*"; }
fail() { printf '[deploy] ERROR: %s\n' "$*" >&2; exit 1; }

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || fail "Command '$1' not found."
}

ensure_root() {
  if [ "$EUID" -ne 0 ]; then
    fail "Must run as root (use sudo)."
  fi
}

# Configurable knobs (env or deploy.env)
APP_USER="${APP_USER:-${SUDO_USER:-$(whoami)}}"
SERVICE_NAME="${SERVICE_NAME:-auto-wechat.service}"
SERVICE_UNIT_PATH="${SERVICE_UNIT_PATH:-/etc/systemd/system/$SERVICE_NAME}"
BINARY="${BINARY:-$ROOT/bin/auto-wechat-article-publisher}"
CONFIG_FILE="${CONFIG_FILE:-$ROOT/config/config.json}"
BIND_ADDR="${BIND_ADDR:-}"
STATIC_SRC="${STATIC_SRC:-$ROOT/server/web/dist}"
STATIC_ROOT="${STATIC_ROOT:-/var/www/auto-wechat}"
STATIC_OWNER="${STATIC_OWNER:-$APP_USER}"
STATIC_GROUP="${STATIC_GROUP:-$APP_USER}"
DOMAIN="${DOMAIN:-}"
DOMAIN_ALIASES="${DOMAIN_ALIASES:-}"
HTTPS_PORT="${HTTPS_PORT:-443}"
HTTP_PORT="${HTTP_PORT:-}"
SSL_CERT_PATH="${SSL_CERT_PATH:-}"
SSL_KEY_PATH="${SSL_KEY_PATH:-}"
NGINX_SITE_NAME="${NGINX_SITE_NAME:-auto-wechat}"
NGINX_CONF_PATH="${NGINX_CONF_PATH:-/etc/nginx/sites-available/${NGINX_SITE_NAME}.conf}"
NGINX_ENABLED_PATH="${NGINX_ENABLED_PATH:-/etc/nginx/sites-enabled/${NGINX_SITE_NAME}.conf}"
LOG_FILE="${LOG_FILE:-$ROOT/logs/app.log}"
LOGROTATE_ENABLE="${LOGROTATE_ENABLE:-1}"

ensure_root
require_cmd systemctl
require_cmd nginx
command -v rsync >/dev/null 2>&1 || log "rsync not found; will fallback to cp"

# Validation
id "$APP_USER" >/dev/null 2>&1 || fail "User '$APP_USER' does not exist."
[ -x "$BINARY" ] || fail "Backend binary not found/executable at $BINARY. Run scripts/build.sh first."
[ -f "$CONFIG_FILE" ] || fail "Config file missing at $CONFIG_FILE."
[ -d "$STATIC_SRC" ] || fail "Frontend dist not found at $STATIC_SRC. Run scripts/build.sh (frontend) first."
[ -n "$DOMAIN" ] || fail "DOMAIN is required for TLS."
[ -f "$SSL_CERT_PATH" ] || fail "SSL cert not found at $SSL_CERT_PATH."
[ -f "$SSL_KEY_PATH" ] || fail "SSL key not found at $SSL_KEY_PATH."

SERVER_NAMES="$DOMAIN"
if [ -n "$DOMAIN_ALIASES" ]; then
  SERVER_NAMES="$SERVER_NAMES $DOMAIN_ALIASES"
fi

BACKEND_HOST="127.0.0.1"
BACKEND_PORT="8080"
if [ -z "$BIND_ADDR" ] && [ -f "$CONFIG_FILE" ]; then
  # Try to read server_addr from config JSON; ignore failure and keep default.
  addr=$(
    python - "$CONFIG_FILE" <<'PY' 2>/dev/null
import json, re, sys, pathlib
path = pathlib.Path(sys.argv[1])
try:
    text = re.sub(r'//.*', '', path.read_text())
    data = json.loads(text)
    val = data.get("server_addr") or ""
    print(val)
except Exception:
    pass
PY
  )
  if [ -n "$addr" ]; then
    BIND_ADDR="$addr"
  fi
fi

if [ -n "$BIND_ADDR" ]; then
  addr="$BIND_ADDR"
  if [[ "$addr" == *:* ]]; then
    host_part="${addr%:*}"
    port_part="${addr##*:}"
    [ -n "$host_part" ] && BACKEND_HOST="$host_part"
    [ -n "$port_part" ] && BACKEND_PORT="$port_part"
  else
    BACKEND_PORT="$addr"
  fi
fi
PROXY_PASS="http://${BACKEND_HOST}:${BACKEND_PORT}/"

ensure_paths() {
  mkdir -p "$STATIC_ROOT"
  chown -R "$STATIC_OWNER":"$STATIC_GROUP" "$STATIC_ROOT" || true
  mkdir -p "$(dirname "$LOG_FILE")"
  touch "$LOG_FILE" || true
  chown "$STATIC_OWNER":"$STATIC_GROUP" "$LOG_FILE" || true
}

sync_frontend() {
  log "Syncing frontend dist -> $STATIC_ROOT"
  if command -v rsync >/dev/null 2>&1; then
    rsync -a --delete "$STATIC_SRC"/ "$STATIC_ROOT"/
  else
    rm -rf "${STATIC_ROOT:?}/"*
    cp -a "$STATIC_SRC"/. "$STATIC_ROOT"/
  fi
  chown -R "$STATIC_OWNER":"$STATIC_GROUP" "$STATIC_ROOT" || true
}

write_nginx() {
  log "Writing nginx config -> $NGINX_CONF_PATH"
  cat >"$NGINX_CONF_PATH" <<EOF
server {
    listen ${HTTPS_PORT} ssl http2;
    listen [::]:${HTTPS_PORT} ssl http2;
    server_name ${SERVER_NAMES};

    ssl_certificate     ${SSL_CERT_PATH};
    ssl_certificate_key ${SSL_KEY_PATH};
    ssl_protocols       TLSv1.2 TLSv1.3;
    ssl_prefer_server_ciphers on;

    root ${STATIC_ROOT};
    index index.html;
    try_files \$uri \$uri/ /index.html;

    location /api/ {
        proxy_pass ${PROXY_PASS};
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
        proxy_http_version 1.1;
    }

    location ~* \.(css|js|jpg|jpeg|png|gif|ico|svg)$ {
        expires 7d;
        access_log off;
    }
}
EOF

  if [ -n "$HTTP_PORT" ]; then
    cat >"$NGINX_CONF_PATH.http" <<EOF
server {
    listen ${HTTP_PORT};
    listen [::]:${HTTP_PORT};
    server_name ${SERVER_NAMES};
    return 301 https://\$host:${HTTPS_PORT}\$request_uri;
}
EOF
    # prepend HTTP block to main file
    cat "$NGINX_CONF_PATH.http" "$NGINX_CONF_PATH" >"$NGINX_CONF_PATH.tmp" && mv "$NGINX_CONF_PATH.tmp" "$NGINX_CONF_PATH"
    rm -f "$NGINX_CONF_PATH.http"
  fi

  ln -sf "$NGINX_CONF_PATH" "$NGINX_ENABLED_PATH"
  nginx -t
}

write_systemd() {
  log "Writing systemd unit -> $SERVICE_UNIT_PATH"
  cat >"$SERVICE_UNIT_PATH" <<EOF
[Unit]
Description=Auto WeChat Article Publisher
After=network.target

[Service]
User=${APP_USER}
Group=${APP_USER}
WorkingDirectory=${ROOT}
ExecStart=${BINARY} --serve --config ${CONFIG_FILE} ${BIND_ADDR:+--addr ${BIND_ADDR}}
Restart=always
RestartSec=5
StandardOutput=append:${LOG_FILE}
StandardError=append:${LOG_FILE}

[Install]
WantedBy=multi-user.target
EOF
  systemctl daemon-reload
  systemctl enable --now "$SERVICE_NAME"
}

write_logrotate() {
  [ "${LOGROTATE_ENABLE}" = "1" ] || return 0
  if ! command -v logrotate >/dev/null 2>&1; then
    log "logrotate not found; skipping log rotation config"
    return 0
  fi
  local target="/etc/logrotate.d/${SERVICE_NAME%.*}"
  log "Writing logrotate config -> $target"
  cat >"$target" <<EOF
${LOG_FILE} {
    weekly
    rotate 8
    compress
    missingok
    notifempty
    copytruncate
}
EOF
}

reload_nginx() {
  log "Reloading nginx"
  systemctl reload nginx
}

deploy() {
  ensure_paths
  sync_frontend
  write_nginx
  write_systemd
  write_logrotate
  reload_nginx
  log "Deployment complete. Check: systemctl status $SERVICE_NAME"
}

case "${1:-deploy}" in
  deploy) deploy ;;
  start) systemctl start "$SERVICE_NAME" ;;
  stop) systemctl stop "$SERVICE_NAME" ;;
  restart) systemctl restart "$SERVICE_NAME" ;;
  status) systemctl status "$SERVICE_NAME" ;;
  reload-nginx) reload_nginx ;;
  *)
    cat >&2 <<EOF
Usage: sudo ./scripts/deploy.sh [deploy|start|stop|restart|status|reload-nginx]
Env vars (or config/deploy.env) to override defaults:
  APP_USER, BINARY, CONFIG_FILE, BIND_ADDR, STATIC_SRC, STATIC_ROOT,
  DOMAIN, DOMAIN_ALIASES, HTTPS_PORT, HTTP_PORT,
  SSL_CERT_PATH, SSL_KEY_PATH, SERVICE_NAME, NGINX_SITE_NAME
EOF
    exit 1
    ;;
esac

#!/usr/bin/env bash
# One-time / repeatable provisioning of the Dietsis service on Ubuntu. Run as root FROM THE UNPACKED
# RELEASE DIRECTORY on the VPS:   sudo ./deploy/install.sh
# It does NOT touch nginx or certificates (see docs/DEPLOYMENT.md), and never overwrites the env file.
#
# Optional environment:
#   DIETSIS_WWW_ROOT   where static releases live (default /var/www/dietsis). The folder may already exist
#                      (e.g. /var/www/html/dietsis); only releases/ and the current symlink are managed inside it.
set -euo pipefail

[ "$(id -u)" -eq 0 ] || { echo "run as root (sudo)"; exit 1; }
HERE="$(cd "$(dirname "$0")/.." && pwd)"
[ -x "$HERE/bin/dietsis-server" ] || { echo "bin/dietsis-server not found next to deploy/"; exit 1; }
[ -f "$HERE/www/index.html" ] || { echo "www/index.html not found"; exit 1; }

VERSION="$(cat "$HERE/VERSION" 2>/dev/null || date +%Y%m%d%H%M%S)"
WWW="${DIETSIS_WWW_ROOT:-/var/www/dietsis}"

id dietsis >/dev/null 2>&1 || useradd --system --no-create-home --shell /usr/sbin/nologin dietsis
install -d -m 0700 -o dietsis -g dietsis /var/lib/dietsis /var/backups/dietsis
install -d -m 0750 -o root -g dietsis /etc/dietsis
install -d -m 0755 /opt/dietsis/bin "$WWW/releases" /var/www/certbot

if [ ! -f /etc/dietsis/dietsis.env ]; then
  install -m 0640 -o root -g dietsis "$HERE/deploy/dietsis.env.example" /etc/dietsis/dietsis.env
  echo ">> Created /etc/dietsis/dietsis.env: fill DIETSIS_GOOGLE_CLIENT_ID before starting the service."
fi

# Back up first, so a bad migration or release can be rolled back.
if [ -f /var/lib/dietsis/dietsis.db ] && [ -x /opt/dietsis/bin/dietsis-server ]; then
  echo ">> Backing up the current database"
  sudo -u dietsis env $(grep -v '^#' /etc/dietsis/dietsis.env | xargs) /opt/dietsis/bin/dietsis-server backup
fi

# Static files: new release directory, then an atomic symlink switch (rollback = point back).
rm -rf "$WWW/releases/$VERSION"
cp -r "$HERE/www" "$WWW/releases/$VERSION"
chmod -R a+rX "$WWW/releases/$VERSION"
ln -sfn "$WWW/releases/$VERSION" "$WWW/current.new"
mv -T "$WWW/current.new" "$WWW/current"
# Keep the 5 newest releases.
ls -1dt "$WWW"/releases/* | tail -n +6 | xargs -r rm -rf

# Binary: atomic replace.
install -m 0755 "$HERE/bin/dietsis-server" /opt/dietsis/bin/dietsis-server.new
mv -f /opt/dietsis/bin/dietsis-server.new /opt/dietsis/bin/dietsis-server

install -m 0644 "$HERE"/deploy/dietsis.service /etc/systemd/system/dietsis.service
install -m 0644 "$HERE"/deploy/dietsis-backup.service /etc/systemd/system/dietsis-backup.service
install -m 0644 "$HERE"/deploy/dietsis-backup.timer /etc/systemd/system/dietsis-backup.timer
if [ -d /etc/nginx/snippets ]; then
  install -m 0644 "$HERE"/deploy/nginx-headers.conf /etc/nginx/snippets/dietsis-headers.conf
  # Empty unless the site sits behind Cloudflare (then run deploy/cloudflare-realip.sh once).
  [ -f /etc/nginx/snippets/dietsis-cloudflare.conf ] || echo "# real client IP settings: see deploy/cloudflare-realip.sh" > /etc/nginx/snippets/dietsis-cloudflare.conf
else
  echo ">> nginx not installed yet: copy deploy/nginx-headers.conf to /etc/nginx/snippets/dietsis-headers.conf later"
fi

systemctl daemon-reload
systemctl enable --now dietsis-backup.timer
systemctl enable dietsis.service

# Production refuses to start without a Google client id; do not loop on a known failure.
if ! grep -Eq '^DIETSIS_GOOGLE_CLIENT_ID=.+' /etc/dietsis/dietsis.env; then
  echo ">> DIETSIS_GOOGLE_CLIENT_ID is empty in /etc/dietsis/dietsis.env. Fill it, then run: sudo systemctl restart dietsis"
  exit 0
fi
systemctl restart dietsis.service
sleep 2
ADDR="$(sed -n 's/^DIETSIS_ADDR=//p' /etc/dietsis/dietsis.env | tail -1)"
if curl -fsS "http://${ADDR:-127.0.0.1:8787}/api/v1/health" >/dev/null; then
  echo ">> dietsis $VERSION is healthy"
else
  echo "!! health check failed: journalctl -u dietsis -n 50"; exit 1
fi

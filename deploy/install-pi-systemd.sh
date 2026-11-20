#!/usr/bin/env bash
# Install systemd units so API+UI compose stacks come up after reboot.
# Run on the Pi with sudo: sudo bash /home/dima/ligaBackend/deploy/install-pi-systemd.sh
set -euo pipefail

API_UNIT_SRC="${1:-/home/dima/ligaBackend/deploy/mrpushkin-api.service}"
UI_UNIT_SRC="${2:-/home/dima/mr-pushkin-league/deploy/mrpushkin-ui.service}"

install -m 644 "$API_UNIT_SRC" /etc/systemd/system/mrpushkin-api.service
install -m 644 "$UI_UNIT_SRC" /etc/systemd/system/mrpushkin-ui.service
systemctl daemon-reload
systemctl enable mrpushkin-api.service mrpushkin-ui.service
systemctl start mrpushkin-api.service
systemctl start mrpushkin-ui.service
systemctl --no-pager --full status mrpushkin-api.service mrpushkin-ui.service || true
echo "enabled: mrpushkin-api + mrpushkin-ui"

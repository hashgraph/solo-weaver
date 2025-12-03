#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
set -x
mkdir -p /var/run/dbus

DBUS_SESSION_BUS_ADDRESS="$(dbus-daemon --fork --config-file=/usr/share/dbus-1/system.conf --print-address)"
export DBUS_SESSION_BUS_ADDRESS

exec /usr/lib/systemd/systemd --system
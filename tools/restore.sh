#!/usr/bin/env bash
# Restores a podium backup into a scratch directory and checks it with
# "podium-server check -hash", without touching the live data. The script
# itself ships in the release as deploy/backup/podium-restore (installed at
# /usr/local/lib/podium/podium-restore); this wrapper runs it from a
# checkout. Run it without arguments for the forms it takes.
exec bash "$(dirname "${BASH_SOURCE[0]}")/../deploy/backup/podium-restore" "$@"

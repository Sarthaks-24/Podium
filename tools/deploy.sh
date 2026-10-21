#!/usr/bin/env bash
# Builds a release and installs it on the VM with install.sh upgrade, from
# Git Bash on Windows (or any bash with make, ssh and scp).
#
#   tools/deploy.sh HOST [--force "<reason>"]
#   VERSION=vX.Y.Z tools/deploy.sh HOST [--force "<reason>"]
#
# HOST is what you type after ssh, for example owner@contest.example.edu
# (your own account with sudo, never the podium service user) or an alias
# from ~/.ssh/config. The release is named by VERSION when it is set (a
# drill build, C2-002), otherwise by the vX.Y.Z tag on this commit; with
# neither, or with uncommitted changes, it stops before anything else. In
# order:
#   1. asks the running server's deploy guard (podium-server deploy-check)
#      over ssh, before anything is built or copied (D112);
#   2. make release VERSION=<that version>;
#   3. shows the tarball, its SHA-256 and the destination, and asks you to
#      type the host name before anything leaves this machine;
#   4. scp to /tmp on the VM, unpack, and sudo ./install.sh upgrade there,
#      which runs the guard again right before installing.
# --force passes the reason to install.sh upgrade --force: only for a fix
# that cannot wait for the guard (docs/RUNBOOK.md, "Emergency deploy").
set -euo pipefail

die() { printf 'deploy.sh: %s\n' "$*" >&2; exit 1; }
[[ $# -ge 1 ]] || die "usage: tools/deploy.sh HOST [--force \"<reason>\"]"
host=$1; shift
force=""
if [[ $# -gt 0 ]]; then
  [[ $1 == --force && $# -eq 2 ]] || die "usage: tools/deploy.sh HOST [--force \"<reason>\"]"
  force=$2
  [[ ${#force} -ge 10 ]] || die "give a reason of at least 10 characters with --force; it goes in the contest's audit log"
fi
[[ $host != -* ]] || die "HOST must not start with -"
cd "$(dirname "${BASH_SOURCE[0]}")/.."

# A release must be named vX.Y.Z (D260), so decide the name before the
# guard and the build: VERSION when given (a drill build), otherwise the
# tag on this commit. make release would refuse any other name, but only
# after minutes of building.
version=${VERSION:-$(git describe --tags --exact-match 2>/dev/null || true)}
[[ $version =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] \
  || die "this commit is not a vX.Y.Z tag${version:+ ($version)}: tag it, or run VERSION=vX.Y.Z tools/deploy.sh HOST"
git diff --quiet HEAD -- 2>/dev/null \
  || die "the working tree has uncommitted changes; commit or stash them, so the release is what the commit says"
if [[ -n ${VERSION:-} ]]; then
  echo "Building $version from VERSION, not from a tag: for drill builds only (D105)."
fi

echo "== 1/4 deploy guard on $host"
code=0
ssh -t "$host" "sudo -u podium /usr/local/bin/podium-server deploy-check -db /var/lib/podium/db/podium.db" || code=$?
case $code in
  0) ;;
  255) die "ssh to $host failed; check the host name and your key" ;;
  1) [[ -n $force ]] || die "the guard refused (above); nothing was built or copied" ;;
  *) echo "The guard could not answer (exit $code): an older server without deploy-check, or no database yet. install.sh upgrade runs the new release's guard before installing." ;;
esac

echo "== 2/4 make release VERSION=$version"
make release VERSION="$version"
tarball=release/podium-$version.tar.gz
[[ -f $tarball ]] || die "make release did not write $tarball"

echo "== 3/4 confirm"
echo "  release:     $tarball ($(du -h "$tarball" | cut -f1))"
echo "  sha256:      $(sha256sum "$tarball" | cut -d' ' -f1)"
echo "  copy to:     $host:/tmp/"
echo "  then run:    sudo ./install.sh upgrade${force:+ --force <reason>}"
read -r -p "Type the host ($host) to copy the release there: " answer
[[ $answer == "$host" ]] || die "stopped; nothing left this machine"

echo "== 4/4 copy and upgrade"
# A private folder made by mktemp on the VM: a fixed name under /tmp could
# be made first by another local user.
dir=$(ssh "$host" 'mktemp -d /tmp/podium-deploy.XXXXXX') || die "could not make a folder on $host"
[[ $dir =~ ^/tmp/podium-deploy\.[A-Za-z0-9]+$ ]] || die "unexpected folder name from $host: $dir"
scp "$tarball" "$host:$dir/"
remote="set -e; cd $dir; tar xzf podium-$version.tar.gz; cd podium-$version; sudo ./install.sh upgrade"
if [[ -n $force ]]; then
  remote+=" --force $(printf '%q' "$force")"
fi
ssh -t "$host" "$remote"

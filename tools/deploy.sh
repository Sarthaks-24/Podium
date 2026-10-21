#!/usr/bin/env bash
# Builds a release and installs it on the VM with install.sh upgrade, from
# Git Bash on Windows (or any bash with make, ssh and scp).
#
#   tools/deploy.sh HOST [--force "<reason>"]
#
# HOST is what you type after ssh, for example owner@contest.example.edu
# (your own account with sudo, never the podium service user) or an alias from ~/.ssh/config. In order:
#   1. asks the running server's deploy guard (podium-server deploy-check)
#      over ssh, before anything is built or copied (D112);
#   2. make release;
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

# Every deploy is a tag (D105): say so when this commit is not one.
version=$(git describe --tags --always --dirty 2>/dev/null || echo dev)
if ! git describe --tags --exact-match >/dev/null 2>&1 || [[ $version == *-dirty ]]; then
  echo "This commit is not exactly a tag (version would be $version). Every deploy should be a tag (D105)."
  read -r -p "Deploy it anyway? Type yes: " answer
  [[ $answer == yes ]] || die "stopped; tag the commit (the owner pushes tags) and run again"
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

echo "== 2/4 make release"
make release
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

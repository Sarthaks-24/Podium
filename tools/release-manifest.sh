#!/usr/bin/env bash
# Writes MANIFEST and SHA256SUMS into a release folder (B1.2), called by
# make release before the folder is packed:
#
#   tools/release-manifest.sh DIR VERSION COMMIT BUILT UPGRADES_FROM
#
# MANIFEST is what the release is, one key=value per line, read by
# install.sh upgrade (the version check and the "oldest it upgrades from"
# refusal) and kept on the VM as /usr/local/lib/podium/MANIFEST. The
# schema and protocol come from the server's own -version output, so they
# cannot disagree with the binaries. SHA256SUMS lists every other file in
# the folder; install.sh checks it before installing anything.
set -euo pipefail

dir=$1 version=$2 commit=$3 built=$4 upgrades_from=$5
# install.sh compares versions to refuse an older or unversioned install
# and a downgrade; a release named by a commit hash could never be
# upgraded from, so only vX.Y.Z is accepted for both values.
semver='^v[0-9]+\.[0-9]+\.[0-9]+$'
[[ $version =~ $semver ]] || { echo "release-manifest: version '$version' is not vX.Y.Z; tag the commit or run make release VERSION=vX.Y.Z" >&2; exit 1; }
[[ $upgrades_from =~ $semver ]] || { echo "release-manifest: UPGRADES_FROM '$upgrades_from' is not vX.Y.Z" >&2; exit 1; }
# v1.0.0 is the Makefile's placeholder until the owner decides D236; a
# real release (v1.5.0 or later) built with it would accept an upgrade
# from releases that were never tagged (C2-040).
if [[ $upgrades_from == v1.0.0 ]] && [[ $(printf '%s\n%s\n' 1.5.0 "${version#v}" | sort -V | head -n1) == 1.5.0 ]]; then
  echo "release-manifest: $version still upgrades from the placeholder v1.0.0; set UPGRADES_FROM in the Makefile (D236)" >&2
  exit 1
fi
repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
[[ -d $dir/bin ]] || { echo "release-manifest: $dir has no bin/" >&2; exit 1; }

# The host build answers -version; only schema and protocol are read
# from it (version, commit and date are the arguments).
info=$(cd "$repo/server" && go run ./cmd/podium-server -version)
schema=$(sed -n 's/^schema //p' <<<"$info")
protocol=$(sed -n 's/^protocol //p' <<<"$info")
[[ -n $schema && -n $protocol ]] || { echo "release-manifest: podium-server -version gave no schema or protocol" >&2; exit 1; }
migrations=$(cd "$repo/server/internal/db/migrations" && ls -1 [0-9]*.sql | sed 's/\.sql$//' | paste -sd, -)
[[ ${migrations##*,} == "$schema" ]] || { echo "release-manifest: the newest migration file is not $schema" >&2; exit 1; }

cat > "$dir/MANIFEST" <<EOF
release=$(basename "$dir")
version=$version
commit=$commit
built=$built
schema=$schema
migrations=$migrations
protocol=$protocol
upgrades_from=$upgrades_from
EOF

(cd "$dir" && find . -type f ! -name SHA256SUMS -print | sed 's|^\./||' | LC_ALL=C sort | xargs sha256sum > SHA256SUMS)
echo "MANIFEST: $version, schema $schema, protocol $protocol, upgrades from $upgrades_from; SHA256SUMS: $(wc -l < "$dir/SHA256SUMS") files"

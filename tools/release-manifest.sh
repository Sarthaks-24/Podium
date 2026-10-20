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

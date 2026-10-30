// Package buildinfo says which build a binary is: the release version,
// commit and build date set by make release (-ldflags -X), the schema
// version (the highest migration this build carries) and the judge
// protocol version. Both binaries print it with -version, and
// make release writes the same facts into the release MANIFEST.
package buildinfo

import (
	"fmt"
	"io"

	"github.com/Sarthaks-24/podium/internal/db/migrations"
	"github.com/Sarthaks-24/podium/internal/judgeapi"
)

// Set at build time: -X github.com/Sarthaks-24/podium/internal/buildinfo.Version=v1.1.0
// and likewise Commit and Date. A plain go build leaves the defaults.
var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)

// Schema is the highest migration name this build applies.
func Schema() string { return migrations.Latest() }

// Protocol is the judge API version this build speaks.
func Protocol() int { return judgeapi.ProtocolVersion }

// Write prints the five lines of -version, one "key value" pair each, in
// a form install.sh and make release read with sed.
func Write(w io.Writer) {
	fmt.Fprintf(w, "version %s\ncommit %s\nbuilt %s\nschema %s\nprotocol %d\n", Version, Commit, Date, Schema(), Protocol())
}

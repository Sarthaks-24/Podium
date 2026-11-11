package db

import (
	"errors"

	"modernc.org/sqlite"
)

// SQLite's extended result codes for the constraint failures the services
// tell apart (https://www.sqlite.org/rescode.html).
const (
	codeConstraintForeignKey = 787
	codeConstraintPrimaryKey = 1555
	codeConstraintUnique     = 2067
)

// IsUnique reports whether err is a UNIQUE or PRIMARY KEY violation. It reads
// the driver's error code, not the message text, so it does not depend on
// SQLite's wording or on a table or index name. A statement that can break
// two unique indexes cannot tell them apart by this alone.
func IsUnique(err error) bool {
	c := resultCode(err)
	return c == codeConstraintUnique || c == codeConstraintPrimaryKey
}

// IsForeignKey reports whether err is a FOREIGN KEY violation: the row
// points at one that does not exist.
func IsForeignKey(err error) bool {
	return resultCode(err) == codeConstraintForeignKey
}

func resultCode(err error) int {
	var se *sqlite.Error
	if errors.As(err, &se) {
		return se.Code()
	}
	return 0
}

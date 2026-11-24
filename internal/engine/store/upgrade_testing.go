package store

import (
	"fmt"
	"strings"
	"sync"
)

// beforeMu keeps two NewBefore calls from changing the migrations at once.
var beforeMu sync.Mutex

// NewBefore opens (creating it when missing) the database at dbPath with
// the schema an older Legato left: migrated up to, and not including, the
// migration whose name ends with migration ("_review_loop_conversion.sql").
// It is for tests of an upgrade, which seed what the older Legato wrote,
// close it, and open it again with New. It changes the migrations this
// binary runs while it opens, so nothing else may open a database then.
func NewBefore(dbPath, migration string) (*Store, error) {
	beforeMu.Lock()
	defer beforeMu.Unlock()
	at := -1
	for i, name := range migrations {
		if strings.HasSuffix(name, migration) {
			at = i
			break
		}
	}
	if at < 0 {
		return nil, fmt.Errorf("no migration %q", migration)
	}
	all := migrations
	migrations = append([]string(nil), all[:at]...)
	defer func() { migrations = all }()
	return New(dbPath)
}

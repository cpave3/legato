// Package version is the version of this Legato binary: the release it was
// built as, or "dev" for a build from source.
package version

import (
	"regexp"
	"strconv"
)

// stamped is set by a release build:
//
//	go build -ldflags "-X github.com/cpave3/legato/internal/engine/version.stamped=v1.2.3"
var stamped string

var stable = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

// String is the version this binary was released as, else "dev".
func String() string {
	if stamped == "" {
		return "dev"
	}
	return stamped
}

// IsRelease reports whether this binary is a stable release (vX.Y.Z). Only
// a release updates itself; a build from source is left to its developer.
func IsRelease() bool {
	return IsStable(stamped)
}

// IsStable reports whether v is a stable version, vX.Y.Z.
func IsStable(v string) bool {
	return stable.MatchString(v)
}

// Newer reports whether candidate is a stable version later than current,
// itself a stable version. Anything else is never newer.
func Newer(candidate, current string) bool {
	a, okA := parse(candidate)
	b, okB := parse(current)
	if !okA || !okB {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

func parse(v string) ([3]int, bool) {
	m := stable.FindStringSubmatch(v)
	if m == nil {
		return [3]int{}, false
	}
	var out [3]int
	for i := range out {
		n, err := strconv.Atoi(m[i+1])
		if err != nil {
			return [3]int{}, false
		}
		out[i] = n
	}
	return out, true
}

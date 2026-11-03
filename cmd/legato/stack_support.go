package main

import (
	"context"
	"sync"
	"time"

	"github.com/cpave3/legato/internal/cli"
)

// stackSupportFor is how long a host keeps StackSupport's answer: st is
// asked again a minute later, so one installed or updated is found without
// asking it at every session.
const stackSupportFor = time.Minute

var stackSupportCache struct {
	sync.Mutex
	at        time.Time
	supported bool
}

// hostStackSupport is cli.StackSupport, kept for a minute.
func hostStackSupport() bool {
	stackSupportCache.Lock()
	defer stackSupportCache.Unlock()
	if time.Since(stackSupportCache.at) < stackSupportFor {
		return stackSupportCache.supported
	}
	stackSupportCache.supported = cli.StackSupport(context.Background())
	stackSupportCache.at = time.Now()
	return stackSupportCache.supported
}

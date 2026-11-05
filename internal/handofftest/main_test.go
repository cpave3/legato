package handofftest

import (
	"os"
	"testing"

	"github.com/cpave3/legato/internal/testenv"
)

// Two Legatos run in these tests; neither may reach the person's own
// configuration, data or running Legato.
func TestMain(m *testing.M) {
	os.Exit(testenv.Run(m.Run))
}

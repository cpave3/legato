package main

import (
	"os"
	"testing"

	"github.com/cpave3/legato/internal/testenv"
)

func TestMain(m *testing.M) {
	os.Exit(testenv.Run(m.Run))
}

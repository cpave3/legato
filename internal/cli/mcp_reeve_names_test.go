package cli_test

import (
	"github.com/cpave3/legato/internal/cli"
)

// everyReeveTool is a Reeve with everything its tools need, so that each
// is offered.
func everyReeveTool() *cli.MCPReeve {
	return &cli.MCPReeve{}
}

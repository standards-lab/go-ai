//go:build integration

package integration_test

import (
	"testing"

	"github.com/standards-lab/go-ai/tools/infer/integration"
)

// The suite's TestMain builds the program once for the run. The unit tier
// never enters here: without the tag the harness type-checks and needs no
// binary.
func TestMain(m *testing.M) { integration.Main(m) }

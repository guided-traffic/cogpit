package integration_test

import (
	"testing"

	"github.com/guided-traffic/cogpit/internal/testkit"
)

// TestMain runs the tests, or the credential plugin a test started (testkit.Plugin).
func TestMain(m *testing.M) { testkit.Main(m) }

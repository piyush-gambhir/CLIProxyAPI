package responses

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	SetClientToolSearchEnabled(true)
	os.Exit(m.Run())
}

func setClientToolSearchEnabledForTest(t *testing.T, enabled bool) {
	t.Helper()
	previous := ClientToolSearchEnabled()
	SetClientToolSearchEnabled(enabled)
	t.Cleanup(func() { SetClientToolSearchEnabled(previous) })
}

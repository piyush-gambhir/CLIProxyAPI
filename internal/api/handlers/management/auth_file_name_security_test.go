package management

import "testing"

func TestAuthFileNamesRejectDirectoryTraversal(t *testing.T) {
	for _, name := range []string{"", " ", ".", "..", "../outside.json", `..\outside.json`, "dir/account.json", `/tmp/account.json`} {
		if !isUnsafeAuthFileName(name) {
			t.Errorf("unsafe auth filename accepted: %q", name)
		}
	}
	if isUnsafeAuthFileName("account.json") {
		t.Fatal("ordinary auth filename rejected")
	}
}

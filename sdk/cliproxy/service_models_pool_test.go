package cliproxy

import "testing"

// TestModelPrefixesDecideWhetherAPoolExists pins the precondition for
// reset-soonest routing: a shared pool needs bare model IDs registered for every
// credential, which means no per-credential prefix, or force-model-prefix off.
//
// With force-model-prefix on and a per-credential prefix, only
// "<prefix>/<model>" is registered, so an unprefixed request has no candidates
// at all and every selection strategy is inert.
func TestModelPrefixesDecideWhetherAPoolExists(t *testing.T) {
	t.Parallel()

	models := func() []*ModelInfo {
		return []*ModelInfo{{ID: "claude-opus-5"}, {ID: "claude-fable-5-1"}}
	}

	cases := []struct {
		name             string
		prefix           string
		forceModelPrefix bool
		want             []string
	}{
		{
			name:             "no prefix leaves bare ids, which is the pool",
			prefix:           "",
			forceModelPrefix: false,
			want:             []string{"claude-opus-5", "claude-fable-5-1"},
		},
		{
			name:             "a prefix without force registers both, so the pool survives",
			prefix:           "account-1",
			forceModelPrefix: false,
			want: []string{
				"claude-opus-5", "account-1/claude-opus-5",
				"claude-fable-5-1", "account-1/claude-fable-5-1",
			},
		},
		{
			name:             "a prefix with force pins the credential and removes it from the pool",
			prefix:           "account-1",
			forceModelPrefix: true,
			want:             []string{"account-1/claude-opus-5", "account-1/claude-fable-5-1"},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			got := applyModelPrefixes(models(), testCase.prefix, testCase.forceModelPrefix)
			ids := make([]string, 0, len(got))
			for _, model := range got {
				ids = append(ids, model.ID)
			}
			if len(ids) != len(testCase.want) {
				t.Fatalf("ids = %v, want %v", ids, testCase.want)
			}
			for i := range ids {
				if ids[i] != testCase.want[i] {
					t.Fatalf("ids = %v, want %v", ids, testCase.want)
				}
			}
		})
	}
}

// TestPrefixedRouteRemainsAvailableAsAManualOverride checks that turning
// force-model-prefix off does not remove the pinned route: both the bare id and
// the prefixed id are registered, so the prefix stays usable as an override.
func TestPrefixedRouteRemainsAvailableAsAManualOverride(t *testing.T) {
	t.Parallel()

	got := applyModelPrefixes([]*ModelInfo{{ID: "claude-opus-5"}}, "account-7", false)
	ids := make(map[string]bool, len(got))
	for _, model := range got {
		ids[model.ID] = true
	}
	if !ids["claude-opus-5"] {
		t.Fatalf("ids = %v, want the bare model id for the pool", ids)
	}
	if !ids["account-7/claude-opus-5"] {
		t.Fatalf("ids = %v, want the prefixed id as a manual override", ids)
	}
}

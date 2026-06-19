package artifact

import (
	"sort"
	"testing"
)

// TestRegistry_AllPersistentRegistered asserts that every entry in
// the AllPersistent manifest has a corresponding factory in
// Registered(). A failure here means a new persistable artifact type
// was added to the package, or removed from it, without updating
// AllPersistent or the per-type init() blocks. See issue #453.
func TestRegistry_AllPersistentRegistered(t *testing.T) {
	registered := Registered()

	want := map[string]struct{}{}
	for _, a := range AllPersistent() {
		want[a.Kind()] = struct{}{}
	}

	have := map[string]struct{}{}
	for k := range registered {
		have[k] = struct{}{}
	}

	// Anything in the manifest that didn't register.
	var missing []string
	for k := range want {
		if _, ok := have[k]; !ok {
			missing = append(missing, k)
		}
	}
	// Anything registered that isn't in the manifest (i.e. an init()
	// block that wasn't paired with a manifest entry, or vice versa).
	var extra []string
	for k := range have {
		if _, ok := want[k]; !ok {
			extra = append(extra, k)
		}
	}

	if len(missing) > 0 || len(extra) > 0 {
		sort.Strings(missing)
		sort.Strings(extra)
		t.Errorf("artifact registry drift: missing from Registered()=%v, registered but not in AllPersistent()=%v", missing, extra)
	}
}

// TestRegistry_FactoriesYieldCorrectKind asserts that the factory
// stored in Registered() for a given kind actually produces a value
// whose Kind() matches the key. A mismatch here means a per-type
// init() registered a factory with the wrong kind identifier.
func TestRegistry_FactoriesYieldCorrectKind(t *testing.T) {
	registered := Registered()
	for kind, factory := range registered {
		a := factory()
		if got := a.Kind(); got != kind {
			t.Errorf("registry: kind %q produced artifact with Kind()=%q", kind, got)
		}
	}
}

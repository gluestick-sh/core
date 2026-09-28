package envset

import "testing"

// These tests only exercise the empty-input short circuit, which returns before
// the platform layer touches the user environment. Non-empty cases would write
// to the real HKCU\Environment key and must not run in unit tests.

func TestApplyUser_emptyIsNoop(t *testing.T) {
	if err := ApplyUser(nil); err != nil {
		t.Fatalf("ApplyUser(nil) = %v, want nil", err)
	}
	if err := ApplyUser(map[string]string{}); err != nil {
		t.Fatalf("ApplyUser(empty) = %v, want nil", err)
	}
}

func TestRemoveUser_emptyIsNoop(t *testing.T) {
	if err := RemoveUser(nil); err != nil {
		t.Fatalf("RemoveUser(nil) = %v, want nil", err)
	}
	if err := RemoveUser([]string{}); err != nil {
		t.Fatalf("RemoveUser(empty) = %v, want nil", err)
	}
}
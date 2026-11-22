package install

import "testing"

func TestIsHookVar(t *testing.T) {
	for kv, want := range map[string]bool{
		"VW2026_HOOK_NAME=cli":  true,
		"VW2025_HOOK_TAG=":      true,
		"vw2025_hook_tag=x":     true,
		"VW1999_HOOK_X":         true,
		"VW2026_HOOK=x":         false,
		"VW2026_SESSION=x":      false,
		"VW202_HOOK_NAME=cli":   false,
		"VWABCD_HOOK_NAME=cli":  false,
		"XVW2026_HOOK_NAME=cli": false,
		"VW":                    false,
	} {
		if got := isHookVar(kv); got != want {
			t.Errorf("isHookVar(%q) = %v, want %v", kv, got, want)
		}
	}
}

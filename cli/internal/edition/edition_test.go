package edition

import (
	"regexp"
	"testing"
)

// 版から決まる名前が、どの版でも同じ形になる（2026 で vw2026・VW2026_ なら、2025 で vw2025・VW2025_）。
func TestNames(t *testing.T) {
	if !regexp.MustCompile(`^20[0-9]{2}$`).MatchString(Year) {
		t.Fatalf("Year = %q", Year)
	}
	for got, want := range map[string]string{
		Command:       "vw" + Year,
		EnvPrefix:     "VW" + Year + "_",
		HookEnvPrefix: "VW" + Year + "_HOOK_",
		Product:       "Vectorworks " + Year,
		Manifest:      "vw" + Year + ".json",
		Bootstrap:     "get-vw" + Year,
	} {
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}

func TestExpand(t *testing.T) {
	for in, want := range map[string]string{
		"":                                 "",
		"no variables":                     "no variables",
		"${Cmd} runs ${cmd}":               "Vw" + Year + " runs vw" + Year,
		"${prefix}CALL_TIMEOUT":            "VW" + Year + "_CALL_TIMEOUT",
		"${product}/${year}":               "Vectorworks " + Year + "/" + Year,
		"${bootstrap}.sh and ${manifest}":  "get-vw" + Year + ".sh and vw" + Year + ".json",
		"${default} and ${env} stay as is": "${default} and ${env} stay as is",
		"$HOME and ${unknown}":             "$HOME and ${unknown}",
	} {
		if got := Expand(in); got != want {
			t.Errorf("Expand(%q) = %q, want %q", in, got, want)
		}
	}
}

// Vars のどの名前も、kong が自分で埋める ${default}・${env} と重ならない。
func TestVarsDoNotShadowKong(t *testing.T) {
	for _, name := range []string{"default", "env"} {
		if _, ok := Vars()[name]; ok {
			t.Errorf("Vars has %s, which kong fills by itself", name)
		}
	}
}

package registry

import (
	"strings"
	"testing"
)

// 既定のレジストリは CLI に埋め込むので、読めなければ install cli が動かない。cli の規則は
// latest を決まった場所で、それ以外をタグで指す（docs/registry.md「レジストリ」）。
func TestDefaultRegistry(t *testing.T) {
	def := Default()
	cli, ok := def.Plugins["cli"]
	if !ok || len(def.Plugins) != 1 {
		t.Fatalf("plugins: %v", def.Plugins)
	}
	base := "https://github.com/min-nano/vectorworks-plugin-command-line-interface/releases/"
	for requested, want := range map[string]string{
		"latest":                     base + "latest/download/vw2026.json",
		"0.0.0-a1b2c3d":              base + "download/0.0.0-a1b2c3d/vw2026.json",
		"dev-feature-x-61-a1b2c3d":   base + "download/dev-feature-x-61-a1b2c3d/vw2026.json",
		"latest-but-not-the-keyword": base + "download/latest-but-not-the-keyword/vw2026.json",
	} {
		rule, ok := cli.Select(requested)
		if !ok || rule.Location(requested) != want {
			t.Errorf("%s: %v %q", requested, ok, rule.Location(requested))
		}
		if rule.ByTag() != (requested != "latest") {
			t.Errorf("%s: ByTag %v", requested, rule.ByTag())
		}
	}
}

func TestValidName(t *testing.T) {
	for _, name := range []string{"cli", "a", "import-ifc-homeskz", "a1", strings.Repeat("a", 64), "com10"} {
		if !ValidName(name) {
			t.Errorf("%q should be valid", name)
		}
	}
	for _, name := range []string{"", "-a", "a-", "A", "a_b", "a.b", "a/b", "..", strings.Repeat("a", 65), "con", "nul", "com1", "lpt9", "aux", "prn"} {
		if ValidName(name) {
			t.Errorf("%q should be invalid", name)
		}
	}
}

// タグの先頭を英数字に限るので、. や .. だけのタグが {tag} の置き換えで上の階層を指さない。
func TestValidTag(t *testing.T) {
	for _, tag := range []string{"latest", "0.0.0-a1b2c3d", "v1.2_3", "A", strings.Repeat("a", 128)} {
		if !ValidTag(tag) {
			t.Errorf("%q should be valid", tag)
		}
	}
	for _, tag := range []string{"", ".", "..", "-a", "_a", ".a", "a/b", `a\b`, "a b", "a@b", "a{tag}", strings.Repeat("a", 129)} {
		if ValidTag(tag) {
			t.Errorf("%q should be invalid", tag)
		}
	}
}

func TestMatch(t *testing.T) {
	for _, c := range []struct {
		pattern, s string
		want       bool
	}{
		{"latest", "latest", true},
		{"latest", "latest2", false},
		{"latest", "Latest", false},
		{"*", "", true},
		{"*", "anything", true},
		{"dev-*", "dev-x", true},
		{"dev-*", "dev-", true},
		{"dev-*", "xdev-", false},
		{"*-rc", "1.0-rc", true},
		{"*-rc", "1.0-rc1", false},
		{"a*b*c", "abc", true},
		{"a*b*c", "a-b-c", true},
		{"a*b*c", "acb", false},
		{"a*a", "a", false},
		{"**", "x", true},
	} {
		if got := Match(c.pattern, c.s); got != c.want {
			t.Errorf("Match(%q, %q) = %v", c.pattern, c.s, got)
		}
	}
}

// 最初に合った規則だけを使う（後ろに合う規則があっても見ない）。
func TestSelectFirstMatch(t *testing.T) {
	entry := Entry{Manifests: []Rule{
		{Match: "stable", Path: "https://example.com/stable.json"},
		{Match: "*", Path: "https://example.com/{tag}/vw2026.json"},
		{Match: "stable", Path: "https://example.com/never.json"},
	}}
	if rule, _ := entry.Select("stable"); rule.Path != "https://example.com/stable.json" {
		t.Fatal(rule)
	}
	narrow := Entry{Manifests: []Rule{{Match: "v*", Path: "https://example.com/{tag}.json"}}}
	if _, ok := narrow.Select("latest"); ok {
		t.Fatal("no rule should match")
	}
}

func TestParse(t *testing.T) {
	good := `{"registry_version":1,"extra":true,"plugins":{"a":{"manifests":[{"match":"*","path":"https://x/{tag}.json","note":1}]}}}`
	if _, err := Parse([]byte(good), true); err != nil {
		t.Fatalf("unknown fields are ignored: %v", err)
	}
	local := `{"registry_version":1,"plugins":{"a":{"manifests":[{"match":"*","path":"` + absPath("{tag}.json") + `"}]}}}`
	if _, err := Parse([]byte(local), false); err != nil {
		t.Fatalf("a local registry can refer to a path: %v", err)
	}
	for name, data := range map[string]string{
		"not JSON":            `{`,
		"no version":          `{"plugins":{}}`,
		"unknown version":     `{"registry_version":2,"plugins":{}}`,
		"bad name":            `{"registry_version":1,"plugins":{"A":{"manifests":[{"match":"*","path":"https://x"}]}}}`,
		"no rules":            `{"registry_version":1,"plugins":{"a":{"manifests":[]}}}`,
		"bad match":           `{"registry_version":1,"plugins":{"a":{"manifests":[{"match":"a/b","path":"https://x"}]}}}`,
		"http":                `{"registry_version":1,"plugins":{"a":{"manifests":[{"match":"*","path":"http://x"}]}}}`,
		"relative":            `{"registry_version":1,"plugins":{"a":{"manifests":[{"match":"*","path":"x/{tag}.json"}]}}}`,
		"remote to a path":    local,
		"one bad of two":      `{"registry_version":1,"plugins":{"a":{"manifests":[{"match":"*","path":"https://x"}]},"b":{"manifests":[{"match":"*","path":"ftp://x"}]}}}`,
		"empty URL":           `{"registry_version":1,"plugins":{"a":{"manifests":[{"match":"*","path":"https://"}]}}}`,
		"a string as version": `{"registry_version":"1","plugins":{}}`,
	} {
		if _, err := Parse([]byte(data), true); err == nil {
			t.Errorf("%s: should fail", name)
		}
	}
}

func manifestJSON(asset, sha, hooks string) string {
	return `{"manifest_version":1,"tag":"t1","targets":{"macos-arm64":{"asset":"` + asset + `","sha256":"` + sha + `"` + hooks + `}}}`
}

func TestParseManifest(t *testing.T) {
	sha := strings.Repeat("0a", 32)
	m, err := ParseManifest([]byte(manifestJSON("https://x/a.zip", sha, `,"hooks":{"install":["sh","hooks/install.sh"]}`)), true)
	if err != nil {
		t.Fatal(err)
	}
	asset := m.Targets["macos-arm64"]
	if m.Tag != "t1" || asset.URL != "https://x/a.zip" || asset.SHA256 != sha ||
		strings.Join(asset.Hooks.Install, " ") != "sh hooks/install.sh" || asset.Hooks.Uninstall != nil {
		t.Fatalf("%+v", m)
	}
	if _, err := ParseManifest([]byte(manifestJSON(absPath("a.zip"), sha, "")), false); err != nil {
		t.Fatalf("a local manifest can refer to a path: %v", err)
	}
	for name, data := range map[string]string{
		"unknown version": `{"manifest_version":2,"tag":"t1","targets":{}}`,
		"no version":      `{"tag":"t1","targets":{}}`,
		"bad tag":         `{"manifest_version":1,"tag":"..","targets":{}}`,
		"remote to path":  manifestJSON(absPath("a.zip"), sha, ""),
		"relative asset":  manifestJSON("a.zip", sha, ""),
		"upper sha":       manifestJSON("https://x/a.zip", strings.ToUpper(sha), ""),
		"short sha":       manifestJSON("https://x/a.zip", sha[:63], ""),
		"no sha":          manifestJSON("https://x/a.zip", "", ""),
		"sh without file": manifestJSON("https://x/a.zip", sha, `,"hooks":{"install":["sh"]}`),
		"sh -c":           manifestJSON("https://x/a.zip", sha, `,"hooks":{"install":["sh","-c","rm -rf ~"]}`),
		"dotdot":          manifestJSON("https://x/a.zip", sha, `,"hooks":{"uninstall":["powershell","../x.ps1"]}`),
		"absolute":        manifestJSON("https://x/a.zip", sha, `,"hooks":{"install":["/bin/rm"]}`),
		"backslash":       manifestJSON("https://x/a.zip", sha, `,"hooks":{"install":["sh","hooks\\x.sh"]}`),
		"empty hook":      manifestJSON("https://x/a.zip", sha, `,"hooks":{"install":[]}`),
	} {
		if _, err := ParseManifest([]byte(data), true); err == nil {
			t.Errorf("%s: should fail", name)
		}
	}
}

func TestScript(t *testing.T) {
	for argv, want := range map[string]string{
		"sh hooks/install.sh":            "hooks/install.sh",
		"powershell hooks/install.ps1 x": "hooks/install.ps1",
		"bin/hook --flag":                "bin/hook",
	} {
		got, err := Script(strings.Fields(argv))
		if err != nil || got != want {
			t.Errorf("%s: %q %v", argv, got, err)
		}
	}
	for _, argv := range []string{"sh", "-x", "sh -c", "sh ../x", "./../x", "/x", "sh /x"} {
		if _, err := Script(strings.Fields(argv)); err == nil {
			t.Errorf("%s: should fail", argv)
		}
	}
}

func TestCheckRelative(t *testing.T) {
	for _, path := range []string{"a", "a/b", "a/.b", "a..b"} {
		if err := CheckRelative(path); err != nil {
			t.Errorf("%q: %v", path, err)
		}
	}
	for _, path := range []string{"", "-a", `a\b`, "/a", "../a", "a/../../b", "a/.."} {
		if err := CheckRelative(path); err == nil {
			t.Errorf("%q should be refused", path)
		}
	}
}

// 間の部分が見つからなければ合わない。手元のレジストリでも相対パスは断る。読めないマニフェストは断る。
func TestRegistryEdges(t *testing.T) {
	if Match("a*b*c", "a-x-c") {
		t.Error("a*b*c should not match a-x-c")
	}
	relative := `{"registry_version":1,"plugins":{"a":{"manifests":[{"match":"*","path":"x/{tag}.json"}]}}}`
	if _, err := Parse([]byte(relative), false); err == nil {
		t.Error("a relative path should be refused in a local registry too")
	}
	if _, err := ParseManifest([]byte("{"), false); err == nil {
		t.Error("a broken manifest should be refused")
	}
}

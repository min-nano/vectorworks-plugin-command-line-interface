// Package registry は、vw2026 install が何を・どこから取るかを決める 2 つのファイル（レジストリと
// マニフェスト）の読み方を持つ（docs/registry.md）。取ってくること・置くことは install が持つ。
package registry

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// Version は読めるレジストリの registry_version。
const Version = 1

// ManifestVersion は読めるマニフェストの manifest_version。
const ManifestVersion = 1

// DefaultName は、記録の registry に書く既定のレジストリの名前（docs/registry.md「入れたときの記録」）。
const DefaultName = "default"

// TagPlaceholder は規則の path の中で、@ の後ろの文字列に置き換わる部分。
const TagPlaceholder = "{tag}"

// defaultJSON は既定のレジストリ。CLI に埋め込み、必ず先頭に引く（docs/registry.md「レジストリの順」）。
//
//go:embed default.json
var defaultJSON []byte

// Default は既定のレジストリを返す。埋め込んだものは TestDefaultRegistry が読めることを確かめるので、
// 誤りは返さない（読めないのはビルドの誤りで、実行時には起きない）。
func Default() *Registry {
	registry, _ := Parse(defaultJSON, false)
	return registry
}

var (
	namePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)
	tagPattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	// match はタグの文字に * を足したもの。
	matchPattern = regexp.MustCompile(`^[A-Za-z0-9._*-]+$`)
	sha256Hex    = regexp.MustCompile(`^[0-9a-f]{64}$`)
	// Windows の予約された名前（大文字と小文字を区別しない OS なので、名前が小文字でも当たる）。
	reservedName = regexp.MustCompile(`^(con|nul|aux|prn|com[0-9]|lpt[0-9])$`)
)

// ValidName は名前が使える文字だけでできているか（docs/registry.md「名前・タグ・ターゲット」）。
// 名前はプラグインのフォルダ名にもなる。
func ValidName(name string) bool {
	return len(name) <= 64 && namePattern.MatchString(name) && !reservedName.MatchString(name)
}

// ValidTag はタグが使える文字だけでできているか。先頭を英数字に限るので、. や .. だけのタグが
// {tag} の置き換えで上の階層を指さない。
func ValidTag(tag string) bool {
	return len(tag) <= 128 && tagPattern.MatchString(tag)
}

// Targets は CLI が知るターゲット。
var Targets = []string{"macos-arm64", "macos-x64", "windows-x64"}

// Registry はレジストリ 1 つ。
type Registry struct {
	Plugins map[string]Entry
}

// Entry はレジストリの名前 1 つの項目。
type Entry struct {
	Manifests []Rule
}

// Rule はマニフェストの場所の規則 1 つ。
type Rule struct {
	Match string
	Path  string
}

// Parse はレジストリを読む。remote は https:// で取ったものか（そうなら path は https:// だけ。
// 遠くのレジストリが利用者の手元のパスを指さないため）。1 つでも正しくない項目があれば、
// レジストリ全体を読めないものとして扱う。知らない項目は無視し、知らない版は失敗にする。
func Parse(data []byte, remote bool) (*Registry, error) {
	var raw struct {
		RegistryVersion *int `json:"registry_version"`
		Plugins         map[string]struct {
			Manifests []struct {
				Match string `json:"match"`
				Path  string `json:"path"`
			} `json:"manifests"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("not a registry: %w", err)
	}
	if raw.RegistryVersion == nil {
		return nil, errors.New("not a registry: registry_version is missing")
	}
	if *raw.RegistryVersion != Version {
		return nil, fmt.Errorf("unknown registry_version %d (this vw2026 reads %d)", *raw.RegistryVersion, Version)
	}
	registry := &Registry{Plugins: map[string]Entry{}}
	for name, plugin := range raw.Plugins {
		if !ValidName(name) {
			return nil, fmt.Errorf("invalid name %q", name)
		}
		if len(plugin.Manifests) == 0 {
			return nil, fmt.Errorf("%s: no manifests", name)
		}
		var entry Entry
		for i, rule := range plugin.Manifests {
			if !matchPattern.MatchString(rule.Match) {
				return nil, fmt.Errorf("%s: manifests[%d]: invalid match %q", name, i, rule.Match)
			}
			if err := checkRef(strings.ReplaceAll(rule.Path, TagPlaceholder, "tag"), remote); err != nil {
				return nil, fmt.Errorf("%s: manifests[%d]: path: %w", name, i, err)
			}
			entry.Manifests = append(entry.Manifests, Rule{Match: rule.Match, Path: rule.Path})
		}
		registry.Plugins[name] = entry
	}
	return registry, nil
}

// Select は @ の後ろの文字列に最初に合った規則を返す。合わなければ false。取れなくても次の
// 規則へは進まない（どの場所を使うかが文字列だけで決まり、ネットワークの状態で変わらないため）。
func (e Entry) Select(requested string) (Rule, bool) {
	for _, rule := range e.Manifests {
		if Match(rule.Match, requested) {
			return rule, true
		}
	}
	return Rule{}, false
}

// Location はマニフェストの場所（{tag} を置き換えたもの）。
func (r Rule) Location(requested string) string {
	return strings.ReplaceAll(r.Path, TagPlaceholder, requested)
}

// ByTag は規則がタグで直接指すか（path に {tag} がある）。そうならマニフェストの tag が
// 指定と一致することを求める。
func (r Rule) ByTag() bool {
	return strings.Contains(r.Path, TagPlaceholder)
}

// Match はパターン（* は 0 文字以上に合う）が s 全体に合うか。
func Match(pattern, s string) bool {
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return pattern == s
	}
	if !strings.HasPrefix(s, parts[0]) {
		return false
	}
	s = s[len(parts[0]):]
	last := parts[len(parts)-1]
	for _, part := range parts[1 : len(parts)-1] {
		i := strings.Index(s, part)
		if i < 0 {
			return false
		}
		s = s[i+len(part):]
	}
	return len(s) >= len(last) && strings.HasSuffix(s, last)
}

// IsURL は参照が https:// の URL か（遠くのもの）。
func IsURL(ref string) bool {
	return strings.HasPrefix(ref, "https://")
}

// IsFileURL は参照が file:// の URL か（手元のもの）。
func IsFileURL(ref string) bool {
	return strings.HasPrefix(ref, "file://")
}

// FilePath は file:// の URL を、この OS のパスにする（docs/registry.md「参照の書き方」）。
func FilePath(ref string) (string, error) {
	return filePath(ref, runtime.GOOS == "windows")
}

// filePath は file:// の URL を、windows なら Windows の、そうでなければ mac・Linux のパスにする。
// %XX は解く（空白は %20 と書く）。ホストは、Windows の共有フォルダ（\\host\share）を指すときだけ書ける
// （localhost は書かないのと同じ）。問い合わせ・断片の付いたもの・相対のものは断る。
func filePath(ref string, windows bool) (string, error) {
	switch {
	case !IsFileURL(ref):
		return "", fmt.Errorf("%q is not a file:// URL", ref)
	case strings.ContainsAny(ref, " \t\r\n"):
		// URL は空白を含まない（引数と環境変数の値は空白で区切る）。
		return "", fmt.Errorf("%q has a space (write it as %%20)", ref)
	}
	u, err := url.Parse(ref)
	if err != nil {
		return "", fmt.Errorf("%q is not a URL (write a space as %%20): %w", ref, err)
	}
	host := u.Host
	if host == "localhost" {
		host = ""
	}
	switch {
	case u.RawQuery != "" || u.ForceQuery || u.Fragment != "":
		return "", fmt.Errorf("%q has a query or a fragment", ref)
	case !strings.HasPrefix(u.Path, "/"):
		return "", fmt.Errorf("%q has no absolute path (write file:///…)", ref)
	case host != "" && !windows:
		return "", fmt.Errorf("%q names the host %s (only a shared folder on Windows has one)", ref, host)
	}
	if !windows {
		return u.Path, nil
	}
	path := strings.ReplaceAll(u.Path, "/", `\`)
	if host != "" {
		return `\\` + host + path, nil
	}
	// file:///C:/… の C: の前の / を除く。ドライブの無いものは絶対ではない。
	if len(path) < 4 || path[2] != ':' || path[3] != '\\' || !isLetter(path[1]) {
		return "", fmt.Errorf("%q has no drive (write file:///C:/…)", ref)
	}
	return path[1:], nil
}

func isLetter(c byte) bool {
	return 'A' <= c && c <= 'Z' || 'a' <= c && c <= 'z'
}

// checkRef は参照が https:// か file:// の URL かを確かめる。remote なら https:// だけ（遠くのものが
// 利用者の手元のファイルを指さないため）。
func checkRef(ref string, remote bool) error {
	switch {
	case IsURL(ref):
		if len(ref) == len("https://") {
			return errors.New("empty URL")
		}
		return nil
	case remote:
		return fmt.Errorf("%q is not an https:// URL (a remote file can refer only to https://)", ref)
	case IsFileURL(ref):
		_, err := FilePath(ref)
		return err
	}
	return fmt.Errorf("%q is neither an https:// nor a file:// URL", ref)
}

// Manifest は 1 つのリリースのマニフェスト（vw2026.json）。
type Manifest struct {
	Tag     string
	Targets map[string]Asset
}

// Asset はターゲット 1 つの資産。
type Asset struct {
	URL    string
	SHA256 string
	Hooks  Hooks
}

// Hooks は install と uninstall のフック。どちらも省略できる（nil）。
type Hooks struct {
	Install   []string `json:"install,omitempty"`
	Uninstall []string `json:"uninstall,omitempty"`
}

// ParseManifest はマニフェストを読む。remote は https:// で取ったものか（そうなら asset は https:// だけ）。
func ParseManifest(data []byte, remote bool) (*Manifest, error) {
	var raw struct {
		ManifestVersion *int   `json:"manifest_version"`
		Tag             string `json:"tag"`
		Targets         map[string]struct {
			Asset  string `json:"asset"`
			SHA256 string `json:"sha256"`
			Hooks  *struct {
				Install   []string `json:"install"`
				Uninstall []string `json:"uninstall"`
			} `json:"hooks"`
		} `json:"targets"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("not a manifest: %w", err)
	}
	if raw.ManifestVersion == nil {
		return nil, errors.New("not a manifest: manifest_version is missing")
	}
	if *raw.ManifestVersion != ManifestVersion {
		return nil, fmt.Errorf("unknown manifest_version %d (this vw2026 reads %d; to update vw2026, run the first-time script)",
			*raw.ManifestVersion, ManifestVersion)
	}
	if !ValidTag(raw.Tag) {
		return nil, fmt.Errorf("invalid tag %q", raw.Tag)
	}
	manifest := &Manifest{Tag: raw.Tag, Targets: map[string]Asset{}}
	for target, asset := range raw.Targets {
		if err := checkRef(asset.Asset, remote); err != nil {
			return nil, fmt.Errorf("%s: asset: %w", target, err)
		}
		if !sha256Hex.MatchString(asset.SHA256) {
			return nil, fmt.Errorf("%s: sha256 must be 64 lowercase hexadecimal digits", target)
		}
		var hooks Hooks
		if asset.Hooks != nil {
			hooks = Hooks{Install: asset.Hooks.Install, Uninstall: asset.Hooks.Uninstall}
		}
		if err := hooks.Check(); err != nil {
			return nil, fmt.Errorf("%s: %w", target, err)
		}
		manifest.Targets[target] = Asset{URL: asset.Asset, SHA256: asset.SHA256, Hooks: hooks}
	}
	return manifest, nil
}

// Check はフックの書き方を確かめる（docs/registry.md「書き方」）。zip の中に実在するかは、
// 展開したあとで install が確かめる。
func (h Hooks) Check() error {
	for _, hook := range []struct {
		name string
		argv []string
	}{{"install", h.Install}, {"uninstall", h.Uninstall}} {
		if hook.argv == nil {
			continue
		}
		if _, err := Script(hook.argv); err != nil {
			return fmt.Errorf("hooks.%s: %w", hook.name, err)
		}
	}
	return nil
}

// Interpreters はフックの 1 つ目に書ける、zip の外のもの。
var Interpreters = []string{"sh", "powershell"}

// Script はフックの argv から、zip の中のファイルの相対パス（/ 区切り）を返す。1 つ目が sh・powershell
// なら 2 つ目、それ以外なら 1 つ目。絶対パス・..・- で始まるもの（sh -c のような指定）・\ は断る。
func Script(argv []string) (string, error) {
	if len(argv) == 0 {
		return "", errors.New("empty")
	}
	script := argv[0]
	for _, interpreter := range Interpreters {
		if argv[0] == interpreter {
			if len(argv) < 2 {
				return "", fmt.Errorf("%s needs the path of a script in the zip", interpreter)
			}
			script = argv[1]
		}
	}
	if err := CheckRelative(script); err != nil {
		return "", err
	}
	return script, nil
}

// CheckRelative は、フックが渡すスクリプトの相対パスを確かめる。CheckInside に加えて、- で始まるもの
// （インタープリターが指定と読む）も断る。
func CheckRelative(path string) error {
	if strings.HasPrefix(path, "-") {
		return fmt.Errorf("%q starts with -", path)
	}
	return CheckInside(path)
}

// CheckInside は、zip の中の相対パス（区切りは /）が展開先の外へ出ないことを確かめる。zip の項目の
// 名前（install の展開）とフックのスクリプトのパスの両方が使う（規則を 1 か所に保つ）。
func CheckInside(path string) error {
	switch {
	case path == "":
		return errors.New("empty path")
	case strings.Contains(path, `\`):
		return fmt.Errorf("%q uses \\ (the separator is /)", path)
	case strings.HasPrefix(path, "/") || !filepath.IsLocal(filepath.FromSlash(path)):
		return fmt.Errorf("%q is not a relative path inside the folder", path)
	}
	for _, part := range strings.Split(path, "/") {
		if part == ".." {
			return fmt.Errorf("%q contains ..", path)
		}
	}
	return nil
}

// Package registry は、vw2026 install が何を・どこから取得するかを決定する 2 つの
// ファイル（レジストリとマニフェスト）の読み取り方を有する（docs/registry.md）。取得と配置は
// install が担う。
package registry

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/edition"
)

// Version は読み取れるレジストリの registry_version。
const Version = 1

// manifestVersion は読み取れるマニフェストの manifest_version。
const manifestVersion = 1

// DefaultName は、記録の registry に記述する既定のレジストリの
// 名前（docs/registry.md「インストールの記録」）。
const DefaultName = "default"

// tagPlaceholder は規則の path の中で、@ の後ろの文字列に置換される部分。
const tagPlaceholder = "{tag}"

// Default は既定のレジストリを返す。埋め込んだものは TestDefaultRegistry が読み取れることを
// 検証するため、誤りは返さない（読み取れないのはビルドの誤りであり、実行時には発生しない）。
func Default() *Registry {
	registry, _ := Parse(defaultJSON, false)
	return registry
}

var (
	namePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)
	tagPattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	// match はタグの文字に * を追加したもの。
	matchPattern = regexp.MustCompile(`^[A-Za-z0-9._*-]+$`)
	sha256Hex    = regexp.MustCompile(`^[0-9a-f]{64}$`)
	// Windows の予約された名前（大文字と小文字を区別しない OS であるため、名前が小文字でも
	// 該当する）。
	reservedName = regexp.MustCompile(`^(con|nul|aux|prn|com[0-9]|lpt[0-9])$`)
)

// ValidName は名前が使用できる文字のみで構成されているか
// （docs/registry.md「名前・タグ・ターゲット」）。名前はプラグインのフォルダ名にもなる。
func ValidName(name string) bool {
	return len(name) <= 64 && namePattern.MatchString(name) && !reservedName.MatchString(name)
}

// ValidTag はタグが使用できる文字のみで構成されているか。先頭を英数字に限るため、. や .. のみの
// タグが {tag} の置換で上の階層を指さない。
func ValidTag(tag string) bool {
	return len(tag) <= 128 && tagPattern.MatchString(tag)
}

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
	Match string `json:"match"`
	Path  string `json:"path"`
}

// Parse はレジストリを読み取る。remote は https:// で取得したものか否か（そうであれば path は
// https:// のみ。遠隔のレジストリが利用者のローカル環境のパスを指さないため）。1 つでも正しくない
// 項目があれば、レジストリ全体を読み取れないものとして扱う。未知の項目は無視し、未知の版は
// 失敗とする。
func Parse(data []byte, remote bool) (*Registry, error) {
	var raw struct {
		RegistryVersion *int `json:"registry_version"`
		Plugins         map[string]struct {
			Manifests []Rule `json:"manifests"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("not a registry: %w", err)
	}
	if raw.RegistryVersion == nil {
		return nil, errors.New("not a registry: registry_version is missing")
	}
	if *raw.RegistryVersion != Version {
		return nil, fmt.Errorf("unknown registry_version %d (this "+edition.Command+" reads %d)", *raw.RegistryVersion, Version)
	}
	registry := &Registry{Plugins: map[string]Entry{}}
	for name, plugin := range raw.Plugins {
		if !ValidName(name) {
			return nil, fmt.Errorf("invalid name %q", name)
		}
		if len(plugin.Manifests) == 0 {
			return nil, fmt.Errorf("%s: no manifests", name)
		}
		for i, rule := range plugin.Manifests {
			if !matchPattern.MatchString(rule.Match) {
				return nil, fmt.Errorf("%s: manifests[%d]: invalid match %q", name, i, rule.Match)
			}
			if err := CheckRef(strings.ReplaceAll(rule.Path, tagPlaceholder, "tag"), remote); err != nil {
				return nil, fmt.Errorf("%s: manifests[%d]: path: %w", name, i, err)
			}
		}
		registry.Plugins[name] = Entry{Manifests: plugin.Manifests}
	}
	return registry, nil
}

// Select は @ の後ろの文字列に最初に合致した規則を返す。合致しなければ false。取得できなくても次の
// 規則へは進まない（いずれの場所を使用するかが文字列のみで決定し、ネットワークの状態で
// 変わらないため）。タグの綴り（ValidTag）を満たさない文字列にはいずれの規則も合致しない（match の
// 前提）。
func (e Entry) Select(requested string) (Rule, bool) {
	if !ValidTag(requested) {
		return Rule{}, false
	}
	for _, rule := range e.Manifests {
		if match(rule.Match, requested) {
			return rule, true
		}
	}
	return Rule{}, false
}

// Location はマニフェストの場所（{tag} を置換したもの）。
func (r Rule) Location(requested string) string {
	return strings.ReplaceAll(r.Path, tagPlaceholder, requested)
}

// ByTag は規則がタグで直接指すか否か（path に {tag} がある）。そうであればマニフェストの tag が
// 指定と一致することを要求する。
func (r Rule) ByTag() bool {
	return strings.Contains(r.Path, tagPlaceholder)
}

// match はパターン（* は 0 文字以上に合致する）が s 全体に合致するか否か。path.Match の * は / に
// 合致せず、? [ ] \ も特別に扱うが、パターンは matchPattern が、s（タグ）は ValidTag（Select が
// 検証する）がそれらを含まないことを検証しているため、ここでは * のみの照合と同一になる（壊れた
// パターンの誤りも発生しない）。
func match(pattern, s string) bool {
	ok, _ := path.Match(pattern, s)
	return ok
}

// IsURL は参照が https:// の URL か否か（遠隔のもの）。
func IsURL(ref string) bool {
	return strings.HasPrefix(ref, "https://")
}

// isFileURL は参照が file:// の URL か否か（ローカル環境のもの）。
func isFileURL(ref string) bool {
	return strings.HasPrefix(ref, "file://")
}

// FilePath は file:// の URL を、この OS のパスに変換する（docs/registry.md「参照の記法」）。
func FilePath(ref string) (string, error) {
	return filePath(ref, runtime.GOOS == "windows")
}

// filePath は file:// の URL を、windows ならば Windows の、そうでなければ mac・Linux のパスに
// 変換する。%XX はデコードする（空白は %20 と記述する）。ホストは、Windows の
// 共有フォルダ（\\host\share）を指す場合のみ記述できる（localhost は記述しないことと同等）。
// クエリ・フラグメントの付いたもの・相対のものは拒否する。
func filePath(ref string, windows bool) (string, error) {
	switch {
	case !isFileURL(ref):
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
		return "", fmt.Errorf("%q has no absolute path (write file:///...)", ref)
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
	// file:///C:/… の C: の前の / を除去する。ドライブの無いものは絶対ではない。
	if len(path) < 4 || path[2] != ':' || path[3] != '\\' || !isLetter(path[1]) {
		return "", fmt.Errorf("%q has no drive (write file:///C:/...)", ref)
	}
	return path[1:], nil
}

func isLetter(c byte) bool {
	return 'A' <= c && c <= 'Z' || 'a' <= c && c <= 'z'
}

// CheckRef は参照が https:// か file:// の URL かを検証する。remote ならば https:// のみ
// （遠隔のものが利用者のローカル環境のファイルを指さないため）。--registry に渡したものも、
// レジストリの中の参照も、これで検証する。
func CheckRef(ref string, remote bool) error {
	switch {
	case IsURL(ref):
		// ホストの無いもの（https://・https:///x・https://?x）は拒否する（取得を試みてから
		// 失敗させない）。
		if u, err := url.Parse(ref); err != nil || u.Host == "" {
			return fmt.Errorf("%q has no host", ref)
		}
		return nil
	case remote:
		return fmt.Errorf("%q is not an https:// URL (a remote file can refer only to https://)", ref)
	case isFileURL(ref):
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

// Hooks は install と uninstall のフック。いずれも省略できる（nil）。
type Hooks struct {
	Install   []string `json:"install,omitempty"`
	Uninstall []string `json:"uninstall,omitempty"`
}

// ParseManifest はマニフェストを読み取る。remote は https:// で取得したものか否か（そうであれば
// asset は https:// のみ）。
func ParseManifest(data []byte, remote bool) (*Manifest, error) {
	var raw struct {
		ManifestVersion *int   `json:"manifest_version"`
		Tag             string `json:"tag"`
		Targets         map[string]struct {
			Asset  string `json:"asset"`
			SHA256 string `json:"sha256"`
			Hooks  Hooks  `json:"hooks"`
		} `json:"targets"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("not a manifest: %w", err)
	}
	if raw.ManifestVersion == nil {
		return nil, errors.New("not a manifest: manifest_version is missing")
	}
	if *raw.ManifestVersion != manifestVersion {
		return nil, fmt.Errorf("unknown manifest_version %d (this "+edition.Command+" reads %d; to update "+edition.Command+", run the first-time script)",
			*raw.ManifestVersion, manifestVersion)
	}
	if !ValidTag(raw.Tag) {
		return nil, fmt.Errorf("invalid tag %q", raw.Tag)
	}
	manifest := &Manifest{Tag: raw.Tag, Targets: map[string]Asset{}}
	for target, asset := range raw.Targets {
		if err := CheckRef(asset.Asset, remote); err != nil {
			return nil, fmt.Errorf("%s: asset: %w", target, err)
		}
		if !sha256Hex.MatchString(asset.SHA256) {
			return nil, fmt.Errorf("%s: sha256 must be 64 lowercase hexadecimal digits", target)
		}
		if err := asset.Hooks.Check(); err != nil {
			return nil, fmt.Errorf("%s: %w", target, err)
		}
		manifest.Targets[target] = Asset{URL: asset.Asset, SHA256: asset.SHA256, Hooks: asset.Hooks}
	}
	return manifest, nil
}

// Check はフックの記法を検証する（docs/registry.md「記法」）。zip の中に実在するかは、展開した後で
// install が検証する。
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

// interpreters はフックの 1 つ目に記述できる、zip の外のもの。
var interpreters = []string{"sh", "powershell"}

// ViaInterpreter は、フックの argv の 1 つ目が zip の外のインタプリタ（interpreters）か否か。
// そうであれば 2 つ目が zip の中のスクリプト。フックの記法を検証する側（Script）と実行する
// 側（install）が同一のこの関数で判定する。
func ViaInterpreter(argv []string) bool {
	return len(argv) > 0 && slices.Contains(interpreters, argv[0])
}

// Script はフックの argv から、zip の中のファイルの相対パス（/ 区切り）を返す。1 つ目が
// sh・powershell ならば 2 つ目、それ以外ならば 1 つ目。絶対パス・..・- で始まるもの（sh -c のような
// 指定）・\ は拒否する。powershell のスクリプトは .ps1 に限る（Windows PowerShell 5.1 の -File は
// 他の拡張子を拒否するため、差し替えの前に拒否する）。
func Script(argv []string) (string, error) {
	if len(argv) == 0 {
		return "", errors.New("empty")
	}
	script := argv[0]
	if ViaInterpreter(argv) {
		if len(argv) < 2 {
			return "", fmt.Errorf("%s needs the path of a script in the zip", argv[0])
		}
		script = argv[1]
	}
	if argv[0] == "powershell" && !strings.EqualFold(filepath.Ext(script), ".ps1") {
		return "", fmt.Errorf("powershell runs only a .ps1 script, not %q", script)
	}
	if err := checkRelative(script); err != nil {
		return "", err
	}
	return script, nil
}

// checkRelative は、フックが渡すスクリプトの相対パスを検証する。CheckInside に加えて、- で
// 始まるもの（インタープリターが指定と解釈する）も拒否する。
func checkRelative(path string) error {
	if strings.HasPrefix(path, "-") {
		return fmt.Errorf("%q starts with -", path)
	}
	return CheckInside(path)
}

// CheckInside は、zip の中の相対パス（区切りは /）が展開先の外へ出ないことを検証する。zip の項目の
// 名前（install の展開）とフックのスクリプトのパスの両方が使用する（規則を 1 か所に保つ）。
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

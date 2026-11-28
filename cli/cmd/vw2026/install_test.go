package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/edition"
	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/fakeplugin"
	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/install"
)

// fakeInstalled は <Plug-Ins>/<name>/ に vw2026 がインストールしたプラグイン（記録とモジュール）を
// 作成する。
func fakeInstalled(t *testing.T, plugins, name string) {
	t.Helper()
	dir := filepath.Join(plugins, name)
	if err := os.MkdirAll(filepath.Join(dir, name+".vwlibrary"), 0o755); err != nil {
		t.Fatal(err)
	}
	record, _ := json.Marshal(map[string]any{
		"record_version": 1, "name": name, "registry": "default", "tag": "t1",
		"target": "macos-arm64", "module": name + ".vwlibrary", "hooks": map[string]any{},
	})
	if err := os.WriteFile(filepath.Join(dir, install.RecordFile), record, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestInstallUsageErrors(t *testing.T) {
	for _, args := range [][]string{
		{"install"},
		{"install", "cli"},
		{"install", "CLI@latest"},
		{"install", "cli@.."},
		{"install", "cli@latest", "extra"},
		{"uninstall"},
		{"uninstall", "Bad_Name"},
	} {
		r := invoke(t, opts{}, args...)
		if r.code != exitUsage {
			t.Errorf("%v: %+v", args, r)
		}
	}
}

// インストールされていなければ何もせずに成功する。remaining には、残存する記録のあるフォルダの
// 名前が含まれる。場所は、インストールされた CLI ならば自身のインストール先の <Plug-Ins>、
// フォルダの外の CLI ならば --plugins-dir（初回スクリプトが渡す）、無ければ既定の
// 場所（env.pluginsDir）。
func TestUninstallNotInstalled(t *testing.T) {
	flag := t.TempDir()
	own := t.TempDir()
	fallback := t.TempDir()
	fakeInstalled(t, flag, "from-flag")
	fakeInstalled(t, own, "from-own")
	fakeInstalled(t, fallback, "from-default")
	// インストールされた CLI は、自身のインストール先の <Plug-Ins>（<own>/cli/bin/vw2026 と cli の
	// 記録）を使用する。
	fakeInstalled(t, own, "cli")
	installed := filepath.Join(own, "cli", "bin", edition.Command)
	os.MkdirAll(filepath.Dir(installed), 0o755)
	os.WriteFile(installed, nil, 0o700)
	// cli 以外の名前は cli のある <Plug-Ins> でのみ扱うため、フォルダの外の CLI の場合は cli を
	// 除外する（インストールされていない）。
	for _, c := range []struct {
		o    opts
		name string
		args []string
		want string
	}{
		{opts{plugins: fallback}, "cli", nil, "from-default"},
		{opts{plugins: fallback}, "cli", []string{"--plugins-dir", flag}, "from-flag"},
		{opts{plugins: fallback, executable: installed}, "missing", nil, "cli\",\"from-own"},
	} {
		r := invoke(t, c.o, append([]string{"uninstall", c.name}, c.args...)...)
		if r.code != exitOK || strings.TrimSpace(r.stdout) !=
			`{"outcome":"not_installed","name":"`+c.name+`","remaining":["`+c.want+`"],"left":[]}` {
			t.Errorf("%s %v: %+v", c.name, c.args, r)
		}
	}
}

// コマンドの側で検出した使用方法の誤りも、install の中で検出したものと同様に failed の JSON を
// 1 行出力する。
func TestUsageErrorsPrintJSON(t *testing.T) {
	for _, c := range []struct {
		o    opts
		args []string
	}{
		{opts{}, []string{"install", "cli"}},
		{opts{vars: map[string]string{edition.EnvPrefix + "REGISTRY": " "}}, []string{"install", "cli@latest"}},
		{opts{}, []string{"install", "CLI@latest"}},
	} {
		r := invoke(t, c.o, c.args...)
		var out map[string]string
		if r.code != exitUsage || json.Unmarshal([]byte(r.stdout), &out) != nil || out["outcome"] != "failed" ||
			out["message"] == "" || !strings.Contains(r.stderr, out["message"]) {
			t.Errorf("%v: %+v", c.args, r)
		}
	}
}

// 空の --plugins-dir は使用方法の誤り（既定の場所を使用しない）。インストールされた CLI は
// --plugins-dir を受け付けない（初回スクリプトのみが一時フォルダの CLI に渡す）。いずれも何も
// 変更しない。
func TestPluginsDirFlag(t *testing.T) {
	fallback := t.TempDir()
	fakeInstalled(t, fallback, "cli")
	installed := filepath.Join(fallback, "cli", "bin", edition.Command)
	os.MkdirAll(filepath.Dir(installed), 0o755)
	os.WriteFile(installed, nil, 0o700)
	for _, c := range []struct {
		o    opts
		args []string
		want string
	}{
		{opts{plugins: fallback}, []string{"uninstall", "cli", "--plugins-dir", ""}, "the folder of plug-ins is empty"},
		{opts{plugins: fallback}, []string{"install", "cli@latest", "--plugins-dir", ""}, "the folder of plug-ins is empty"},
		{opts{plugins: fallback, executable: installed}, []string{"uninstall", "cli", "--plugins-dir", t.TempDir()},
			"only for the first-time script"},
		{opts{plugins: fallback, executable: installed}, []string{"install", "cli@latest", "--plugins-dir", t.TempDir()},
			"only for the first-time script"},
	} {
		r := invoke(t, c.o, c.args...)
		if r.code != exitUsage || !strings.Contains(r.stderr, c.want) || !strings.Contains(r.stdout, `"outcome":"failed"`) {
			t.Errorf("%v: %+v", c.args, r)
		}
	}
	if !exist(filepath.Join(fallback, "cli", install.RecordFile)) {
		t.Fatal("touched the folder")
	}
}

// Vectorworks が実行中である（<Plug-Ins>/cli/spool のロックが保持されている）場合、何もせずに 6 で
// 終了する。
func TestUninstallWhileRunning(t *testing.T) {
	plugins := t.TempDir()
	fakeInstalled(t, plugins, "cli")
	spoolDir := filepath.Join(plugins, "cli", "spool")
	os.Mkdir(spoolDir, 0o700)
	fakeplugin.HoldLock(t, spoolDir)
	r := invoke(t, opts{plugins: plugins}, "uninstall", "cli")
	if r.code != exitRunning || !strings.Contains(r.stdout, `"outcome":"vectorworks_running"`) ||
		!strings.Contains(r.stderr, "Vectorworks is running") || !exist(filepath.Join(plugins, "cli")) {
		t.Fatalf("%+v", r)
	}
}

// 失敗は標準出力の message と標準エラーに書き込み、1 で終了する。
func TestUninstallRefusal(t *testing.T) {
	plugins := t.TempDir()
	fakeInstalled(t, plugins, "cli")
	os.Remove(filepath.Join(plugins, "cli", install.RecordFile))
	r := invoke(t, opts{plugins: plugins}, "uninstall", "cli")
	if r.code != exitFailure || !strings.Contains(r.stdout, `"outcome":"failed"`) ||
		!strings.Contains(r.stdout, `"message":`) || !strings.Contains(r.stderr, "nothing is removed") {
		t.Fatalf("%+v", r)
	}
}

// 場所を導出できなければ 1 で終了する（初回スクリプトの --plugins-dir があれば既定の場所は
// 不要である）。
func TestDeployPlacesNotLocated(t *testing.T) {
	var stdout, stderr bytes.Buffer
	failing := func() (string, error) { return "", os.ErrNotExist }
	e := env{stdout: &stdout, stderr: &stderr, pluginsDir: failing, workDir: failing, executable: failing}
	if code := run([]string{"uninstall", "cli"}, e); code != exitFailure || !strings.Contains(stderr.String(), "folder of plug-ins") {
		t.Fatalf("%d %s", code, stderr.String())
	}
	stderr.Reset()
	if code := run([]string{"install", "cli@latest"}, e); code != exitFailure || !strings.Contains(stderr.String(), "folder of plug-ins") {
		t.Fatalf("%d %s", code, stderr.String())
	}
	// 実行中の CLI の場所が判明しても（フォルダの外）、既定の場所を導出できなければ 1。
	stderr.Reset()
	outside := e
	outside.executable = func() (string, error) { return filepath.Join(t.TempDir(), edition.Command), nil }
	if code := run([]string{"uninstall", "cli"}, outside); code != exitFailure ||
		!strings.Contains(stderr.String(), "cannot locate the folder of plug-ins") {
		t.Fatalf("%d %s", code, stderr.String())
	}
	stderr.Reset()
	if code := run([]string{"uninstall", "cli", "--plugins-dir", t.TempDir()}, e); code != exitFailure ||
		!strings.Contains(stderr.String(), "work folder") {
		t.Fatalf("%d %s", code, stderr.String())
	}
}

// ローカル環境のファイル（file:// の URL）のレジストリ（VW2026_REGISTRY でも渡せる）から
// インストールし、--check で新しいか否かを検証する。マニフェストには、計算機から決定した
// ターゲットを記述する。値は空白で分割し（VW2026_REGISTRY に 2 つ）、コンマでは分割しない。パスの
// 空白は %20。パスのままは使用方法の誤り。
func TestInstallFromLocalRegistry(t *testing.T) {
	target := install.Target()
	module := "demo.vlb"
	if strings.HasPrefix(target, "macos-") {
		module = "demo.vwlibrary/x"
	}
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, _ := w.Create(module)
	f.Write([]byte("module"))
	w.Close()
	tmp := filepath.Join(t.TempDir(), "a,b c")
	os.Mkdir(tmp, 0o755)
	zipPath := filepath.Join(tmp, "demo.zip")
	os.WriteFile(zipPath, buf.Bytes(), 0o644)
	sum := sha256.Sum256(buf.Bytes())
	manifest, _ := json.Marshal(map[string]any{
		"manifest_version": 1, "tag": "d1",
		"targets": map[string]any{target: map[string]any{"asset": fileURL(zipPath), "sha256": hex.EncodeToString(sum[:])}},
	})
	os.WriteFile(filepath.Join(tmp, "d1.json"), manifest, 0o644)
	registry, _ := json.Marshal(map[string]any{
		"registry_version": 1,
		"plugins": map[string]any{"demo": map[string]any{"manifests": []any{map[string]any{"match": "*",
			"path": strings.Replace(fileURL(filepath.Join(tmp, "x.json")), "x.json", "{tag}.json", 1)}}}},
	})
	os.WriteFile(filepath.Join(tmp, "plugins.json"), registry, 0o644)
	registryPath := fileURL(filepath.Join(tmp, "plugins.json"))
	other := filepath.Join(tmp, "other.json")
	os.WriteFile(other, []byte(`{"registry_version":1,"plugins":{}}`), 0o644)
	plugins := t.TempDir()
	fakeInstalled(t, plugins, "cli")

	if r := invoke(t, opts{plugins: plugins}, "install", "demo@d1", "--registry", filepath.Join(tmp, "plugins.json")); r.code != exitUsage {
		t.Fatalf("a path: %+v", r)
	}
	// 空の値（空白のみを含む）は、引数でも環境変数でも使用方法の誤り。
	r := invoke(t, opts{plugins: plugins, vars: map[string]string{edition.EnvPrefix + "REGISTRY": " "}}, "install", "demo@d1")
	if r.code != exitUsage || !strings.Contains(r.stderr, "the registry is empty") {
		t.Fatalf("an empty "+edition.EnvPrefix+"REGISTRY: %+v", r)
	}
	if r := invoke(t, opts{plugins: plugins}, "install", "demo@d1", "--registry", ""); r.code != exitUsage {
		t.Fatalf("an empty --registry: %+v", r)
	}
	// いずれのレジストリにも無ければ、参照したレジストリを示す。
	r = invoke(t, opts{plugins: plugins}, "install", "demo@d1", "--registry", fileURL(other))
	if r.code != exitFailure || !strings.Contains(r.stdout, "looked in: default "+fileURL(other)+")") {
		t.Fatalf("in no registry: %+v", r)
	}
	vars := map[string]string{edition.EnvPrefix + "REGISTRY": fileURL(other) + " " + registryPath}
	r = invoke(t, opts{plugins: plugins, vars: vars}, "install", "demo@d1", "--check")
	if r.code != exitOK || !strings.Contains(r.stdout, `"outcome":"available"`) || exist(filepath.Join(plugins, "demo")) {
		t.Fatalf("--check: %+v", r)
	}
	r = invoke(t, opts{plugins: plugins}, "install", "demo@d1", "--registry", registryPath)
	var out map[string]any
	if r.code != exitOK || json.Unmarshal([]byte(r.stdout), &out) != nil || out["outcome"] != "installed" ||
		out["registry"] != registryPath || out["tag"] != "d1" || out["target"] != target {
		t.Fatalf("%+v", r)
	}
	r = invoke(t, opts{plugins: plugins, vars: vars}, "install", "demo@d1")
	if r.code != exitOK || !strings.Contains(r.stdout, `"outcome":"up_to_date"`) {
		t.Fatalf("again: %+v", r)
	}
	// ロックが保持されていれば、同じタグでも 6。
	spoolDir := filepath.Join(plugins, "cli", "spool")
	os.Mkdir(spoolDir, 0o700)
	fakeplugin.HoldLock(t, spoolDir)
	r = invoke(t, opts{plugins: plugins, vars: vars}, "install", "demo@d1")
	if r.code != exitRunning || !strings.Contains(r.stdout, `"outcome":"vectorworks_running"`) {
		t.Fatalf("running: %+v", r)
	}
}

func exist(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// インストールした cli の vw2026（フォルダの中の実行ファイル）で uninstall cli を実行すると、何も
// 変更せずに 2 で終了し、アンインストールするための初回スクリプトを示す。
func TestUninstallFromInsideShowsTheScript(t *testing.T) {
	plugins := t.TempDir()
	fakeInstalled(t, plugins, "cli")
	executable := filepath.Join(plugins, "cli", "bin", edition.Command)
	os.MkdirAll(filepath.Dir(executable), 0o755)
	os.WriteFile(executable, nil, 0o700)
	r := invoke(t, opts{plugins: plugins, executable: executable}, "uninstall", "cli")
	var out map[string]string
	if r.code != exitUsage || json.Unmarshal([]byte(r.stdout), &out) != nil || out["outcome"] != "failed" ||
		!strings.Contains(out["command"], "download/t1/"+edition.Bootstrap) || !strings.HasSuffix(out["command"], " uninstall") ||
		!strings.Contains(r.stderr, out["command"]) || !strings.Contains(r.stderr, "latest/download") {
		t.Fatalf("%+v", r)
	}
	if !exist(filepath.Join(plugins, "cli", install.RecordFile)) {
		t.Fatal("removed cli")
	}
	// 既定の場所を導出できない場合も、場所が不明であるため --plugins-dir を含める。
	var stdout, stderr bytes.Buffer
	failing := func() (string, error) { return "", os.ErrNotExist }
	e := env{stdout: &stdout, stderr: &stderr, pluginsDir: failing, workDir: func() (string, error) { return t.TempDir(), nil },
		executable: func() (string, error) { return executable, nil }}
	if code := run([]string{"uninstall", "cli"}, e); code != exitUsage || !strings.Contains(stdout.String(), "--plugins-dir") {
		t.Fatalf("%d %s %s", code, stdout.String(), stderr.String())
	}
	// インストール先の <Plug-Ins> が既定の場所でなければ、初回スクリプトに渡す --plugins-dir を、
	// 示すコマンドに含める（場所はリンクを解決したもの。mac の /private/var など、一時フォルダの
	// 表記と異なりうる）。
	own, _ := install.PluginsDirOf(executable)
	r = invoke(t, opts{executable: executable}, "uninstall", "cli")
	if r.code != exitUsage || json.Unmarshal([]byte(r.stdout), &out) != nil ||
		!strings.HasSuffix(out["command"], " uninstall --plugins-dir '"+own+"'") {
		t.Fatalf("not the default folder: %+v", r)
	}
}

// fileURL は、絶対パス path を指す file:// の URL（Windows は file:///C:/…。空白などは %XX に
// 変換する）。
func fileURL(path string) string {
	slashed := filepath.ToSlash(path)
	if !strings.HasPrefix(slashed, "/") {
		slashed = "/" + slashed
	}
	return (&url.URL{Scheme: "file", Path: slashed}).String()
}

func TestUninstallScript(t *testing.T) {
	tagged := releases + "download/t1/"
	latest := releases + "latest/download/"
	for _, c := range []struct{ goos, tag, dir, want string }{
		{"darwin", "t1", "", "curl -fsSL " + tagged + edition.Bootstrap + ".sh | sh -s -- uninstall"},
		{"darwin", "latest", "", "curl -fsSL " + latest + edition.Bootstrap + ".sh | sh -s -- uninstall"},
		{"darwin", "t1", "/a b/it's", "curl -fsSL " + tagged + edition.Bootstrap + `.sh | sh -s -- uninstall --plugins-dir '/a b/it'\''s'`},
		{"windows", "t1", "", "& ([scriptblock]::Create((irm " + tagged + edition.Bootstrap + ".ps1))) uninstall"},
		{"windows", "latest", `C:\a b\it's`, "& ([scriptblock]::Create((irm " + latest + edition.Bootstrap + `.ps1))) uninstall --plugins-dir 'C:\a b\it''s'`},
		// PowerShell は活字の引用符も単一引用符の区切りとするため、それも重ねる。
		{"windows", "t1", "C:\\O\u2019B\u2018r\u201ai\u201bn", "& ([scriptblock]::Create((irm " + tagged +
			edition.Bootstrap + ".ps1))) uninstall --plugins-dir 'C:\\O\u2019\u2019B\u2018\u2018r\u201a\u201ai\u201b\u201bn'"},
	} {
		if got := uninstallScript(c.goos, c.tag, c.dir); got != c.want {
			t.Errorf("%s %s %q:\n got %s\nwant %s", c.goos, c.tag, c.dir, got, c.want)
		}
	}
}

// 端末（文字型のデバイス）の場合のみ、取得の進捗を同じ行に書き直す。
func TestIsTerminal(t *testing.T) {
	if isTerminal(&bytes.Buffer{}) {
		t.Error("a buffer is not a terminal")
	}
	file, err := os.Create(filepath.Join(t.TempDir(), "log"))
	if err != nil {
		t.Fatal(err)
	}
	if isTerminal(file) {
		t.Error("a file is not a terminal")
	}
	file.Close()
	if isTerminal(file) {
		t.Error("a closed file is not a terminal")
	}
	// os.DevNull は文字型のデバイス（/dev/null・NUL）であるが、端末ではない。
	device, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer device.Close()
	if isTerminal(device) {
		t.Error("the null device is not a terminal")
	}
	// 疑似端末は端末（mac・Linux。terminal_*_test.go）。
	if !isTerminal(openTerminal(t)) {
		t.Error("a pseudo terminal is a terminal")
	}
}

// 記法の誤り（名前・タグ・--registry）は、場所を導出できなくても使用方法の誤り（場所を導出する前に
// 検証する）。
func TestUsageBeforeLocating(t *testing.T) {
	fail := func() (string, error) { return "", errors.New("unknown") }
	for _, args := range [][]string{
		{"install", "Bad_Name@1"},
		{"install", "cli@-bad"},
		{"install", "cli@latest", "--registry", "/plugins.json"},
		{"uninstall", "Bad_Name"},
	} {
		var stdout, stderr bytes.Buffer
		code := run(args, env{
			stdin: strings.NewReader(""), stdout: &stdout, stderr: &stderr,
			spool: fail, workDir: fail, pluginsDir: fail, executable: fail,
		})
		if code != exitUsage || !strings.Contains(stdout.String(), `"outcome":"failed"`) {
			t.Errorf("%v: %d %s %s", args, code, stdout.String(), stderr.String())
		}
	}
}

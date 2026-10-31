package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/fakeplugin"
	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/install"
)

// fakeInstalled は <Plug-Ins>/<name>/ に vw2026 が入れたプラグイン（記録とモジュール）を作る。
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

// 入っていなければ何もせずに成功する。remaining には、残った記録のあるフォルダの名前が載る。
// 場所は --plugins-dir、無ければ VW2026_PLUGINS_DIR、無ければ既定の場所（env.pluginsDir）。
func TestUninstallNotInstalled(t *testing.T) {
	flag := t.TempDir()
	variable := t.TempDir()
	fallback := t.TempDir()
	fakeInstalled(t, flag, "from-flag")
	fakeInstalled(t, variable, "from-env")
	fakeInstalled(t, fallback, "from-default")
	vars := map[string]string{"VW2026_PLUGINS_DIR": variable}
	for _, c := range []struct {
		o    opts
		args []string
		want string
	}{
		{opts{plugins: fallback}, []string{"uninstall", "cli"}, "from-default"},
		{opts{plugins: fallback, vars: vars}, []string{"uninstall", "cli"}, "from-env"},
		{opts{plugins: fallback, vars: vars}, []string{"uninstall", "cli", "--plugins-dir", flag}, "from-flag"},
	} {
		r := invoke(t, c.o, c.args...)
		if r.code != exitOK || strings.TrimSpace(r.stdout) !=
			`{"outcome":"not_installed","name":"cli","remaining":["`+c.want+`"],"left":[]}` {
			t.Errorf("%v: %+v", c.args, r)
		}
	}
}

// Vectorworks が動いている（<Plug-Ins>/cli/spool のロックが掴まれている）と、何もせずに 6 で終わる。
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

// 失敗は標準出力の message と標準エラーに書き、1 で終わる。
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

// 場所を求められなければ 1 で終わる（--plugins-dir があれば既定の場所は要らない）。
func TestDeployPlacesNotLocated(t *testing.T) {
	var stdout, stderr bytes.Buffer
	failing := func() (string, error) { return "", os.ErrNotExist }
	e := env{stdout: &stdout, stderr: &stderr, pluginsDir: failing, workDir: failing, executable: failing}
	if code := run([]string{"uninstall", "cli"}, e); code != exitFailure || !strings.Contains(stderr.String(), "give --plugins-dir") {
		t.Fatalf("%d %s", code, stderr.String())
	}
	stderr.Reset()
	if code := run([]string{"install", "cli@latest"}, e); code != exitFailure || !strings.Contains(stderr.String(), "give --plugins-dir") {
		t.Fatalf("%d %s", code, stderr.String())
	}
	stderr.Reset()
	if code := run([]string{"uninstall", "cli", "--plugins-dir", t.TempDir()}, e); code != exitFailure ||
		!strings.Contains(stderr.String(), "work folder") {
		t.Fatalf("%d %s", code, stderr.String())
	}
}

// 手元のファイル（file:// の URL）のレジストリ（VW2026_REGISTRY でも渡せる）から入れ、--check で新しいかを
// 確かめる。マニフェストには、計算機から決めたターゲットを書く。値は空白で分け（VW2026_REGISTRY に 2 つ）、
// コンマでは分けない。パスの空白は %20。パスのままは使い方の誤り。
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
	vars := map[string]string{"VW2026_REGISTRY": fileURL(other) + " " + registryPath}
	r := invoke(t, opts{plugins: plugins, vars: vars}, "install", "demo@d1", "--check")
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
	// ロックが掴まれていれば、同じタグでも 6。
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

// 入れた cli の vw2026（フォルダの中の実行ファイル）で uninstall cli を走らせると、何も変えずに 2 で
// 終わり、外すための初回のスクリプトを示す。
func TestUninstallFromInsideShowsTheScript(t *testing.T) {
	plugins := t.TempDir()
	fakeInstalled(t, plugins, "cli")
	executable := filepath.Join(plugins, "cli", "bin", "vw2026")
	os.MkdirAll(filepath.Dir(executable), 0o755)
	os.WriteFile(executable, nil, 0o700)
	r := invoke(t, opts{plugins: plugins, executable: executable}, "uninstall", "cli")
	var out map[string]string
	if r.code != exitUsage || json.Unmarshal([]byte(r.stdout), &out) != nil || out["outcome"] != "failed" ||
		!strings.Contains(out["command"], "download/t1/get-vw2026") || !strings.HasSuffix(out["command"], " uninstall") ||
		!strings.Contains(r.stderr, out["command"]) || !strings.Contains(r.stderr, "latest/download") {
		t.Fatalf("%+v", r)
	}
	if !exist(filepath.Join(plugins, "cli", install.RecordFile)) {
		t.Fatal("removed cli")
	}
}

// fileURL は、絶対パス path を指す file:// の URL（Windows は file:///C:/…。空白などは %XX にする）。
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
		{"darwin", "t1", "", "curl -fsSL " + tagged + "get-vw2026.sh | sh -s -- uninstall"},
		{"darwin", "latest", "", "curl -fsSL " + latest + "get-vw2026.sh | sh -s -- uninstall"},
		{"darwin", "t1", "/a b/it's", "curl -fsSL " + tagged + `get-vw2026.sh | sh -s -- uninstall --plugins-dir '/a b/it'\''s'`},
		{"windows", "t1", "", "& ([scriptblock]::Create((irm " + tagged + "get-vw2026.ps1))) uninstall"},
		{"windows", "latest", `C:\a b\it's`, "& ([scriptblock]::Create((irm " + latest + `get-vw2026.ps1))) uninstall --plugins-dir 'C:\a b\it''s'`},
	} {
		if got := uninstallScript(c.goos, c.tag, c.dir); got != c.want {
			t.Errorf("%s %s %q:\n got %s\nwant %s", c.goos, c.tag, c.dir, got, c.want)
		}
	}
}

// 端末（文字型のデバイス）のときだけ、取得の進み具合を同じ行に書き直す。
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
	// os.DevNull は文字型のデバイス（/dev/null・NUL）。
	device, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer device.Close()
	if !isTerminal(device) {
		t.Error("a character device is a terminal")
	}
}

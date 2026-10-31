package install

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/fakeplugin"
	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/registry"
	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/spool"
)

// testTarget はテストが使うターゲット。配置のコードはターゲットからモジュールの拡張子を決めるだけで、
// 走っている OS に依らない（フックの sh は TestHookHelper に差し替える）。
const testTarget = "macos-arm64"

// hookPlan は偽のフック（TestHookHelper）がすること。zip の中のフックのファイルに JSON で書く。
type hookPlan struct {
	// 標準出力に書くもの
	Stdout string `json:"stdout"`
	// 標準出力に書くバイト列（正しくない UTF-8 を書くため。JSON の文字列では書けない）
	Raw []byte `json:"raw,omitempty"`
	// 終了コード
	Exit int `json:"exit"`
	// 引数・作業ディレクトリ・VW2026_HOOK_* を JSON で書き足す（1 行 1 回）ファイル
	Record string `json:"record,omitempty"`
	// 走り始めたら作るファイル
	Signal string `json:"signal,omitempty"`
	// 作られるまで待つファイル（テストがロックを掴むのを待つ）
	Await string `json:"await,omitempty"`
	// 標準出力と標準エラーを受け継いだ子を後ろに残す。子はこのファイルができるまで待ち、終わる前に
	// <このファイル>.done を作る（TestHookLinger）
	Linger string `json:"linger,omitempty"`
}

// hookRun は偽のフックが 1 回走ったときの記録。
type hookRun struct {
	Args []string          `json:"args"`
	Dir  string            `json:"dir"`
	Env  map[string]string `json:"env"`
}

// TestMain は、偽のフック（このテストの実行ファイル）を -race でビルドしたときに、終わる前の待ち
// （既定で 1 秒）をなくす。フックは何度も走るので、待ちがテストの時間の大半になるため。
func TestMain(m *testing.M) {
	if _, ok := os.LookupEnv("GORACE"); !ok {
		os.Setenv("GORACE", "atexit_sleep_ms=0")
	}
	os.Exit(m.Run())
}

// TestHookHelper は偽のフックとして走る（Options.Interpreters の sh をこのテストの実行ファイルに
// 差し替える）。テストとして走ったとき（-- の後ろが無い）は何もしない。sh の無い Windows でも同じ
// フックを走らせるため。
func TestHookHelper(t *testing.T) {
	args := os.Args
	at := -1
	for i, arg := range args {
		if arg == "--" {
			at = i
			break
		}
	}
	if at < 0 || at+1 >= len(args) {
		return
	}
	data, err := os.ReadFile(args[at+1])
	if err != nil {
		os.Stderr.WriteString(err.Error())
		os.Exit(99)
	}
	var plan hookPlan
	if err := json.Unmarshal(data, &plan); err != nil {
		os.Stderr.WriteString(err.Error())
		os.Exit(99)
	}
	if plan.Record != "" {
		dir, _ := os.Getwd()
		run := hookRun{Args: args[at+2:], Dir: dir, Env: map[string]string{}}
		for _, kv := range os.Environ() {
			if name, value, _ := strings.Cut(kv, "="); strings.HasPrefix(name, "VW2026_HOOK_") {
				run.Env[name] = value
			}
		}
		line, _ := json.Marshal(run)
		file, _ := os.OpenFile(plan.Record, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
		file.Write(append(line, '\n'))
		file.Close()
	}
	if plan.Signal != "" {
		os.WriteFile(plan.Signal, nil, 0o600)
	}
	if plan.Await != "" {
		for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
			if _, err := os.Stat(plan.Await); err == nil {
				break
			}
		}
	}
	if plan.Linger != "" {
		child := exec.Command(os.Args[0], "-test.run=^TestHookLinger$")
		child.Env = append(os.Environ(), "VW2026_TEST_LINGER="+plan.Linger)
		// 作業ディレクトリをプラグインのフォルダの外にする（Windows で、テストの片付けが消せなくならないように）。
		child.Dir = os.TempDir()
		child.Stdout = os.Stdout
		child.Stderr = os.Stderr
		child.Start()
	}
	os.Stdout.WriteString(plan.Stdout)
	os.Stdout.Write(plan.Raw)
	os.Exit(plan.Exit)
}

// TestHookLinger は、フックが後ろに残す子として走る（VW2026_TEST_LINGER のファイルができるまで待ち、
// 終わる前に <そのファイル>.done を作る）。テストとして走ったとき（環境変数が無い）は何もしない。
func TestHookLinger(t *testing.T) {
	release := os.Getenv("VW2026_TEST_LINGER")
	if release == "" {
		return
	}
	for deadline := time.Now().Add(60 * time.Second); !exist(release) && time.Now().Before(deadline); {
		time.Sleep(10 * time.Millisecond)
	}
	os.WriteFile(release+".done", nil, 0o600)
}

// harness は一時ディレクトリの <Plug-Ins>・<Work> と、リリースを配る偽の HTTPS サーバー。
type harness struct {
	t       *testing.T
	tmp     string
	plugins string
	work    string
	server  *httptest.Server
	def     *registry.Registry

	mu    sync.Mutex
	files map[string][]byte
	hits  map[string]int
	// パスを配る直前に呼ぶ（ロックを掴むなど）
	before map[string]func()
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	tmp := t.TempDir()
	h := &harness{
		t:       t,
		tmp:     tmp,
		plugins: filepath.Join(tmp, "Plug-Ins"),
		work:    filepath.Join(tmp, "work"),
		files:   map[string][]byte{},
		hits:    map[string]int{},
		before:  map[string]func(){},
	}
	if err := os.MkdirAll(h.plugins, 0o755); err != nil {
		t.Fatal(err)
	}
	h.server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		data, ok := h.files[r.URL.Path]
		h.hits[r.URL.Path]++
		before := h.before[r.URL.Path]
		h.mu.Unlock()
		if before != nil {
			before()
		}
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(data)
	}))
	t.Cleanup(h.server.Close)
	// 既定のレジストリの代わり。cli と demo を、latest は決まった場所、それ以外はタグで指す。
	def, err := registry.Parse([]byte(`{"registry_version":1,"plugins":{`+
		`"cli":{"manifests":[{"match":"latest","path":"`+h.url("/cli/latest/vw2026.json")+`"},`+
		`{"match":"*","path":"`+h.url("/cli/{tag}/vw2026.json")+`"}]},`+
		`"demo":{"manifests":[{"match":"latest","path":"`+h.url("/demo/latest/vw2026.json")+`"},`+
		`{"match":"*","path":"`+h.url("/demo/{tag}/vw2026.json")+`"}]}}}`), true)
	if err != nil {
		t.Fatal(err)
	}
	h.def = def
	return h
}

func (h *harness) url(path string) string {
	return h.server.URL + path
}

func (h *harness) serve(path string, data []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.files[path] = data
}

func (h *harness) hit(path string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.hits[path]
}

func (h *harness) onGet(path string, f func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.before[path] = f
}

func (h *harness) options() Options {
	return Options{
		PluginsDir:   h.plugins,
		WorkDir:      h.work,
		Default:      h.def,
		Target:       testTarget,
		Client:       h.server.Client(),
		Stderr:       testWriter{h.t},
		Interpreters: map[string][]string{"sh": {os.Args[0], "-test.run=^TestHookHelper$", "--"}},
	}
}

type testWriter struct {
	t *testing.T
}

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Logf("hook stderr: %s", p)
	return len(p), nil
}

// entry は zip の項目 1 つ。
type entry struct {
	name string
	body string
	mode fs.FileMode
}

func makeZip(t *testing.T, entries []entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, e := range entries {
		header := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		mode := e.mode
		if mode == 0 {
			mode = 0o644
		}
		header.SetMode(mode)
		f, err := w.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(e.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// release は 1 つのリリース。
type release struct {
	name string
	tag  string
	// 既定の項目（モジュール・版のファイル・フック）の代わりに使う項目
	entries []entry
	// 既定の項目に足す項目
	extra []entry
	// フックの計画。nil なら {"stdout":"{\"ran\":\"<kind>\"}"} で記録を runs に書く
	install   *hookPlan
	uninstall *hookPlan
	// マニフェストのフック。nil なら hooks/install.hook と hooks/uninstall.hook を sh で
	hooks *registry.Hooks
	// マニフェストの sha256 を書き換える
	sha string
	// latest としても配る
	latest bool
}

// runs はフックの記録のファイル。
func (h *harness) runs(name string) string {
	return filepath.Join(h.tmp, name+"-hooks.jsonl")
}

// hookRuns はフックが走った記録を返す。
func (h *harness) hookRuns(name string) []hookRun {
	h.t.Helper()
	data, err := os.ReadFile(h.runs(name))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		h.t.Fatal(err)
	}
	var runs []hookRun
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var run hookRun
		if err := json.Unmarshal([]byte(line), &run); err != nil {
			h.t.Fatal(err)
		}
		runs = append(runs, run)
	}
	return runs
}

func planJSON(t *testing.T, plan hookPlan) string {
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// publish はリリースの zip とマニフェストを配り、zip のパスを返す。
func (h *harness) publish(r release) string {
	h.t.Helper()
	install := hookPlan{Stdout: `{"ran":"install","tag":"` + r.tag + `"}`, Record: h.runs(r.name)}
	if r.install != nil {
		install = *r.install
	}
	uninstall := hookPlan{Stdout: `{"ran":"uninstall"}`, Record: h.runs(r.name)}
	if r.uninstall != nil {
		uninstall = *r.uninstall
	}
	entries := r.entries
	if entries == nil {
		entries = []entry{
			{name: r.name + ".vwlibrary/Contents/MacOS/" + r.name, body: "module " + r.tag, mode: 0o755},
			{name: "version.txt", body: r.tag},
			{name: "bin/vw2026", body: "cli " + r.tag, mode: 0o755},
			{name: "hooks/install.hook", body: planJSON(h.t, install)},
			{name: "hooks/uninstall.hook", body: planJSON(h.t, uninstall)},
		}
	}
	data := makeZip(h.t, append(append([]entry{}, entries...), r.extra...))
	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])
	if r.sha != "" {
		sha = r.sha
	}
	hooks := registry.Hooks{Install: []string{"sh", "hooks/install.hook"}, Uninstall: []string{"sh", "hooks/uninstall.hook"}}
	if r.hooks != nil {
		hooks = *r.hooks
	}
	zipPath := "/" + r.name + "/" + r.tag + "/" + r.name + "-" + testTarget + ".zip"
	manifest, err := json.Marshal(map[string]any{
		"manifest_version": 1,
		"tag":              r.tag,
		"targets": map[string]any{
			testTarget: map[string]any{"asset": h.url(zipPath), "sha256": sha, "hooks": hooks},
		},
	})
	if err != nil {
		h.t.Fatal(err)
	}
	h.serve(zipPath, data)
	h.serve("/"+r.name+"/"+r.tag+"/vw2026.json", manifest)
	if r.latest {
		h.serve("/"+r.name+"/latest/vw2026.json", manifest)
	}
	return zipPath
}

// install は Install を走らせる。
func (h *harness) install(name, tag string) (*InstallResult, error) {
	h.t.Helper()
	return Install(h.options(), name, tag, false)
}

// mustInstall はリリースを配って入れる。
func (h *harness) mustInstall(r release) *InstallResult {
	h.t.Helper()
	h.publish(r)
	result, err := h.install(r.name, r.tag)
	if err != nil {
		h.t.Fatalf("install %s@%s: %v", r.name, r.tag, err)
	}
	return result
}

// installedVersion は <Plug-Ins>/<name>/version.txt（入っている版）。無ければ空。
func (h *harness) installedVersion(name string) string {
	data, err := os.ReadFile(filepath.Join(h.plugins, name, "version.txt"))
	if err != nil {
		return ""
	}
	return string(data)
}

// holdLock は <Plug-Ins>/cli/spool/ のロックを掴む（Vectorworks が動いている）。
func (h *harness) holdLock() func() {
	dir := filepath.Join(h.plugins, CLIName, spool.SpoolDirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		h.t.Fatal(err)
	}
	return fakeplugin.HoldLock(h.t, dir)
}

// snapshot はディレクトリの中身（相対パス → 中身。ディレクトリは "/"）。スプールの中は除く。
func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) && path == dir {
				return filepath.SkipDir
			}
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if d.Name() == spool.SpoolDirName {
				return filepath.SkipDir
			}
			files[rel] = "/"
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[rel] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func sameSnapshot(t *testing.T, before, after map[string]string) {
	t.Helper()
	for path, body := range before {
		if got, ok := after[path]; !ok || got != body {
			t.Errorf("%s changed (%q -> %q, present %v)", path, body, got, ok)
		}
	}
	for path := range after {
		if _, ok := before[path]; !ok {
			t.Errorf("%s appeared", path)
		}
	}
}

// failRename は、from からの名前の変更を失敗させる。
func failRename(t *testing.T, from string) {
	t.Helper()
	original := rename
	rename = func(oldpath, newpath string) error {
		if oldpath == from {
			return &os.LinkError{Op: "rename", Old: oldpath, New: newpath, Err: os.ErrPermission}
		}
		return original(oldpath, newpath)
	}
	t.Cleanup(func() { rename = original })
}

// fileURL は、絶対パス path を指す file:// の URL（Windows は file:///C:/…。空白などは %XX にする）。
func fileURL(path string) string {
	slashed := filepath.ToSlash(path)
	if !strings.HasPrefix(slashed, "/") {
		slashed = "/" + slashed
	}
	return (&url.URL{Scheme: "file", Path: slashed}).String()
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func exist(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// fakeCLI は <Plug-Ins>/cli/ に入れた cli を作る（cli 以外を入れるテストの前提）。
func (h *harness) installCLI() {
	h.t.Helper()
	h.mustInstall(release{name: "cli", tag: "c1"})
}

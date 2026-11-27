package install

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/edition"
	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/registry"
)

// hookEnv はフックに渡す環境変数の中身（docs/registry.md「フックに渡す環境変数」）。
type hookEnv struct {
	name       string
	tag        string
	previous   string
	target     string
	pluginDir  string
	installDir string
}

func (h hookEnv) environ() []string {
	var env []string
	for _, kv := range os.Environ() {
		// 呼ぶ側の環境に同じ名前があっても、こちらの値だけを渡す。ほかの版の名前（VW2025_HOOK_* と
		// VW2026_HOOK_*）も渡さない。cli のフックは渡された名前で版を決め、両方あると断るため
		// （docs/plugin/editions.md）。
		if !isHookVar(kv) {
			env = append(env, kv)
		}
	}
	p := edition.HookEnvPrefix
	return append(env,
		p+"NAME="+h.name,
		p+"TAG="+h.tag,
		p+"PREVIOUS_TAG="+h.previous,
		p+"TARGET="+h.target,
		p+"PLUGIN_DIR="+h.pluginDir,
		p+"INSTALL_DIR="+h.installDir,
	)
}

// isHookVar は、環境の 1 項目 kv が、どれかの版のフックの変数（VW<4 桁>_HOOK_*。大文字と小文字を区別しない）か。
func isHookVar(kv string) bool {
	const head, tail = "VW", "_HOOK_"
	upper := strings.ToUpper(kv)
	if len(upper) < len(head)+4+len(tail) || !strings.HasPrefix(upper, head) {
		return false
	}
	for _, c := range upper[len(head) : len(head)+4] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return strings.HasPrefix(upper[len(head)+4:], tail)
}

// interpreter は、フックの 1 つ目（sh・powershell）を走らせる argv の頭。PATH からは探さない。
func (o *Options) interpreter(name string) ([]string, error) {
	if argv, ok := o.Interpreters[name]; ok {
		return argv, nil
	}
	return defaultInterpreter(name)
}

// scriptIn は、フックが指す zip の中のファイルが、展開したフォルダの中に実在するファイルかを
// 確かめ、そのパスを返す。インタプリタを通さないもの（1 つ目が zip の中のパス）は、この OS が直接走らせ
// られるか（mac は実行の権限、Windows は拡張子）も確かめる。
func scriptIn(dir string, argv []string) (string, error) {
	// 書き方は、マニフェストと記録を読むときに Hooks.Check が確かめている。
	script, _ := registry.Script(argv)
	path := filepath.Join(dir, filepath.FromSlash(script))
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf("the hook %q is not a file in the zip", script)
	}
	switch {
	case registry.ViaInterpreter(argv):
	// Windows は権限では決まらず、拡張子で決まる。PATHEXT は読まない（.vbs・.js なども載るが、それらは
	// CreateProcess では走らない）。
	case runtime.GOOS == "windows":
		if !slices.Contains(windowsRunnable, strings.ToLower(filepath.Ext(script))) {
			return "", fmt.Errorf("the hook %q does not run on Windows (use %s, or run a script with powershell)",
				script, strings.Join(windowsRunnable, ", "))
		}
	case info.Mode().Perm()&0o111 == 0:
		return "", fmt.Errorf("the hook %q has no permission to execute in the zip (give it one, or run it with sh)", script)
	}
	return path, nil
}

// windowsRunnable は、Windows がインタプリタを通さずに走らせるフックの拡張子（.cmd・.bat は cmd.exe が走らせる）。
var windowsRunnable = []string{".exe", ".com", ".cmd", ".bat"}

// runHook はフックを 1 つ走らせ、標準出力の JSON のオブジェクトを返す（docs/registry.md「書き方」）。
// argv が nil なら走らせずに nil。スクリプトを探す場所と作業ディレクトリはプラグインのフォルダ（env.pluginDir）。
// 標準入力は閉じて渡し、標準エラーはそのまま流す。0 以外で終わる・標準出力が空でも JSON のオブジェクト 1 つでも
// ないと失敗にする。時間の上限は持たない。
func (o *Options) runHook(kind string, argv []string, env hookEnv) (json.RawMessage, error) {
	if argv == nil {
		return nil, nil
	}
	path, err := scriptIn(env.pluginDir, argv)
	if err != nil {
		return nil, fmt.Errorf("the %s hook: %w", kind, err)
	}
	var command []string
	// 1 つ目が zip の外のインタプリタなら、2 つ目が zip の中のスクリプト。
	if registry.ViaInterpreter(argv) {
		head, err := o.interpreter(argv[0])
		if err != nil {
			return nil, fmt.Errorf("the %s hook: %w", kind, err)
		}
		command = slices.Concat(head, []string{path}, argv[2:])
	} else {
		command = slices.Concat([]string{path}, argv[1:])
	}
	// 受けるのは小さな JSON のオブジェクト 1 つなので、レジストリと同じ上限までだけを溜める（超えた分は
	// 捨てて読み続け、フックを止めない。終わったあとで失敗にする）。
	stdout := &boundedBuffer{max: maxDocument}
	cmd := exec.Command(command[0], command[1:]...)
	cmd.Dir = env.pluginDir
	cmd.Env = env.environ()
	cmd.Stdout = stdout
	cmd.Stderr = o.stderr()
	// フックが終わったあとは、後ろに残った子（フックが & で起こしたもの）が標準出力・標準エラーを
	// 持ち続けても待たずにパイプを閉じる。フックそのものの時間には上限を持たない（docs/registry.md「書き方」）。
	cmd.WaitDelay = hookWaitDelay
	err = cmd.Run()
	if errors.Is(err, exec.ErrWaitDelay) {
		fmt.Fprintf(o.stderr(), edition.Command+": the %s hook left a process that keeps its output open; "+edition.Command+" stopped waiting for it\n", kind)
		err = nil
	}
	shown := strings.Join(argv, " ")
	if err != nil {
		return nil, fmt.Errorf("the %s hook %q failed: %w", kind, shown, err)
	}
	if stdout.over {
		return nil, fmt.Errorf("the %s hook %q printed more than %d bytes", kind, shown, maxDocument)
	}
	out, err := hookOutput(stdout.buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("the %s hook %q printed %w", kind, shown, err)
	}
	return out, nil
}

// hookWaitDelay は、フックが終わってから、残った子が持つ標準出力・標準エラーを待つ上限。テストが縮める。
var hookWaitDelay = 5 * time.Second

// boundedBuffer は、max バイトまでを溜め、超えた分は捨てて over にする io.Writer。bytes.Buffer を埋め込まない
// （埋め込むと ReadFrom が見え、io.Copy が Write を通らずに上限なしに読む）。
type boundedBuffer struct {
	buf  bytes.Buffer
	max  int
	over bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if room := b.max - b.buf.Len(); n > room {
		b.over = true
		p = p[:room]
	}
	b.buf.Write(p)
	return n, nil
}

// hookOutput はフックの標準出力を、1 行の JSON のオブジェクトにする。空なら {}。先頭の BOM と
// 前後の空白は許す。
func hookOutput(data []byte) (json.RawMessage, error) {
	data = trimBOM(data)
	if !utf8.Valid(data) {
		return nil, errors.New("invalid UTF-8")
	}
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return json.RawMessage("{}"), nil
	}
	if data[0] != '{' {
		return nil, errors.New("something other than a JSON object")
	}
	// Compact は構文を確かめ、値が 2 つ以上あっても断る。
	var compact bytes.Buffer
	if err := json.Compact(&compact, data); err != nil {
		return nil, fmt.Errorf("broken JSON: %v", err)
	}
	return compact.Bytes(), nil
}

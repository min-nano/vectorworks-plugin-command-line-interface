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

// hookEnv はフックに渡す環境変数の内容（docs/registry.md「フックに渡す環境変数」）。
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
		// 呼び出し元の環境に同じ名前があっても、こちらの値のみを渡す。他の版の名前
		// （VW2025_HOOK_* と VW2026_HOOK_*）も渡さない。cli のフックは渡された名前で版を決定し、
		// 両方が存在すると拒否するため（docs/plugin/editions.md）。
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

// isHookVar は、環境の 1 項目 kv が、いずれかの版のフックの変数（VW<4 桁>_HOOK_*。
// 大文字と小文字を区別しない）か。
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

// interpreter は、フックの 1 つ目（sh・powershell）を実行する argv の先頭。PATH からは探索しない。
func (o *Options) interpreter(name string) ([]string, error) {
	if argv, ok := o.Interpreters[name]; ok {
		return argv, nil
	}
	return defaultInterpreter(name)
}

// scriptIn は、フックが指す zip の中のファイルが、展開したフォルダの中に実在するファイルかを
// 検証し、そのパスを返す。インタプリタを経由しないもの（1 つ目が zip の中のパス）は、
// この OS が直接実行できるか（mac は実行の権限、Windows は拡張子）も検証する。
func scriptIn(dir string, argv []string) (string, error) {
	// 記法は、マニフェストと記録を読み取る際に Hooks.Check が検証している。
	script, _ := registry.Script(argv)
	path := filepath.Join(dir, filepath.FromSlash(script))
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf("the hook %q is not a file in the zip", script)
	}
	switch {
	case registry.ViaInterpreter(argv):
	// Windows は権限では決定されず、拡張子で決定される。PATHEXT は読み取らない（.vbs・.js なども
	// 含まれるが、それらは CreateProcess では実行されない）。
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

// windowsRunnable は、Windows がインタプリタを経由せずに実行するフックの拡張子（.cmd・.bat は
// cmd.exe が実行する）。
var windowsRunnable = []string{".exe", ".com", ".cmd", ".bat"}

// runHook はフックを 1 つ実行し、標準出力の JSON のオブジェクトを返す（docs/registry.md「記法」）。
// argv が nil ならば実行せずに nil。スクリプトを探索する場所と作業ディレクトリはプラグインの
// フォルダ（env.pluginDir）。
// 標準入力は閉じて渡し、標準エラーはそのまま出力する。0 以外で終了する・標準出力が空でも JSON の
// オブジェクト 1 つでもない場合は失敗とする。時間の上限は設けない。
func (o *Options) runHook(kind string, argv []string, env hookEnv) (json.RawMessage, error) {
	if argv == nil {
		return nil, nil
	}
	path, err := scriptIn(env.pluginDir, argv)
	if err != nil {
		return nil, fmt.Errorf("the %s hook: %w", kind, err)
	}
	var command []string
	// 1 つ目が zip の外のインタプリタならば、2 つ目が zip の中のスクリプト。
	if registry.ViaInterpreter(argv) {
		head, err := o.interpreter(argv[0])
		if err != nil {
			return nil, fmt.Errorf("the %s hook: %w", kind, err)
		}
		command = slices.Concat(head, []string{path}, argv[2:])
	} else {
		command = slices.Concat([]string{path}, argv[1:])
	}
	// 受け取るのは小さな JSON のオブジェクト 1 つであるため、レジストリと同じ上限までのみを蓄積する
	// （超過分は破棄して読み取りを続け、フックを停止させない。終了した後で失敗とする）。
	stdout := &boundedBuffer{max: maxDocument}
	cmd := exec.Command(command[0], command[1:]...)
	cmd.Dir = env.pluginDir
	cmd.Env = env.environ()
	cmd.Stdout = stdout
	cmd.Stderr = o.stderr()
	// フックが終了した後は、後方に残存した子（フックが & で起動したもの）が標準出力・標準エラーを
	// 保持し続けても待機せずにパイプを閉じる。フックそのものの時間には上限を設けない
	// （docs/registry.md「記法」）。
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

// hookWaitDelay は、フックが終了してから、残存した子が保持する標準出力・標準エラーを待機する上限。
// テストが縮小する。
var hookWaitDelay = 5 * time.Second

// boundedBuffer は、max バイトまでを蓄積し、超過分は破棄して over とする io.Writer。
// bytes.Buffer を埋め込まない（埋め込むと ReadFrom が公開され、io.Copy が Write を経由せずに
// 上限なしに読み取る）。
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

// hookOutput はフックの標準出力を、1 行の JSON のオブジェクトとする。空ならば {}。
// 先頭の BOM と前後の空白は許容する。
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
	// Compact は構文を検証し、値が 2 つ以上あっても拒否する。
	var compact bytes.Buffer
	if err := json.Compact(&compact, data); err != nil {
		return nil, fmt.Errorf("broken JSON: %v", err)
	}
	return compact.Bytes(), nil
}

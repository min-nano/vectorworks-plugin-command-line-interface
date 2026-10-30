package install

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf8"

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
		// 呼ぶ側の環境に同じ名前があっても、こちらの値だけを渡す。
		if !strings.HasPrefix(strings.ToUpper(kv), "VW2026_HOOK_") {
			env = append(env, kv)
		}
	}
	return append(env,
		"VW2026_HOOK_NAME="+h.name,
		"VW2026_HOOK_TAG="+h.tag,
		"VW2026_HOOK_PREVIOUS_TAG="+h.previous,
		"VW2026_HOOK_TARGET="+h.target,
		"VW2026_HOOK_PLUGIN_DIR="+h.pluginDir,
		"VW2026_HOOK_INSTALL_DIR="+h.installDir,
	)
}

// interpreter は、フックの 1 つ目（sh・powershell）を走らせる argv の頭。PATH からは探さない。
func (o *Options) interpreter(name string) ([]string, error) {
	if argv, ok := o.Interpreters[name]; ok {
		return argv, nil
	}
	switch {
	case name == "sh" && runtime.GOOS != "windows":
		return []string{"/bin/sh"}, nil
	case name == "powershell" && runtime.GOOS == "windows":
		root := os.Getenv("SystemRoot")
		if root == "" {
			root = `C:\Windows`
		}
		return []string{
			filepath.Join(root, "System32", "WindowsPowerShell", "v1.0", "powershell.exe"),
			"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File",
		}, nil
	}
	return nil, fmt.Errorf("%s hooks do not run on %s", name, runtime.GOOS)
}

// scriptIn は、フックが指す zip の中のファイルが、展開したフォルダの中に実在するファイルかを
// 確かめ、そのパスを返す。
func scriptIn(dir string, argv []string) (string, error) {
	// 書き方は、マニフェストと記録を読むときに Hooks.Check が確かめている。
	script, _ := registry.Script(argv)
	path := filepath.Join(dir, filepath.FromSlash(script))
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf("the hook %q is not a file in the zip", script)
	}
	return path, nil
}

// runHook はフックを 1 つ走らせ、標準出力の JSON のオブジェクトを返す（docs/registry.md「書き方」）。
// argv が nil なら走らせずに nil。作業ディレクトリはプラグインのフォルダ、標準入力は閉じて渡し、
// 標準エラーはそのまま流す。0 以外で終わる・標準出力が空でも JSON のオブジェクト 1 つでもないと
// 失敗にする。時間の上限は持たない。
func (o *Options) runHook(kind string, argv []string, dir string, env hookEnv) (json.RawMessage, error) {
	if argv == nil {
		return nil, nil
	}
	path, err := scriptIn(dir, argv)
	if err != nil {
		return nil, fmt.Errorf("the %s hook: %w", kind, err)
	}
	var command []string
	if argv[0] == "sh" || argv[0] == "powershell" {
		head, err := o.interpreter(argv[0])
		if err != nil {
			return nil, fmt.Errorf("the %s hook: %w", kind, err)
		}
		command = append(append(append(command, head...), path), argv[2:]...)
	} else {
		command = append([]string{path}, argv[1:]...)
	}
	var stdout bytes.Buffer
	cmd := exec.Command(command[0], command[1:]...)
	cmd.Dir = dir
	cmd.Env = env.environ()
	cmd.Stdout = &stdout
	cmd.Stderr = o.Stderr
	if o.Stderr == nil {
		cmd.Stderr = io.Discard
	}
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("the %s hook %q failed: %w", kind, strings.Join(argv, " "), err)
	}
	out, err := hookOutput(stdout.Bytes())
	if err != nil {
		return nil, fmt.Errorf("the %s hook %q printed %w", kind, strings.Join(argv, " "), err)
	}
	return out, nil
}

// hookOutput はフックの標準出力を、1 行の JSON のオブジェクトにする。空なら {}。先頭の BOM と
// 前後の空白は許す。
func hookOutput(data []byte) (json.RawMessage, error) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
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

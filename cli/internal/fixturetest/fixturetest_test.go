package fixturetest

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fatalRecorder は Fatal を覚えて、呼んだゴルーチンを止める（testing.T と同じく、そこで戻らない）。
type fatalRecorder struct {
	testing.TB
	message string
}

func (r *fatalRecorder) Helper() {}

func (r *fatalRecorder) Fatal(args ...any) {
	r.message = fmt.Sprint(args...)
	runtime.Goexit()
}

func (r *fatalRecorder) Fatalf(format string, args ...any) {
	r.message = fmt.Sprintf(format, args...)
	runtime.Goexit()
}

// fatalOf は f を別のゴルーチンで走らせ、Fatal の文言を返す（Fatal が無ければ空）。
func fatalOf(t *testing.T, f func(tb testing.TB)) string {
	t.Helper()
	recorder := &fatalRecorder{TB: t}
	done := make(chan struct{})
	go func() {
		defer close(done)
		f(recorder)
	}()
	<-done
	return recorder.message
}

func TestReadStrictReadsFixtures(t *testing.T) {
	var constants struct {
		Protocol int `json:"protocol"`
	}
	var whole map[string]any
	ReadStrict(t, filepath.Join(ProtocolDir, "constants.json"), &whole)
	if _, ok := whole["protocol"]; !ok {
		t.Fatal("constants.json has no protocol")
	}
	// 知らないキーを誤りとして読むので、一部のキーだけの構造体では読めない。
	message := fatalOf(t, func(tb testing.TB) {
		ReadStrict(tb, filepath.Join(ProtocolDir, "constants.json"), &constants)
	})
	if !strings.Contains(message, "constants.json") || !strings.Contains(message, "unknown field") {
		t.Errorf("Fatal = %q, want the path and an unknown field", message)
	}
	var reals map[string]any
	ReadStrict(t, filepath.Join(ToolDir, "reals.json"), &reals)
	if _, ok := reals["reals"]; !ok {
		t.Fatal("reals.json has no reals")
	}
}

func TestReadStrictStopsWithoutTheFile(t *testing.T) {
	var v any
	message := fatalOf(t, func(tb testing.TB) {
		ReadStrict(tb, filepath.Join(t.TempDir(), "missing.json"), &v)
	})
	if !strings.Contains(message, "missing.json") {
		t.Errorf("Fatal = %q, want the missing path", message)
	}
}

func TestDecodeStrictRejectsUnknownFields(t *testing.T) {
	var v struct {
		A int `json:"a"`
	}
	DecodeStrict(t, "known", []byte(`{"a":1}`), &v)
	if v.A != 1 {
		t.Errorf("a = %d, want 1", v.A)
	}
	message := fatalOf(t, func(tb testing.TB) {
		DecodeStrict(tb, "where", []byte(`{"a":1,"b":2}`), &v)
	})
	if !strings.HasPrefix(message, "where: ") || !strings.Contains(message, `"b"`) {
		t.Errorf("Fatal = %q, want where and the unknown field", message)
	}
}

// TestCommandDoesNotDependOnTesting は、配る実行ファイル（cmd）の依存に testing が無いことを
// 確かめる。testing を取り込むパッケージ（fixturetest・fakeplugin）をテスト以外から取り込むと落ちる。
func TestCommandDoesNotDependOnTesting(t *testing.T) {
	for _, tags := range []string{"", "vw2025"} {
		out, err := exec.Command("go", "list", "-deps", "-tags", tags, "../../cmd/...").CombinedOutput()
		if err != nil {
			t.Fatalf("go list -tags %q: %v\n%s", tags, err, out)
		}
		for _, pkg := range strings.Fields(string(out)) {
			if pkg == "testing" {
				t.Errorf("cmd depends on testing (tags %q)", tags)
			}
		}
	}
}

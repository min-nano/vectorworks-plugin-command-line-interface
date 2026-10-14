package spool_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	. "github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/spool"
)

// NestingDepth は手書きの字句処理で、プラグインの入れ子の上限（MaxNestingDepth）と食い違うと、
// 断るべき要求を置く・置ける要求を断る。encoding/json が数えた深さと比べて確かめる。
// 種はプラグイン側のファジング（tests/JsonFuzzer.cpp）と同じ要求の見本（docs/fixtures.md）。
// go test では種だけを確かめ、CI（go.yml）が -fuzz で決まった時間だけ回す。
func FuzzNestingDepth(f *testing.F) {
	paths, err := filepath.Glob(filepath.Join(fixtures, "requests", "*"))
	if err != nil {
		f.Fatal(err)
	}
	if len(paths) == 0 {
		f.Fatal("no request fixtures")
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(data)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		got := NestingDepth(data)
		if got < 0 {
			t.Fatalf("negative depth %d", got)
		}
		// JSON として正しくないものは、落ちないことだけを確かめる（解析する前に深さを見るので、
		// 正しくない入力の深さは決めていない）。
		if !json.Valid(data) {
			return
		}
		if want := tokenDepth(t, data); got != want {
			t.Fatalf("want %d, got %d", want, got)
		}
	})
}

// tokenDepth は encoding/json の字句の列から入れ子の深さを数える。any へ Unmarshal すると
// 再帰で数えることになるので、字句の列で数える。
func tokenDepth(t *testing.T, data []byte) int {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(data))
	// 数を float64 に直さない（1e700 のような正しい JSON の数で Token が失敗しないように）。
	decoder.UseNumber()
	depth, deepest := 0, 0
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return deepest
		}
		if err != nil {
			t.Fatalf("json.Valid accepted but Token failed: %v", err)
		}
		switch token {
		case json.Delim('{'), json.Delim('['):
			depth++
			deepest = max(deepest, depth)
		case json.Delim('}'), json.Delim(']'):
			depth--
		}
	}
}

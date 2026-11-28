package spool_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/fixturetest"
	. "github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/spool"
)

// NestingDepth は手書きの字句処理なので、encoding/json と同じ深さを返すことをファジングで検証する。
// プラグイン（src/core）の数え方との一致は、通信規則のフィクスチャの depth-64・depth-65 で検証する
// （docs/fixtures.md）。go test ではシードのみを検証し、CI（go.yml）が -fuzz で定められた時間のみ
// 実行する。

// FuzzNestingDepth は任意のバイト列を与える。シードはプラグイン側のファジング
// （tests/JsonFuzzer.cpp）と同じ要求のフィクスチャ。
func FuzzNestingDepth(f *testing.F) {
	paths, err := filepath.Glob(filepath.Join(fixturetest.ProtocolDir, "requests", "*"))
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
		// 正しくない入力の深さは定めていない（解析する前に深さを確認するため）が、開く括弧の数は
		// 超えない。
		if opens := bytes.Count(data, []byte("{")) + bytes.Count(data, []byte("[")); got > opens {
			t.Fatalf("depth %d exceeds %d opening brackets", got, opens)
		}
		if !json.Valid(data) {
			return
		}
		if want := tokenDepth(t, data); got != want {
			t.Fatalf("want %d, got %d", want, got)
		}
	})
}

// FuzzNestingDepthOfRequest は Call が NestingDepth に渡す形（json.Marshal した要求）を構築して
// 与える。バイト列を変更するファジングは大半が正しくない JSON になり比較まで到達しないので、
// 正しい JSON のみを多く試すために分割する。text には引用符・\・括弧を含む文字列が入る。
func FuzzNestingDepthOfRequest(f *testing.F) {
	f.Add(uint8(0), "")
	f.Add(uint8(3), `[{"\]`)
	f.Add(uint8(MaxNestingDepth), strings.Repeat("[", MaxNestingDepth))
	f.Fuzz(func(t *testing.T, depth uint8, text string) {
		// 入れ子を depth 段にする。オブジェクトと配列を交互に使用し、キーと要素にも text を
		// 配置する。
		var value any = text
		for i := range int(depth) {
			if i%2 == 0 {
				value = map[string]any{text: value}
			} else {
				value = []any{text, value}
			}
		}
		args, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		payload, err := json.Marshal(Request{Tool: text, Args: args, Session: text})
		if err != nil {
			t.Fatal(err)
		}
		// 外側の要求のオブジェクトの 1 段を追加する。
		if got, want := NestingDepth(payload), int(depth)+1; got != want {
			t.Fatalf("want %d, got %d: %s", want, got, payload)
		}
	})
}

// tokenDepth は encoding/json の字句の列から入れ子の深さを数える。any へ Unmarshal すると
// 再帰で数えることになるので、字句の列で数える。
func tokenDepth(t *testing.T, data []byte) int {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(data))
	// 数を float64 に変換しない（1e700 のような正しい JSON の数で Token が失敗しないように）。
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

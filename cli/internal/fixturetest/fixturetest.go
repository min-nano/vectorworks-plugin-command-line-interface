// Package fixturetest は、テストがフィクスチャ（通信規則のフィクスチャ protocol/fixtures と、
// ツールの結果の規則のフィクスチャ tests/fixtures）を読み込むための小規模な補助。
//
// テスト（_test.go）からのみインポートする。testing をインポートするので、cmd からインポートすると
// 配布する実行ファイルに testing が含まれ、フィクスチャの相対パスも実行時の作業ディレクトリから
// 解決されてしまう。TestCommandDoesNotDependOnTesting が、実行ファイルの依存に testing が無いことを
// 検証する。
package fixturetest

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

// フィクスチャの場所。go test はパッケージのディレクトリで実行されるので、
// cli/internal/<パッケージ> からの相対パスで記述する。
const (
	// ProtocolDir は通信規則のフィクスチャ（docs/fixtures.md）の場所。
	ProtocolDir = "../../../protocol/fixtures"
	// ToolDir はツールの結果の規則のフィクスチャ（通信規則のフィクスチャとは別）の場所。
	ToolDir = "../../../tests/fixtures"
)

// ReadStrict はフィクスチャのファイルを読み取り、DecodeStrict で v へ読み込む。読み込めなければ
// テストを停止する。
func ReadStrict(t testing.TB, path string, v any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	DecodeStrict(t, path, data, v)
}

// DecodeStrict は未知のキーをエラーとして読み込む。フィクスチャのキーの綴りを誤ると、その検証が
// 黙って省略されるため。読み込めなければ where を添えてテストを停止する。
func DecodeStrict(t testing.TB, where string, data []byte, v any) {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(v); err != nil {
		t.Fatalf("%s: %v", where, err)
	}
}

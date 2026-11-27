// Package fixturetest は、テストが見本（通信規則の見本 protocol/fixtures と、道具の結果の決まりの
// 見本 tests/fixtures）を読むための小さな助け。
//
// テスト（_test.go）からだけ取り込む。testing を取り込むので、cmd から取り込むと配る実行ファイルに
// testing が入り、見本の相対のパスも実行時の作業ディレクトリから解決されてしまう。
// TestCommandDoesNotDependOnTesting が、実行ファイルの依存に testing が無いことを確かめる。
package fixturetest

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

// 見本の場所。go test はパッケージのディレクトリで走るので、cli/internal/<パッケージ> からの
// 相対のパスで書く。
const (
	// ProtocolDir は通信規則の見本（docs/fixtures.md）の場所。
	ProtocolDir = "../../../protocol/fixtures"
	// ToolDir は道具の結果の決まりの見本（通信規則の見本とは別）の場所。
	ToolDir = "../../../tests/fixtures"
)

// ReadStrict は見本のファイルを読み、DecodeStrict で v へ読む。読めなければテストを止める。
func ReadStrict(t testing.TB, path string, v any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	DecodeStrict(t, path, data, v)
}

// DecodeStrict は知らないキーを誤りとして読む。見本のキーの綴りを誤ると、その確かめが黙って
// 省かれるため。読めなければ where を添えてテストを止める。
func DecodeStrict(t testing.TB, where string, data []byte, v any) {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(v); err != nil {
		t.Fatalf("%s: %v", where, err)
	}
}

// Package toolfixtures_test は、道具の結果の決まりの見本（tests/fixtures。通信規則の見本とは別。
// docs/plugin/tools.md「最初に提供する道具」の冒頭）を Go の側から確かめる。プラグイン（C++）の
// テストも同じ見本を読むので、片側のテストに書いた期待だけで綴りを決めずに済む。
package toolfixtures_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// fixtures は道具の結果の見本の場所（go test はパッケージのディレクトリで走る）。
const fixtures = "../../../tests/fixtures"

// TestRealsMatchEncodingJSON は、見本の実数と綴りの組が json.Marshal の綴りと一致することを
// 確かめる（プラグインの結果の実数の綴りを Go の encoding/json に合わせる決まり）。
func TestRealsMatchEncodingJSON(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(fixtures, "reals.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Reals []struct {
			Value string `json:"value"`
			Text  string `json:"text"`
		} `json:"reals"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Reals) == 0 {
		t.Fatal("reals.json has no reals")
	}
	for _, pair := range fixture.Reals {
		value, err := strconv.ParseFloat(pair.Value, 64)
		if err != nil {
			t.Errorf("%s: %v", pair.Value, err)
			continue
		}
		got, err := json.Marshal(value)
		if err != nil {
			t.Errorf("%s: %v", pair.Value, err)
			continue
		}
		if string(got) != pair.Text {
			t.Errorf("%s: json.Marshal = %s, want %s", pair.Value, got, pair.Text)
		}
	}
}

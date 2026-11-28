// Package toolfixtures_test は、道具の結果の決まりの見本（tests/fixtures。通信規則の見本とは別。
// docs/plugin/tools.md「共通の規則」）を Go の側から確かめる。プラグイン（C++）の
// テストも同じ見本を読むので、片側のテストに書いた期待だけで綴りを決めずに済む。
package toolfixtures_test

import (
	"encoding/json"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/fixturetest"
)

// TestRealsMatchEncodingJSON は、見本の実数と綴りの組が json.Marshal の綴りと一致することを
// 確かめる（プラグインの結果の実数の綴りを Go の encoding/json に合わせる決まり）。
func TestRealsMatchEncodingJSON(t *testing.T) {
	var fixture struct {
		Reals []struct {
			Value string `json:"value"`
			Text  string `json:"text"`
		} `json:"reals"`
	}
	fixturetest.ReadStrict(t, filepath.Join(fixturetest.ToolDir, "reals.json"), &fixture)
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

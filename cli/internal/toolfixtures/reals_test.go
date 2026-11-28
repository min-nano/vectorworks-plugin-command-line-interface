// Package toolfixtures_test は、ツールの結果の規則のフィクスチャ（tests/fixtures。通信規則の
// フィクスチャとは別。docs/plugin/tools.md「共通の規則」）を Go の側から検証する。プラグイン（C++）
// のテストも同一のフィクスチャを読み込むので、片側のテストに記述した期待のみで綴りを決定せずに
// 済む。
package toolfixtures_test

import (
	"encoding/json"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/fixturetest"
)

// TestRealsMatchEncodingJSON は、フィクスチャの実数と綴りの組が json.Marshal の綴りと一致することを
// 検証する（プラグインの結果の実数の綴りを Go の encoding/json に合致させる規則）。
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

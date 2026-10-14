package spool_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	. "github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/spool"
)

// 通信規則の見本（protocol/fixtures。形は docs/fixtures.md）の定数と id の綴りに照らす。プラグイン側
// （src/core/Protocol.h）も同じ見本に照らすので、両側の綴りが揃う。

// fixtures は通信規則の見本の場所（go test はパッケージのディレクトリで走る）。
const fixtures = "../../../protocol/fixtures"

// readFixture は見本のファイルを、知らないキーを誤りとして読む（綴りを誤った確かめが黙って
// 省かれないように）。
func readFixture(t *testing.T, name string, v any) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(fixtures, name))
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(v); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
}

func sorted(values ...string) []string {
	sort.Strings(values)
	return values
}

func TestConstantsMatchFixtures(t *testing.T) {
	var c struct {
		Protocol        int      `json:"protocol"`
		AppDir          string   `json:"app_dir"`
		SpoolDir        string   `json:"spool_dir"`
		LockFile        string   `json:"lock_file"`
		RequestSuffix   string   `json:"request_suffix"`
		ResponseSuffix  string   `json:"response_suffix"`
		WaitSuffix      string   `json:"wait_suffix"`
		TempSuffix      string   `json:"temp_suffix"`
		MaxRequestBytes int      `json:"max_request_bytes"`
		MaxNestingDepth int      `json:"max_nesting_depth"`
		ReservedTools   []string `json:"reserved_tools"`
		// CLI は道具の種類を読まないので、照らす定数が無い（プラグイン側だけが照らす）。
		Kinds []string `json:"kinds"`
		Codes []string `json:"codes"`
	}
	readFixture(t, "constants.json", &c)
	got := []any{ProtocolVersion, AppDirName, SpoolDirName, LockFile, RequestSuffix, ResponseSuffix,
		WaitSuffix, TempSuffix, MaxRequestBytes, MaxNestingDepth}
	want := []any{c.Protocol, c.AppDir, c.SpoolDir, c.LockFile, c.RequestSuffix, c.ResponseSuffix,
		c.WaitSuffix, c.TempSuffix, c.MaxRequestBytes, c.MaxNestingDepth}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("constants %v, want %v", got, want)
	}
	if tools := sorted(ToolTools, ToolPing, ToolSessionStart, ToolSessionEnd); !reflect.DeepEqual(tools, sorted(c.ReservedTools...)) {
		t.Errorf("reserved tools %v, want %v", tools, c.ReservedTools)
	}
	codes := sorted(CodeInvalidRequest, CodeUnknownTool, CodeInvalidArgs, CodeNoWait, CodeBusy, CodeNoSession, CodeInternal)
	if !reflect.DeepEqual(codes, sorted(c.Codes...)) {
		t.Errorf("codes %v, want %v", codes, c.Codes)
	}
}

func TestValidIDMatchesFixtures(t *testing.T) {
	var ids struct {
		Valid   []string `json:"valid"`
		Invalid []string `json:"invalid"`
	}
	readFixture(t, "ids.json", &ids)
	for _, id := range ids.Valid {
		if !ValidID(id) {
			t.Errorf("%q should be valid", id)
		}
	}
	for _, id := range ids.Invalid {
		if ValidID(id) {
			t.Errorf("%q should be invalid", id)
		}
	}
}

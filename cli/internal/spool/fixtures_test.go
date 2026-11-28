package spool_test

import (
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/fixturetest"
	. "github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/spool"
)

// 通信規則のフィクスチャ（protocol/fixtures。形式は docs/fixtures.md）の定数と id の綴りと
// 照合する。プラグイン側（src/core/Protocol.h）も同一のフィクスチャと照合するので、両側の綴りが
// 統一される。

// readFixture はフィクスチャのファイルを、未知のキーをエラーとして読み込む。
func readFixture(t *testing.T, name string, v any) {
	t.Helper()
	fixturetest.ReadStrict(t, filepath.Join(fixturetest.ProtocolDir, name), v)
}

func sorted(values ...string) []string {
	sort.Strings(values)
	return values
}

func TestConstantsMatchFixtures(t *testing.T) {
	var c struct {
		Protocol        int      `json:"protocol"`
		SpoolDir        string   `json:"spool_dir"`
		LockFile        string   `json:"lock_file"`
		RequestSuffix   string   `json:"request_suffix"`
		ResponseSuffix  string   `json:"response_suffix"`
		WaitSuffix      string   `json:"wait_suffix"`
		TempSuffix      string   `json:"temp_suffix"`
		MaxRequestBytes int      `json:"max_request_bytes"`
		MaxNestingDepth int      `json:"max_nesting_depth"`
		ReservedTools   []string `json:"reserved_tools"`
		// CLI はツールの種類を読み取らないので、照合する定数が無い（プラグイン側のみが照合する）。
		Kinds []string `json:"kinds"`
		Codes []string `json:"codes"`
	}
	readFixture(t, "constants.json", &c)
	got := []any{ProtocolVersion, SpoolDirName, LockFile, RequestSuffix, ResponseSuffix,
		WaitSuffix, TempSuffix, MaxRequestBytes, MaxNestingDepth}
	want := []any{c.Protocol, c.SpoolDir, c.LockFile, c.RequestSuffix, c.ResponseSuffix,
		c.WaitSuffix, c.TempSuffix, c.MaxRequestBytes, c.MaxNestingDepth}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("constants %v, want %v", got, want)
	}
	if tools := sorted(ToolTools, ToolPing, ToolSessionStart, ToolSessionEnd); !reflect.DeepEqual(tools, sorted(c.ReservedTools...)) {
		t.Errorf("reserved tools %v, want %v", tools, c.ReservedTools)
	}
	codes := sorted(CodeInvalidRequest, CodeUnknownTool, CodeInvalidArgs, CodeNoWait, CodeBusy, CodeOccupied, CodeNoSession, CodeInternal)
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

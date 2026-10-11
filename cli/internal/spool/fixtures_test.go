package spool

import (
	"encoding/json"
	"os"
	"slices"
	"testing"
)

// 作法の定数を見本（protocol/fixtures/constants.json）に照らす。プラグイン（C++）も同じ見本を
// 読む（tests/ProtocolFixturesTests.cpp）ので、片側だけ変えるとどちらかが落ちる。
func TestConstantsMatchFixtures(t *testing.T) {
	data, err := os.ReadFile("../../../protocol/fixtures/constants.json")
	if err != nil {
		t.Fatal(err)
	}
	var c struct {
		Protocol        int      `json:"protocol"`
		AppDirName      string   `json:"app_dir_name"`
		SpoolDirName    string   `json:"spool_dir_name"`
		LockFile        string   `json:"lock_file"`
		RequestSuffix   string   `json:"request_suffix"`
		ResponseSuffix  string   `json:"response_suffix"`
		WaitSuffix      string   `json:"wait_suffix"`
		TempSuffix      string   `json:"temp_suffix"`
		MaxRequestBytes int      `json:"max_request_bytes"`
		MaxNestingDepth int      `json:"max_nesting_depth"`
		ValidIDs        []string `json:"valid_ids"`
		InvalidIDs      []string `json:"invalid_ids"`
		ReservedTools   []string `json:"reserved_tools"`
		Codes           []string `json:"codes"`
	}
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatal(err)
	}
	for _, check := range []struct {
		name      string
		got, want any
	}{
		{"protocol", ProtocolVersion, c.Protocol},
		{"app_dir_name", AppDirName, c.AppDirName},
		{"spool_dir_name", SpoolDirName, c.SpoolDirName},
		{"lock_file", LockFile, c.LockFile},
		{"request_suffix", RequestSuffix, c.RequestSuffix},
		{"response_suffix", ResponseSuffix, c.ResponseSuffix},
		{"wait_suffix", WaitSuffix, c.WaitSuffix},
		{"temp_suffix", TempSuffix, c.TempSuffix},
		{"max_request_bytes", MaxRequestBytes, c.MaxRequestBytes},
		{"max_nesting_depth", MaxNestingDepth, c.MaxNestingDepth},
	} {
		if check.got != check.want {
			t.Errorf("%s: %v, fixtures say %v", check.name, check.got, check.want)
		}
	}
	for _, id := range c.ValidIDs {
		if !ValidID(id) {
			t.Errorf("ValidID(%q) = false", id)
		}
	}
	for _, id := range c.InvalidIDs {
		if ValidID(id) {
			t.Errorf("ValidID(%q) = true", id)
		}
	}
	for _, tool := range []string{ToolSessionStart, ToolSessionEnd} {
		if !slices.Contains(c.ReservedTools, tool) {
			t.Errorf("%s is not a reserved tool in the fixtures", tool)
		}
	}
	codes := []string{CodeInvalidRequest, CodeUnknownTool, CodeInvalidArgs, CodeNoWait, CodeBusy, CodeNoSession, CodeInternal}
	if !slices.Equal(codes, c.Codes) {
		t.Errorf("codes %v, fixtures say %v", codes, c.Codes)
	}
}

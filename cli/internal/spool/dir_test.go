package spool

import "testing"

// 呼ぶ側にもスプールの場所を変える環境変数は無い。プラグインは環境変数を読まないので、
// VW2026_SPOOL で別の場所を指せても要求が届かないため（docs/protocol.md「スプールの場所」）。
func TestDefaultDirIgnoresSpoolVariable(t *testing.T) {
	want, err := DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("VW2026_SPOOL", t.TempDir())
	if got, err := DefaultDir(); err != nil || got != want {
		t.Fatalf("DefaultDir() = %s, %v; want %s", got, err, want)
	}
}

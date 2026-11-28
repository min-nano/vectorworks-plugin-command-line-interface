package edition

import "strings"

// Expand は s の ${cmd} などを Vars の値に置換する。kong が挿入しない文字列（コマンドの Help() の
// 詳細な説明・エラーの文言）に使用する。未知の ${…} はそのまま残す。
func Expand(s string) string {
	if !strings.Contains(s, "${") {
		return s
	}
	vars := Vars()
	pairs := make([]string, 0, 2*len(vars))
	for k, v := range vars {
		pairs = append(pairs, "${"+k+"}", v)
	}
	return strings.NewReplacer(pairs...).Replace(s)
}

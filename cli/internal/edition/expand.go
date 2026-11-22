package edition

import "strings"

// Expand は s の ${cmd} などを Vars の値に置き換える。kong が差し込まない文字列（コマンドの Help() の
// 詳しい説明・誤りの文言）に使う。知らない ${…} はそのまま残す。
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

package install

import (
	"errors"
	"fmt"
)

// ErrRunning は、ロックを掴む Vectorworks が居るので何もしなかった（専用の終了コード）。
var ErrRunning = errors.New("Vectorworks is running: quit it, then run again")

// RunsInsideError は、uninstall が外すフォルダの中で、走らせている CLI が動いている（入れた cli の vw2026 で
// uninstall cli を走らせた）ので、何もしなかった。フォルダの外の CLI（初回のスクリプト）で外す。呼ぶ側は
// errors.As で見分ける。Tag は入っているタグ（外すのに使う初回のスクリプトと CLI の版）。
type RunsInsideError struct {
	Dir string
	Tag string
}

func (e *RunsInsideError) Error() string {
	return fmt.Sprintf("vw2026 runs from the folder it would uninstall, %s; nothing is removed", e.Dir)
}

// usageError は使い方の誤り（名前・タグ・指定の形）。外からは Usagef で作り、IsUsage で見分ける。
type usageError struct {
	msg string
}

func (e *usageError) Error() string {
	return e.msg
}

// Usagef は使い方の誤り（IsUsage が真）を作る。コマンドの側で見つけた誤りも、install・uninstall の中で
// 見つけたものと同じ出力（failed の JSON）と終了コードにするため、パッケージの外にも出す。
func Usagef(format string, args ...any) error {
	return &usageError{fmt.Sprintf(format, args...)}
}

// IsUsage は使い方の誤りか。
func IsUsage(err error) bool {
	var usage *usageError
	return errors.As(err, &usage)
}

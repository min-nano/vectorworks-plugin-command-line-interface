package install

import (
	"errors"
	"fmt"
)

// ErrRunning は、ロックを掴む Vectorworks が居るので何もしなかった（専用の終了コード）。
var ErrRunning = errors.New("Vectorworks is running: quit it, then run again")

// ErrRunsInside は、uninstall が外すフォルダの中で、走らせている CLI が動いている（入れた cli の vw2026 で
// uninstall cli を走らせた）ので、何もしなかった。フォルダの外の CLI（初回のスクリプト）で外す。
// 返る誤りは *RunsInsideError で、errors.Is で ErrRunsInside と比べられる。
var ErrRunsInside = errors.New("vw2026 runs from the folder it would uninstall")

// RunsInsideError は ErrRunsInside の中身。Tag は入っているタグ（外すのに使う初回のスクリプトと CLI の版）。
type RunsInsideError struct {
	Dir string
	Tag string
}

func (e *RunsInsideError) Error() string {
	return fmt.Sprintf("%v, %s; nothing is removed", ErrRunsInside, e.Dir)
}

// Is は ErrRunsInside と比べさせる。
func (e *RunsInsideError) Is(target error) bool {
	return target == ErrRunsInside
}

// UsageError は使い方の誤り（名前・タグ・指定の形）。
type UsageError struct {
	msg string
}

func (e *UsageError) Error() string {
	return e.msg
}

// Usagef は使い方の誤り（IsUsage が真）を作る。コマンドの側で見つけた誤りも、install・uninstall の中で
// 見つけたものと同じ出力（failed の JSON）と終了コードにするため、パッケージの外にも出す。
func Usagef(format string, args ...any) error {
	return &UsageError{fmt.Sprintf(format, args...)}
}

// IsUsage は使い方の誤りか。
func IsUsage(err error) bool {
	var usage *UsageError
	return errors.As(err, &usage)
}

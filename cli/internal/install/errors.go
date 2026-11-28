package install

import (
	"errors"
	"fmt"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/edition"
)

// ErrRunning は、ロックを取得する Vectorworks が存在するため何もしなかった（専用の終了コード）。
var ErrRunning = errors.New("Vectorworks is running: quit it, then run again")

// RunsInsideError は、uninstall がアンインストールするフォルダの中で、実行している CLI が
// 動作している（インストールした cli の vw2026 で uninstall cli を実行した）ため、何もしなかった。
// フォルダの外の CLI（初回スクリプト）でアンインストールする。呼び出し元は errors.As で区別する。
// Tag はインストールされているタグ（アンインストールに使用する初回スクリプトと CLI の版）。
type RunsInsideError struct {
	Dir string
	Tag string
}

func (e *RunsInsideError) Error() string {
	return fmt.Sprintf(edition.Command+" runs from the folder it would uninstall, %s; nothing is removed", e.Dir)
}

// usageError は使用方法の誤り（名前・タグ・指定の形式）。外部からは Usagef で生成し、
// IsUsage で区別する。
type usageError struct {
	msg string
}

func (e *usageError) Error() string {
	return e.msg
}

// Usagef は使用方法の誤り（IsUsage が真）を生成する。コマンドの側で検出した誤りも、
// install・uninstall の中で検出したものと同じ出力（failed の JSON）と終了コードとするため、
// パッケージの外にも公開する。
func Usagef(format string, args ...any) error {
	return &usageError{fmt.Sprintf(format, args...)}
}

// IsUsage は使用方法の誤りか。
func IsUsage(err error) bool {
	var usage *usageError
	return errors.As(err, &usage)
}

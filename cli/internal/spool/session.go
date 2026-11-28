package spool

import (
	"errors"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/edition"
)

// 予約ツール（docs/protocol.md「予約ツール」）。占有のセッション ID はプラグインが発行してメモリに
// 保持するので、CLI の側に保持するものは無い。予約ツールの名前と、拒否された場合のエラーのみを
// 配置する。
const (
	// ツールの一覧を返す
	ToolTools = "@tools"
	// プラグインの版などを返す。種類 read なので占有と照合しない
	ToolPing = "ping"
	// 占有のセッション ID を発行する
	ToolSessionStart = "@session_start"
	// 占有を終了する
	ToolSessionEnd = "@session_end"
)

// ErrOccupied は、他の呼び出し元がブリッジを占有している（要求は実行されていない）。
var ErrOccupied = errors.New("the bridge is occupied by another session")

// ErrNoSession は、要求に含めたセッション ID が現在の占有のものではない（占有を
// 終了した・Vectorworks を再起動した）か、占有の中でのみ呼び出せるツール（@session_end・種類
// write と app）を占有せずに呼び出した。要求は実行されていない。文言は両方を示す（セッション ID を
// 渡していない呼び出し元に、セッション ID の誤りと読み取らせないように）。
var ErrNoSession = errors.New("not in the current session (the session has ended, or the tool runs only in a session: see \"" + edition.Command + " help session\")")

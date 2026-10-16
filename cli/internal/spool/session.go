package spool

import "errors"

// 予約された道具（docs/protocol.md「予約された道具」）。占有の印はプラグインが発行してメモリに
// 持つので、CLI の側に持つものは無い。予約された道具の名前と、断られたときの誤りだけを置く。
const (
	// 道具の一覧を返す
	ToolTools = "tools"
	// プラグインの版などを返す。種類 read なので占有に照らさない
	ToolPing = "ping"
	// 占有の印を発行する
	ToolSessionStart = "session_start"
	// 占有を終える
	ToolSessionEnd = "session_end"
)

// ErrOccupied は、ほかの呼ぶ側がブリッジを占有している（要求は実行されていない）。
var ErrOccupied = errors.New("the bridge is occupied by another session")

// ErrNoSession は、要求に載せた印がいまの占有のものではない（占有を終えた・Vectorworks を起動
// し直した）か、占有の中でしか呼べない道具（session_end・種類 write と app）を占有せずに呼んだ。
// 要求は実行されていない。
// 文言は両方を言う（印を渡していない呼ぶ側に、印の誤りと読ませないように）。
var ErrNoSession = errors.New("not in the current session (the session has ended, or the tool runs only in a session: see \"vw2026 help session\")")

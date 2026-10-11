package spool

import "errors"

// 占有（docs/protocol.md「占有」）。占有の印はプラグインが発行してメモリに持つので、CLI の側に
// 持つものは無い。予約された道具の名前と、断られたときの誤りだけを置く。
const (
	ToolSessionStart = "session_start" // 占有の印を発行する
	ToolSessionEnd   = "session_end"   // 占有を終える
)

// ErrBusy は、ほかの呼ぶ側がブリッジを占有している（要求は実行されていない）。
var ErrBusy = errors.New("the bridge is occupied by another session")

// ErrNoSession は、要求に載せた印がいまの占有のものではない（占有を終えた・Vectorworks を起動
// し直した）か、占有の中でしか呼べない道具（quit）を占有せずに呼んだ。要求は実行されていない。
var ErrNoSession = errors.New("no such session")

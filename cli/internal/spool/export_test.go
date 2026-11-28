package spool

import (
	"testing"
	"time"
)

// WithNewID は、b の Call が作成する要求の id を id に固定する。
func WithNewID(b *Bridge, id string) *Bridge {
	b.newID = func(time.Time) string { return id }
	return b
}

// SetTakenGrace はテストの間のみ takenGrace を短縮する。
func SetTakenGrace(t *testing.T, d time.Duration) {
	old := takenGrace
	takenGrace = d
	t.Cleanup(func() { takenGrace = old })
}

// DecodeResponse・CheckResponse は、応答を読み取る処理と検証（decodeResponse・checkResponse）を、
// ファイルを使用せずに呼び出させる。
var (
	DecodeResponse = decodeResponse
	CheckResponse  = checkResponse
)

package spool

import (
	"testing"
	"time"
)

// WriteAtomically は要求を公開する手順（書きかけを書いてから rename）をテストから呼ぶ。
var WriteAtomically = writeAtomically

// SetTakenGrace はテストの間だけ takenGrace を縮める。
func SetTakenGrace(t *testing.T, d time.Duration) {
	old := takenGrace
	takenGrace = d
	t.Cleanup(func() { takenGrace = old })
}

// SetMalformedGrace はテストの間だけ malformedGrace を縮める。
func SetMalformedGrace(t *testing.T, d time.Duration) {
	old := malformedGrace
	malformedGrace = d
	t.Cleanup(func() { malformedGrace = old })
}

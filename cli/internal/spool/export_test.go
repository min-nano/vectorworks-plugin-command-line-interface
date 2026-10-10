package spool

import (
	"testing"
	"time"
)

// SetTakenGrace はテストの間だけ takenGrace を縮める。
func SetTakenGrace(t *testing.T, d time.Duration) {
	old := takenGrace
	takenGrace = d
	t.Cleanup(func() { takenGrace = old })
}

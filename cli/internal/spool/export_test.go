package spool

import (
	"testing"
	"time"
)

// SetNewID はテストの間だけ、Call が作る要求の id を id に固定する。
func SetNewID(t *testing.T, id string) {
	old := newID
	newID = func(time.Time) string { return id }
	t.Cleanup(func() { newID = old })
}

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

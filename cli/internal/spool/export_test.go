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

// SetMalformedGrace はテストの間だけ malformedGrace を縮める。
func SetMalformedGrace(t *testing.T, d time.Duration) {
	old := malformedGrace
	malformedGrace = d
	t.Cleanup(func() { malformedGrace = old })
}

// SetSessionGrace はテストの間だけ sessionGrace を縮める。
func SetSessionGrace(t *testing.T, d time.Duration) {
	old := sessionGrace
	sessionGrace = d
	t.Cleanup(func() { sessionGrace = old })
}

package spool

import (
	"testing"
	"time"
)

// PublishRequest は要求を公開する手順をテストから呼ぶ。
var PublishRequest = publishRequest

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

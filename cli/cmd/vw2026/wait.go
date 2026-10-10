package main

import (
	"fmt"
	"time"
)

type waitCmd struct {
	Down    bool    `help:"Wait until the bridge stops instead."`
	Timeout float64 `default:"120" placeholder:"SECONDS" help:"How long to wait (default ${default})."`
}

func (waitCmd) Help() string {
	return `
Wait waits until the bridge starts, that is, until the plug-in takes the lock.
With --down it waits until the bridge stops (the lock is released). It prints
the same output as status, or exits with 4 when --timeout runs out.

Vectorworks keeps the lock while it shows the dialog that asks to save the
drawings, so "wait --down" does not take that dialog for a stop.
`
}

func (c *waitCmd) run(g *globals, e *env) int {
	deadline := time.Now().Add(seconds(c.Timeout))
	for {
		// 動き出す・止まるはロックだけで決まる。保存の確認のダイアログを開いている間も
		// ロックは掴まれたままなので、--down がそれを「止まった」と誤らない。
		bridge := g.open(e)
		if bridge == nil {
			return exitFailure
		}
		if bridge.Running != c.Down {
			return emit(e, bridgeJSON(bridge))
		}
		if time.Now().After(deadline) {
			fmt.Fprintln(e.stderr, "vw2026: timed out waiting")
			return exitTimeout
		}
		time.Sleep(250 * time.Millisecond)
	}
}

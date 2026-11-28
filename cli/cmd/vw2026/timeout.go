package main

import (
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/alecthomas/kong"
)

// maxTimeoutSeconds は待つ上限の最大（time.Duration に収まる秒数）。
const maxTimeoutSeconds = math.MaxInt64 / int64(time.Second)

// timeout は待つ上限（秒）。call・tools・session start・session end が共有する（指定と環境変数の値の確かめを
// 1 か所にする）。0 から maxTimeoutSeconds までの有限の数だけを受け、外れたら使い方の誤りにする（負の値・NaN・
// 無限大・大きすぎる値は time.Duration に直すと負になり、すぐに待つのをやめてしまうため）。
type timeout float64

// Decode は kong の MapperValue（指定・環境変数・既定値のどれもこれを通る）。
func (t *timeout) Decode(ctx *kong.DecodeContext) error {
	token, err := ctx.Scan.PopValue("seconds")
	if err != nil {
		return err
	}
	var value float64
	switch v := token.Value.(type) {
	case string:
		value, err = strconv.ParseFloat(v, 64)
		if err != nil {
			return fmt.Errorf("expected a number of seconds but got %q", v)
		}
	case float64:
		value = v
	default:
		return fmt.Errorf("expected a number of seconds but got %v", token.Value)
	}
	// NaN は比べると偽なので、範囲の内側を確かめる形で断る。
	if !(value >= 0 && value <= float64(maxTimeoutSeconds)) {
		return fmt.Errorf("expected seconds from 0 to %d but got %v", maxTimeoutSeconds, value)
	}
	*t = timeout(value)
	return nil
}

// duration は待つ上限を time.Duration にする。
func (t timeout) duration() time.Duration {
	return time.Duration(float64(t) * float64(time.Second))
}

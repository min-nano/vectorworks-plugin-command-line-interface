#pragma once

// OS タイマー（docs/plugin/architecture.md「受け付けの流れ」）。刻みごとに受け付けを 1 回行う。
// 終了の依頼（quit）と Quitting は段 6 で足す（docs/plugin/plan.md）。

namespace VwCli {

// startClock は OS タイマーを始める。メインスレッドから呼ぶ。始められたら 2 度目からは何もせず、
// 始められなければ次に呼ばれたときにやり直す。
void startClock();

}

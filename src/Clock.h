#pragma once

// OS タイマー（docs/plugin/architecture.md「受け付けの流れ」）。刻みごとに受け付けを 1 回行い、
// 道具 quit が終了の依頼を返せば、受け付けの外で終了を頼む。起動を終えた印を立てる通知も、最初の
// 刻みで登録する（docs/plugin/architecture.md「入口」）。

namespace VwCli {

// startClock は OS タイマーを始める。メインスレッドから呼ぶ。始められたら 2 度目からは何もせず、
// 始められなければ次に呼ばれたときにやり直す。
void startClock();

}

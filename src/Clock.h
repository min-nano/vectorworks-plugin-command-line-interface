#pragma once

// OS タイマー（docs/plugin/architecture.md「受付処理の流れ」）。周期処理ごとに受付処理を 1 回行い、
// ツール quit が終了の依頼を返せば、受付処理の外で終了を依頼する。起動の完了を示すフラグを設定する
// 通知も、最初の周期処理で登録する（docs/plugin/architecture.md「エントリポイント」）。

namespace VwCli {

// startClock は OS タイマーを開始する。メインスレッドから呼び出す。開始できたら 2 度目からは
// 何もせず、開始できなければ次に呼び出された際に再試行する。
void startClock();

}

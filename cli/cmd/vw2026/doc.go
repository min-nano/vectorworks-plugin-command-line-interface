// Vw2026 は、Vectorworks 2026 で動いているブリッジ（cli プラグイン）へ、
// 道具の呼び出しを 1 つずつ届けるコマンドである。
//
// 指示されたとおりに 1 回だけ動くプリミティブな入口で、
// セッションの見分け・占有・セッション間のロック・再試行・道具ごとの解釈は持たない
// （docs/design.md「CLI はプリミティブに保つ」）。
// それらは呼ぶ側が担う。
// 受け渡しの作法は docs/protocol.md が真実で、
// このコマンドは cli/internal/spool を通してその作法を守る。
//
// この文書が vw2026 の使い方の真実である。
// Markdown には別に書かない。
// 端末では次で読む。
//
//	cd cli && go doc ./cmd/vw2026
//
// # 使い方
//
//	vw2026 <command> [options]
//
// 出力は標準出力へ JSON 1 行、失敗の説明は標準エラーへ英語で書く。
// 成否は終了コードで返す（「終了コード」の節）。
// 指定は位置引数の前後どちらに書いてもよい（vw2026 call layers --timeout 5）。
// 各コマンドの指定の一覧は vw2026 <command> -h で出る。
//
//	vw2026 status
//	vw2026 call tools
//	vw2026 call layers '{"include_sheets":false}'
//	echo '{"layer":"1F"}' | vw2026 call layer_objects -
//	vw2026 launch && vw2026 wait
//
// 更新は、終了・待つ・入れる・起動・待つを呼ぶ側が組み合わせる（install は未実装）。
//
//	vw2026 call quit && vw2026 wait --down && vw2026 install && vw2026 launch && vw2026 wait
//
// # status
//
//	vw2026 status [--spool <dir>]
//
// ブリッジが動いているか（docs/protocol.md「生存の判定」）を返す。
// 判定はロック（bridge.lock）が掴まれているかだけで決まる。
// プラグインの版は call ping で見る。
//
//	{"running":true,"spool":"…"}
//	{"running":false,"spool":"…","reason":"…"}
//
// 動いていなければ上の 2 行目を出したうえで終了コード 3 で終わる。
//
// # call
//
//	vw2026 call <tool> [args-json | -] [--raw] [--timeout <seconds>] [--spool <dir>]
//
// 道具を 1 つ呼ぶ。
// 道具の一覧は call tools で得る（道具の仕様は docs/plugin/tools.md）。
//
//   - args-json は JSON オブジェクト。
//     "-" なら標準入力から読む。
//     省略すれば引数なし。
//   - 成功なら応答の result だけを出す。
//     --raw なら応答全体（ok / result / error）を出す。
//   - 道具が失敗を返せば理由を標準エラーへ書き、終了コード 1 で終わる。
//   - --timeout（既定 30 秒）までに応答が無ければ、置いた要求を取り下げて終了コード 4 で終わる。
//
// # wait
//
//	vw2026 wait [--down] [--timeout <seconds>] [--spool <dir>]
//
// ブリッジが動き出す（ロックが掴まれる）まで待つ。
// --down なら止まる（ロックが放される）まで待つ。
// 出力は status と同じ形。
// --timeout（既定 120 秒）を過ぎれば終了コード 4。
//
// 保存の確認のダイアログを開いている間もロックは掴まれたままなので、
// --down はそれを「止まった」と誤らない。
//
// # launch
//
//	vw2026 launch [--app <name-or-path>] [--spool <dir>]
//
// Vectorworks を起動する。
// ブリッジが動いていれば起動しない。
// 起動を待たないので、待つなら続けて wait を呼ぶ。
//
//	{"launched":true,"command":[…]}
//	{"running":true,"spool":"…","launched":false}
//
// --app（環境変数 VW2026_APP）の既定は、macOS では "Vectorworks 2026"（open -a で起動）、
// Windows では %ProgramFiles%\Vectorworks 2026\Vectorworks2026.exe。
// ほかの場所は探さないので明示する。
// .app で終わらない値は実行ファイルとしてそのまま起動する。
// Vectorworks は動いているがブリッジが動いていなければ（プラグインが入っていない）、
// 終了コード 3 で終わる。
//
// # version
//
//	vw2026 version
//
// この CLI の版と作法の版を返す。
//
//	{"version":"abc1234","protocol":3}
//
// # install と uninstall（未実装）
//
// 実装の段 5 で足す（docs/plugin/plan.md）。
// 仕様は docs/plugin/install-and-update.md。
//
//   - vw2026 install [--check] [--tag <tag>] [--plugins-dir <dir>]:
//     プラグインを入れる・更新する。
//     CLI も同じビルドを <CLI>/bin/ に置き、PATH を通す。
//     入っている版と同じなら何もしない。
//     outcome は installed・up_to_date・available・vectorworks_running。
//   - vw2026 uninstall [--plugins-dir <dir>]: プラグイン・CLI・スプール・PATH の項目を取り除く。
//     outcome は uninstalled・vectorworks_running。
//
// どちらも Vectorworks が動いていれば何もせず終了コード 7 で終わる。
//
// # 共通の指定
//
//   - --spool <dir>（環境変数 VW2026_SPOOL）: スプールの場所。
//     既定は <CLI>/spool（docs/protocol.md「スプールの場所」）。
//     別の場所はテスト用で、プラグインは読まない。
//   - --timeout <seconds>: 待つ上限。
//     call と wait だけが持つ。
//
// # 動いているが応えないとき
//
// Vectorworks は動いている（ロックが掴まれている）が、
// プラグインが受け付けを見送っている状態がある（モーダルダイアログ・undo の記録の最中など。
// docs/protocol.md「生存の判定」）。
// status はこれを見分けない。
//
//   - call は --timeout まで待ち、待ちきれなければ要求を取り下げて終了コード 4 で終わる。
//   - wait --down も同じく、上限を過ぎれば終了コード 4 で終わる。
//
// 呼ぶ側は、終了コード 3（止まっている）と 4（動いているが待ちきれなかった）で、
// 回復の仕方を分ける。
// 4 で launch やプラグインの入れ直しを試みる必要はない。
//
// # 終了コード
//
//	0  成功
//	1  道具が失敗を返した（理由は標準エラー。--raw なら標準出力にも）
//	2  使い方の誤り
//	3  ブリッジが見つからない
//	4  待ちきれなかった（call は置いた要求を取り下げた。Vectorworks は動いている）
//	5  使わない（protocol 2 までは「作法の版が違う」。番号は詰めない）
//	6  そのほか（書き込めない・起動できない・スプールの場所が決まらない等）
//	7  Vectorworks が動いているので行えない（install / uninstall。終了させてから呼び直す）
//
// # ビルドとテスト
//
//	cd cli
//	go vet ./...
//	go test ./...
//	go build -ldflags "-X main.version=$(git describe --always)" -o vw2026 ./cmd/vw2026
//
// テストは偽のプラグイン（cli/internal/fakeplugin）に対して受け渡しを確かめる。
// 偽のプラグインは、ロックを掴み、スプールに応答を書く goroutine である。
package main

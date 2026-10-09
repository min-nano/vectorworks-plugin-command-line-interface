# vw2026

ブリッジへ道具の呼び出しを 1 つずつ届けるコマンドです。**占有・ロック・再試行は持ちません**
（[設計「CLI はプリミティブに保つ」](design.md#cli-はプリミティブに保つ)）。

## コマンド

| コマンド | 内容 | 標準出力 |
| --- | --- | --- |
| `vw2026 status` | ブリッジが動いているか（[作法「生存の判定」](protocol.md#生存の判定)）と印 | `{"running":true,"spool":…,"status":{…}}`／`{"running":false,"spool":…,"reason":…}` |
| `vw2026 tools` | 呼べる道具の一覧（`call tools` と同じ） | 道具の一覧 |
| `vw2026 call <道具> [引数]` | 道具を 1 つ呼ぶ。引数は JSON オブジェクト、`-` なら標準入力から | 成功なら `result`。`--raw` なら応答全体 |
| `vw2026 wait` | ブリッジが動き出す（ロックが掴まれる）まで待つ。`--down` なら止まる（ロックが放される）まで | `status` と同じ形 |
| `vw2026 launch` | Vectorworks を起動する（動いていれば起動しない）。**待たない**（待つなら続けて `wait`） | `{"launched":…}` |
| `vw2026 version` | CLI の版と作法の版 | `{"version":…,"protocol":2}` |
| `vw2026 update`（**未実装**。段 5） | プラグインを更新する。**Vectorworks が動いていれば何もせず終了コード 7**。`--check` / `--tag` / `--plugins-dir` | `{"outcome":"updated"\|"no_new_build"\|"available"\|"vectorworks_running",…}`（[設計](plugin/install-and-update.md#更新vw2026-update)） |

```sh
vw2026 status
vw2026 call layers '{"include_sheets":false}'
echo '{"layer":"1F"}' | vw2026 call layer_objects -
vw2026 launch && vw2026 wait
vw2026 call quit && vw2026 wait --down && vw2026 update && vw2026 launch && vw2026 wait   # 更新
```

## 指定

| 指定 | 環境変数 | 既定 |
| --- | --- | --- |
| `--spool <dir>` | `VW2026_SPOOL` | `<CLI>/spool`（[作法「スプールの場所」](protocol.md#スプールの場所)）。別の場所はテスト用で、プラグインは読まない |
| `--timeout <秒>`（`call`・`wait`） | `VW2026_TIMEOUT` | `call` 30・`wait` 120 |
| `--app <名前／パス>`（`launch`） | `VW2026_APP` | macOS `Vectorworks 2026`（`open -a`）・Windows `%ProgramFiles%\Vectorworks 2026\Vectorworks2026.exe`（探索しない。ほかの場所なら明示する） |

## 動いているが応えないとき

Vectorworks は動いている（ロックが掴まれている）が、プラグインが受け付けを見送っている
状態です（モーダルダイアログ・undo の記録の最中）。CLI はこれを `status` では見分けません
（[作法「生存の判定」](protocol.md#生存の判定)）。

- `call` は要求を置いて `--timeout` まで待ちます（ダイアログが閉じれば処理される）。待ちきれ
  なければ要求を取り下げて終了コード 4 で終わります。待つ上限を延ばすかどうかは呼ぶ側が決めます。
- `wait --down` は Vectorworks が終わる（ロックが放される）まで待ちます。`quit` の保存の確認を
  開いている間に終了したと誤りません（利用者が取り消せば、`--timeout` を過ぎて終了コード 4）。
- `launch` は起動しません（動いている）。
- 呼ぶ側は、終了コード 3（止まっている）と 4（動いているが待ちきれなかった）で回復の仕方を
  分けます。4 で `launch` やプラグインの入れ直しを試みる必要はありません。

## 終了コード

| コード | 意味 |
| --- | --- |
| 0 | 成功 |
| 1 | 道具が失敗を返した（理由は標準エラー。`--raw` なら標準出力にも） |
| 2 | 使い方の誤り |
| 3 | ブリッジが見つからない |
| 4 | 待ちきれなかった（`call` は置いた要求を取り下げた。Vectorworks は動いている） |
| 5 | プラグインと CLI の作法の版が違う |
| 6 | そのほか（書き込めない・起動できない・スプールの場所が決まらない等） |
| 7 | Vectorworks が動いているので行えない（`update`。終了させてから呼び直す） |

## ビルドとテスト

```sh
cd cli
go vet ./...
go test ./...
go build -ldflags "-X main.version=$(git describe --always)" -o vw2026 ./cmd/vw2026
```

テストは偽のプラグイン（`cli/internal/fakeplugin`。ロックを掴み、スプールに応答を書く
goroutine）に対して受け渡しを確かめます。

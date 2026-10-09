# vw2026

ブリッジへ道具の呼び出しを 1 つずつ届けるコマンドです。**占有・ロック・再試行は持ちません**
（[設計「CLI はプリミティブに保つ」](design.md#cli-はプリミティブに保つ)）。

## コマンド

| コマンド | 内容 | 標準出力 |
| --- | --- | --- |
| `vw2026 status` | ブリッジが動いているか（[作法「生存の判定」](protocol.md#生存の判定)）。プラグインの版は `call ping` で見る | `{"running":true,"spool":…}`／`{"running":false,"spool":…,"reason":…}` |
| `vw2026 call <道具> [引数]` | 道具を 1 つ呼ぶ。引数は JSON オブジェクト、`-` なら標準入力から。道具の一覧は `call tools` | 成功なら `result`。`--raw` なら応答全体 |
| `vw2026 wait` | ブリッジが動き出す（ロックが掴まれる）まで待つ。`--down` なら止まる（ロックが放される）まで | `status` と同じ形 |
| `vw2026 launch` | Vectorworks を起動する（動いていれば起動しない）。**待たない**（待つなら続けて `wait`） | `{"launched":…}` |
| `vw2026 version` | CLI の版と作法の版 | `{"version":…,"protocol":3}` |
| `vw2026 install`（**未実装**。段 5） | プラグインを入れる・更新する（CLI も同じビルドを `<CLI>/bin/` に置き、PATH を通す）。入っている版と同じなら何もしない。**Vectorworks が動いていれば何もせず終了コード 7**。`--check` / `--tag` / `--plugins-dir` | `{"outcome":"installed"\|"up_to_date"\|"available"\|"vectorworks_running","path":…,…}`（[インストール](plugin/install-and-update.md#インストールと更新vw2026-install)） |
| `vw2026 uninstall`（**未実装**。段 5） | プラグイン・CLI・スプール・PATH の項目を取り除く。**Vectorworks が動いていれば何もせず終了コード 7**。`--plugins-dir` | `{"outcome":"uninstalled"\|"vectorworks_running","left":[…]}`（[アンインストール](plugin/install-and-update.md#アンインストールvw2026-uninstall)） |

```sh
vw2026 status
vw2026 call tools
vw2026 call layers '{"include_sheets":false}'
echo '{"layer":"1F"}' | vw2026 call layer_objects -
vw2026 launch && vw2026 wait
vw2026 call quit && vw2026 wait --down && vw2026 install && vw2026 launch && vw2026 wait   # 更新
```

## 指定

| 指定 | 環境変数 | 既定 |
| --- | --- | --- |
| `--spool <dir>` | `VW2026_SPOOL` | `<CLI>/spool`（[作法「スプールの場所」](protocol.md#スプールの場所)）。別の場所はテスト用で、プラグインは読まない |
| `--timeout <秒>`（`call`・`wait`） | なし | `call` 30・`wait` 120 |
| `--app <名前／パス>`（`launch`） | `VW2026_APP` | macOS `Vectorworks 2026`（`open -a`）・Windows `%ProgramFiles%\Vectorworks 2026\Vectorworks2026.exe`（探索しない。ほかの場所なら明示する） |

## 動いているが応えないとき

Vectorworks は動いている（ロックが掴まれている）が、プラグインが受け付けを見送っている
状態です（[作法「生存の判定」](protocol.md#生存の判定)）。`status` はこれを見分けません。

- `call` は `--timeout` まで待ち、待ちきれなければ要求を取り下げて終了コード 4 で終わります。
- `wait --down` も同じく、上限を過ぎれば終了コード 4 です。
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
| 5 | 使わない（protocol 2 までは「作法の版が違う」。番号は詰めない。[作法「版」](protocol.md#版)） |
| 6 | そのほか（書き込めない・起動できない・スプールの場所が決まらない等） |
| 7 | Vectorworks が動いているので行えない（`install` / `uninstall`。終了させてから呼び直す） |

## ビルドとテスト

```sh
cd cli
go vet ./...
go test ./...
go build -ldflags "-X main.version=$(git describe --always)" -o vw2026 ./cmd/vw2026
```

テストは偽のプラグイン（`cli/internal/fakeplugin`。ロックを掴み、スプールに応答を書く
goroutine）に対して受け渡しを確かめます。

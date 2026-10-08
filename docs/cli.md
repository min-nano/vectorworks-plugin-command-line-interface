# vwcli

ブリッジへ道具の呼び出しを 1 つずつ届けるコマンドです。**占有・ロック・再試行は持ちません**
（[設計「CLI はプリミティブに保つ」](design.md#cli-はプリミティブに保つ)）。

## コマンド

| コマンド | 内容 | 標準出力 |
| --- | --- | --- |
| `vwcli status` | ブリッジが動いているか | `{"live":true,"spool":…,"status":{…}}`／`{"live":false,"searched":[{"dir":…,"reason":…}]}` |
| `vwcli tools` | 呼べる道具の一覧（`call tools` と同じ） | 道具の一覧 |
| `vwcli call <道具> [引数]` | 道具を 1 つ呼ぶ。引数は JSON オブジェクト、`-` なら標準入力から | 成功なら `result`。`--raw` なら応答全体 |
| `vwcli wait` | ブリッジが動き出すまで待つ。`--down` なら止まるまで | `status` と同じ形 |
| `vwcli launch` | Vectorworks を起動する（既に動いていれば何もしない）。`--timeout` を付けると動き出すまで待つ | `{"launched":…}` |
| `vwcli version` | CLI の版と作法の版 | `{"version":…,"protocol":1}` |

```sh
vwcli status
vwcli call layers '{"include_sheets":false}'
echo '{"layer":"1F"}' | vwcli call layer_objects -
vwcli launch --timeout 120
```

## 指定

| 指定 | 環境変数 | 既定 |
| --- | --- | --- |
| `--plugin <名前>` | `VWCLI_PLUGIN` | `min-nano_cli` |
| `--spool <dir>` | `VWCLI_SPOOL` | 探索する（[作法「スプールの場所」](protocol.md#スプールの場所)） |
| `--timeout <秒>` | `VWCLI_TIMEOUT` | `call` 30・`wait` 120・`launch` 0（待たない） |
| `--app <名前／パス>`（`launch`） | `VWCLI_APP` | macOS `Vectorworks 2026`（`open -a`）・Windows `%ProgramFiles%\Vectorworks 2026\Vectorworks2026.exe` |

`call` は `--timeout` を過ぎても、プラグインがその要求を処理中（生存の印の `busy_id`）で
`busy_until` が未来なら待ち続けます。

## 終了コード

| コード | 意味 |
| --- | --- |
| 0 | 成功 |
| 1 | 道具が失敗を返した（理由は標準エラー。`--raw` なら標準出力にも） |
| 2 | 使い方の誤り |
| 3 | ブリッジが見つからない |
| 4 | 応答を待ちきれなかった（置いた要求は取り下げた） |
| 5 | プラグインと CLI の作法の版が違う |
| 6 | そのほか（書き込めない・起動できない等） |

## ビルドとテスト

```sh
cd cli
go vet ./...
go test ./...
go build -ldflags "-X main.version=$(git describe --always)" -o vwcli ./cmd/vwcli
```

テストは偽のプラグイン（スプールに応答を書く goroutine）に対して受け渡しを確かめます。

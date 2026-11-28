# 通信規則のフィクスチャ

[通信規則](protocol.md)を、プラグイン側（C++）と CLI 側（Go）が同一に実装しているかを
検証するためのフィクスチャです（`protocol/fixtures/`）。**両側のテストが同じフィクスチャを読み込み、通信規則の定数を
テストの中に二重に記述しません。** 通信規則を変更したら、文書とフィクスチャと両側の実装を修正します。

| 読み込む側 | テスト |
| --- | --- |
| プラグイン（`src/core`） | `tests/FixtureTests.cpp`（定数・id・受付処理）。ファジング（`tests/JsonFuzzer.cpp`）は `requests/` をシードにする |
| CLI | `cli/internal/spool/fixtures_test.go`（定数・id）・`cli/internal/fakeplugin/fixtures_test.go`（受付処理）。ファジング（`cli/internal/spool/fuzz_test.go`）は `requests/` をシードにする |

## 配置場所

| ファイル | 内容 |
| --- | --- |
| `constants.json` | 通信規則の定数（版・スプールの名前・ファイルの綴り・1 件の上限・入れ子の深さの上限・予約ツール・種類・`code` の綴り）。両側のコードの定数と照合する |
| `ids.json` | id の綴りのフィクスチャ（`valid` / `invalid`）。両側の id の検査と照合する。`valid` には Windows の予約されたデバイス名（`NUL` など）を含む |
| `requests/` | 要求のフィクスチャ。ファイルの内容がそのまま要求のバイト列（破損した要求・BOM・CRLF・不正な UTF-8 を含む） |
| `fake-tools.json` | 受付処理のフィクスチャが使用する擬似ツールの一覧 |
| `serve/*.json` | 受付処理のフィクスチャ（1 ファイル 1 件） |

ツールの結果の規則（実数の綴り）は通信規則ではないので、ここではなく `tests/fixtures/` に配置します
（C++ は `tests/NumberTests.cpp`、Go は `cli/internal/toolfixtures`。[ツール「共通の規則」](plugin/tools.md#共通の規則)）。

フィクスチャの内容は整形も検査もせず（[整形](format.md#整形の対象外)）、git にも改行を変換させません
（`.gitattributes` の `-text`）。

**両側のテストは、フィクスチャの未知のキーと型の異なる値を誤りとして扱います**（Go は
`cli/internal/fixturetest`）。キーの綴りを誤ると、その検証が暗黙のうちに省略されるためです。手順（`steps`）の
無いフィクスチャも誤りです。

## 擬似ツールの一覧（`fake-tools.json`）

`tools` の各要素は、ツールの一覧の 1 行（`name`・`description`・`inputSchema`・`kind`）と、振る舞い
`does` を有します。省略可能なキーとして、ツールが返す詳細 `detail`（オブジェクト。失敗なら応答の
`detail` に含まれ、成功なら含まれない）と、終了の依頼を設定するか否か `quit`（真偽。省略すると偽。応答が成功の
場合のみ終了を要求する。種類では決定しない）を有します。C++ のテストが扱える擬似ツールの数には上限があります
（`tests/FixtureTests.cpp` の `kRunners`）。

| `does` | 振る舞い |
| --- | --- |
| `echo` | 引数をそのまま結果にする |
| `empty` | 結果を `{}` にする |
| `invalid_args` | `invalid_args` で失敗する |
| `internal` | `internal` で失敗する |
| `throw` | 例外を送出する（応答は `internal`。Go の擬似プラグインは例外を送出せず、同じ応答を返す） |
| `null` | 値を返さない（応答の `result` は `{}`） |
| `array` | 結果を配列にする（オブジェクトでないので、応答は `internal`） |

未知の `does` は、そのツールが呼び出された時点でテストの誤りになります（両側とも）。

## 受付処理のフィクスチャ（`serve/*.json`）

1 件は、同一のプラグイン（ロックとセッション ID を保持し続ける）に対する受付処理の回（`steps`）の列です。
回ごとに、スプールへファイルを配置し、受付処理を 1 回実行し、残存したファイルと応答を検証します。

```json
{
  "description": "…",
  "steps": [
    {
      "other_bridge": false,
      "waits": {"dead": "released"},
      "files": {"notes.txt": ""},
      "requests": {"0001": "ping.json", "big": {"fill": {"head": "…", "pad": "x", "tail": "…", "size": 1048577}}},
      "expect": {
        "phase": "serving",
        "quit": false,
        "calls": [{"tool": "ping", "args": {}}],
        "responses": {"0001": {"ok": true, "result": {}}, "big": {"ok": false, "code": "invalid_request"}},
        "files": ["0001.res.json", "0001.wait", "big.res.json", "big.wait", "bridge.lock", "notes.txt"]
      }
    }
  ]
}
```

配置するもの（いずれも省略可能）:

| キー | 意味 |
| --- | --- |
| `other_bridge` | 真なら、その回の間のみ他の Vectorworks として `bridge.lock` を取得する |
| `activity` | その回にタイマーが渡す処理中の状態。`quitting`（終了の依頼の処理中）。省略すると終了の依頼の処理中でない（[通信規則「Vectorworks が処理中」](protocol.md#vectorworks-が処理中)） |
| `waits` | id ごとの待機標識の状態。`held`（呼び出し元が取得する）・`released`（内容が空のファイルのみ配置する）・`missing`（配置しない） |
| `files` | 名前と内容（文字列）。通信規則の綴りでないファイルや、前の回の残骸を配置する |
| `requests` | id ごとの要求（`<id>.req.json`）。文字列は `requests/` のフィクスチャの名前で、`{"fill": …}` は `head`＋`pad` の 1 文字の繰り返し＋`tail` を `size` バイトにしたもの。`waits` に無い id の待機標識は `held` |

検証すること（`files` 以外は省略すると既定）:

| キー | 既定 | 意味 |
| --- | --- | --- |
| `phase` | `serving` | 受付処理の状態（`serving` / `waiting` / `error`） |
| `quit` | 偽 | 終了の依頼 |
| `calls` | 無い | 実行されたツールの順序。`args` があれば引数も比較する |
| `responses` | 無い | id ごとの応答。`ok` と、失敗なら `code` を比較する（`error` は人間向けなので比較しない）。`result` があれば結果も比較する。`detail` は、記述すれば応答がそれと同一の `detail` を有し、記述しなければ応答が `detail` を有しないことを検証する。この回より前に `files` で配置したものを除き、スプールに存在する応答は全てここに記載されていなければならない |
| `files` | 比較しない | 回の後のスプールのファイル名の一覧（順序は問わない） |

- 値は JSON として比較します（オブジェクトのキーの順序は問わず、数は綴りで比較する）。
- **`$session` は発行されたセッション ID です。** 要求のフィクスチャの中の `$session` は、その時点までに発行された
  セッション ID に置換します。期待する結果の中の文字列 `"$session"` は、id の綴りを満たす文字列に一致し、その値を
  発行されたセッション ID として記憶します。
- 回の後は、`responses` に記載された id について呼び出し元が応答を受け取ったことを模倣します（応答を削除し、
  待機標識を解放して削除する）。処理されずに残存した要求とその待機標識は、次の回へ持ち越します。
- 回の途中で要求を配置すること（回の始めに列挙した要求のみを処理する）や、削除できない要求のようにフィクスチャで
  表現しにくいことは、両側のテストがそれぞれ検証します。受付処理の入れ子（ツールの中から受付処理が
  再び呼び出される）は、C++ のテスト（`tests/ServeTests.cpp`）のみが検証します（Go の擬似ツールは
  イベントを処理しないので、擬似プラグインは入れ子を模倣しない）。

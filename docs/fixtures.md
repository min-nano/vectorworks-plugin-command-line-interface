# 通信規則の見本

[通信規則](protocol.md)を、プラグイン側（C++）と CLI 側（Go）が同じに実装しているかを
確かめるための見本です（`protocol/fixtures/`）。**両側のテストが同じ見本を読み、通信規則の定数を
テストの中に二重に書きません。** 通信規則を変えたら、文書と見本と両側の実装を直します。

| 読む側 | テスト |
| --- | --- |
| プラグイン（`src/core`） | `tests/FixtureTests.cpp`（定数・id・受け付け）。ファジング（`tests/JsonFuzzer.cpp`）は `requests/` を種にする |
| CLI | `cli/internal/spool/fixtures_test.go`（定数・id）・`cli/internal/fakeplugin/fixtures_test.go`（受け付け） |

## 置き場所

| ファイル | 中身 |
| --- | --- |
| `constants.json` | 通信規則の定数（版・スプールの名前・ファイルの綴り・1 件の上限・入れ子の深さの上限・予約された道具・種類・`code` の綴り）。両側のコードの定数と照らす |
| `ids.json` | id の綴りの見本（`valid` / `invalid`）。両側の id の検査と照らす。`valid` には Windows の予約されたデバイス名（`NUL` など）を含む |
| `requests/` | 要求の見本。ファイルの中身がそのまま要求のバイト列（壊れた要求・BOM・CRLF・不正な UTF-8 を含む） |
| `fake-tools.json` | 受け付けの見本が使う偽の道具の表 |
| `serve/*.json` | 受け付けの見本（1 ファイル 1 件） |

見本の中身は整形も検査もせず（[整形](format.md#整形しないもの)）、git にも改行を変えさせません
（`.gitattributes` の `-text`）。

**両側のテストは、見本の知らないキーと型の違う値を誤りとして読みます**（Go は
`DisallowUnknownFields`、C++ はキーと型の表）。キーの綴りを誤ると、その確かめが黙って省かれる
ためです。手順（`steps`）の無い見本も誤りです。

## 偽の道具の表（`fake-tools.json`）

`tools` の各要素は、道具の表の 1 行（`name`・`description`・`inputSchema`・`kind`）と、振る舞い
`does` を持ちます。

| `does` | 振る舞い |
| --- | --- |
| `echo` | 引数をそのまま結果にする |
| `empty` | 結果を `{}` にする |
| `invalid_args` | `invalid_args` で失敗する |
| `internal` | `internal` で失敗する |
| `throw` | 例外を投げる（応答は `internal`。Go の偽のプラグインは例外を投げず、同じ応答を返す） |

知らない `does` は、その道具が呼ばれたときにテストの誤りになります（両側とも）。

## 受け付けの見本（`serve/*.json`）

1 件は、同じプラグイン（ロックと占有の印を持ち続ける）に対する受け付けの回（`steps`）の並びです。
回ごとに、スプールへファイルを置き、受け付けを 1 回走らせ、残ったファイルと応答を確かめます。

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

置くもの（どれも省略できる）:

| キー | 意味 |
| --- | --- |
| `other_bridge` | 真なら、その回の間だけほかの Vectorworks として `bridge.lock` を掴む |
| `waits` | id ごとの待つ印の状態。`held`（呼ぶ側が掴む）・`released`（中身の空のファイルだけ置く）・`missing`（置かない） |
| `files` | 名前と中身（文字列）。通信規則の綴りでないファイルや、前の回の残骸を置く |
| `requests` | id ごとの要求（`<id>.req.json`）。文字列は `requests/` の見本の名前で、`{"fill": …}` は `head`＋`pad` の 1 文字の繰り返し＋`tail` を `size` バイトにしたもの。`waits` に無い id の待つ印は `held` |

確かめること（`files` 以外は省略すると既定）:

| キー | 既定 | 意味 |
| --- | --- | --- |
| `phase` | `serving` | 受け付けの状態（`serving` / `waiting` / `error`） |
| `quit` | 偽 | 終了の依頼 |
| `calls` | 無い | 実行された道具の順。`args` があれば引数も比べる |
| `responses` | 無い | id ごとの応答。`ok` と、失敗なら `code` を比べる（`error` は人向けなので比べない）。`result` があれば結果も比べる。この回より前に `files` で置いたものを除き、スプールにある応答はすべてここに載っていなければならない |
| `files` | 比べない | 回のあとのスプールのファイル名の一覧（並びは問わない） |

- 値は JSON として比べます（オブジェクトのキーの並びは問わず、数は綴りで比べる）。
- **`$session` は発行された占有の印です。** 要求の見本の中の `$session` は、そのときまでに発行された
  印に置き換えます。期待する結果の中の文字列 `"$session"` は、id の綴りを満たす文字列に合い、その値を
  発行された印として覚えます。
- 回のあとは、`responses` に載った id について呼ぶ側が応答を受け取ったことを真似ます（応答を消し、
  待つ印を放して消す）。処理されずに残った要求とその待つ印は、次の回へ持ち越します。
- 回の途中で要求を置くこと（回の始めに並べた要求だけを処理する）や、消せない要求のように見本で
  表しにくいことは、両側のテストがそれぞれ確かめます。

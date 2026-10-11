# 作法の見本

[受け渡しの作法](../../docs/protocol.md)の定数と、受け付けの振る舞いの見本です。プラグイン（C++）と
CLI（Go）の両方のテストがこれを読むので、片側だけ作法から外れるとどちらかのテストが落ちます。
**作法を変えたら、文書・両側の実装と一緒にここも直します。**

| 読むテスト | 照らすもの |
| --- | --- |
| `tests/ProtocolFixturesTests.cpp` | `src/core/Bridge.h` の定数と、`core::Server`（受け付け 1 回） |
| `cli/internal/spool/fixtures_test.go` | `cli/internal/spool` の定数 |
| `cli/internal/fakeplugin/fixtures_test.go` | 偽のプラグイン（`vw2026` の単体テストの相手） |

`tests/fuzz/make-seeds.py` も、見本の要求をファジングの種に使います。

## `constants.json`

作法の定数です。各言語の実装は自分の定数を持ち（C++ は `src/core/Bridge.h`、Go は
`cli/internal/spool`）、テストがこのファイルと照らします。

| キー | 中身 |
| --- | --- |
| `protocol` | 作法の版 |
| `app_dir_name`・`spool_dir_name` | `<CLI>` のディレクトリ名と、その下のスプールの名前 |
| `lock_file`・`request_suffix`・`response_suffix`・`wait_suffix`・`temp_suffix` | ファイルの綴り |
| `max_request_bytes`・`max_nesting_depth`・`max_id_length` | 上限 |
| `valid_ids`・`invalid_ids` | id の綴りとして正しいもの・誤りのもの |
| `reserved_tools` | 予約された道具 |
| `codes`・`kinds` | 応答の `code` と、道具の `kind` の綴り（この順） |

## `serve/*.json`

1 ファイルが見本 1 つです。空のスプールから始め、`rounds` の回ごとにファイルを置いて受け付けを
1 回呼び、残ったファイルと応答を比べます。占有の印は回をまたいで持ちます（同じ橋）。

```json
{
  "description": "何を確かめるか",
  "rounds": [
    {
      "lock_held_elsewhere": false,
      "files": [{"name": "old.res.json", "text": "{}"}],
      "waits": {"kept": "held"},
      "requests": [{"id": "0001", "json": {"tool": "echo"}}],
      "expect": {
        "phase": "serving",
        "quit": false,
        "files": ["0001.res.json", "0001.wait", "kept.wait"],
        "responses": {"0001": {"ok": true, "result": {"args": {}}}}
      }
    }
  ]
}
```

| キー | 中身 |
| --- | --- |
| `lock_held_elsewhere` | 真なら、受け付けの間ほかが `bridge.lock` を掴んでいる（別の Vectorworks） |
| `files` | そのまま置くファイル（名前と中身） |
| `waits` | 要求なしに置く待つ印（`held`: 掴み続ける、`released`: 置くだけ） |
| `requests` | 要求。`wait` は `held`（既定。掴み続ける）・`released`（置くだけ）・`missing`（置かない）。テキストは次のどれか 1 つで作る: `json`（値をそのまま。文字列の `"$session"` は印に置き換える）・`text`（文字列のまま）・`base64`（UTF-8 でないテキスト）・`padded`（その大きさちょうどの `ping` の要求）・`nested`（その深さちょうどの `ping` の要求） |
| `expect.phase` | `serving`（ロックを掴んで受け付けた）・`waiting`（ロックを取れなかった） |
| `expect.quit` | 終了の依頼を返したか |
| `expect.files` | 受け付けのあとにスプールにある名前（`bridge.lock` を除く） |
| `expect.responses` | 応答。`ok`・`code`・`result` を比べ、`error` は失敗のとき空でないことだけを確かめる（人向けの文言なので）。`"$any"` は何にでも合い、`"$session"` は印（id の綴り）として受け取る |

比べたあと、`responses` に挙げた id は呼ぶ側の真似として応答を読んで消し、待つ印を放して消します。
挙げていない id の待つ印は掴んだままです（呼ぶ側がまだ待っている）。

### 偽の道具

見本は次の道具を前提にします（`tests/FakeTools.h` と `fixtures_test.go` の `fixtureTools`）。

| 道具 | 種類 | 振る舞い |
| --- | --- | --- |
| `ping` | read | `{"pong":true}`（引数は見ない） |
| `echo` | read | `{"args":<受け取った引数>}` |
| `fail` | read | `invalid_args` |
| `boom` | read | 例外（`internal`） |
| `quit` | app | `{"quitting":true}` |

### 気を付けること

- **最初の回はロックを取り、残骸を掃除してから要求を取り出します。** 待つ印を掴める・無い id の
  ファイルは、その掃除で消えます。待つ印の無い要求（`no_wait`）や、綴りを満たさない id の要求を
  確かめるには、ロックを取るだけの回を先に置きます（`wait.json`・`bad_ids.json`）。
- `tools` の結果は道具の表しだいなので見本に置きません（`tests/CoreServeTests.cpp` で確かめる）。
- 回の途中に要求を置くこと（回の始めに並べたものだけを処理する）は見本で表せないので、両側の単体
  テストで確かめます。

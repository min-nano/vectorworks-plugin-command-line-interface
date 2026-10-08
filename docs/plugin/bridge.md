# 本体の受け付け（ブリッジ）

受け渡しの形は[受け渡しの作法](../protocol.md)が真実で、ここは本体側の作りです。元の
`元:src/core/Bridge.{h,cpp}`・`元:src/core/Json.{h,cpp}`・`元:src/draw/McpBridge.{h,cpp}` を
移し、名前と生存の印を作法に合わせます。

## 分け方

| ファイル | SDK | 中身 |
| --- | --- | --- |
| `src/core/Json.{h,cpp}` | 依らない | 小さな JSON（元のまま） |
| `src/core/Bridge.{h,cpp}` | 依らない | スプール 1 つ（`prepare` / `sweep` / `statusIsLive` / `poll` / `reply` / `writeStatus` / `removeStatus`）・id の検査・要求と応答の形 |
| `src/core/Serve.{h,cpp}` | 依らない | **受け付け 1 回の手順**（下記）。道具の表と時刻を引数で受けるので無 SDK でテストできる |
| `src/tools/ToolTable.cpp` | 依る | 道具の表 |
| `src/tools/<道具>.cpp` | 依る | 道具の中身 |
| `src/payload/PayloadMain.cpp` | 依る | `vw_payload_serve` → `core::serve(…, tools::table())` |

元では受け付けの手順（`serveMcpBridge`）が SDK 依存の `draw/McpBridge.cpp` に入っていました。
ここでは**手順を `core/Serve` へ出し、偽の道具の表でテストします**（殻に頼む道具の
受け渡し・壊れた要求・`busy` の書き方・印の間隔）。

## スプールの用意

- 場所は `VW2026_SPOOL` があればそれ、無ければ `<一時ディレクトリ>/vectorworks2026-cli-bridge`
  （開発版は `-dev`）。一時ディレクトリは `std::filesystem::temp_directory_path`。**探索しません**
  （探すのは呼ぶ側の役割）。
- `prepare`: 0700 で作り、既にあれば持ち主と権限を確かめる（POSIX）。失敗したら 10 秒後に
  やり直す（見え方に理由を出す）。
- **前の回の残骸は、有効な生存の印が無いときだけ消します**（`statusIsLive` が偽のときだけ
  `sweep`）。本体の入れ替えのたびに用意がやり直されるので、印が生きている間に消すと、
  入れ替えの直前に書いた応答や置かれたばかりの要求まで消えます。

## 受け付け 1 回（`core::serve`）

1. `shellReport` があれば、その応答を書く（`payload_version` を添える）。書けたら `reportDone`。
2. 要求を名前の昇順で最大 16 件取り出す（読んだ要求はすぐ消す）。
3. 読めなかった要求には、ファイル名の id が正しければ失敗で応える。
4. 要求ごとに:
   - `tools` → 道具の一覧を返す（表を検索しない）。
   - 種類 `shell` → action に載せて応答しない（2 件目は失敗で応える）。
   - 種類 `long` → 先に印へ `busy`（道具名）・`busy_id`（要求の id）・`busy_until`
     （今＋`timeoutSeconds`）を書いてから実行し、終わったら印をすぐ書き直す。
   - それ以外 → 実行して応答を書く。
   - **道具 1 つの例外はここで受けて失敗の応答にする**（橋を止めない）。
5. 印を 2 秒ごとに書き直す。書けなければ用意からやり直す（黙って続けない）。
6. 見え方（view）を返す。

## 生存の印

作法の[生存の印](../protocol.md#生存の印bridgejson)に、本体は次を書きます。

| フィールド | 値 |
| --- | --- |
| `plugin` | `cli` / `cli_dev`（`PLUGIN_VWR_ID`） |
| `channel` | `stable` / `dev` |
| `version` | `VW_BUILD_VERSION`（本体の短い sha） |
| `branch` | `VW_BUILD_BRANCH` |
| `protocol` | 1 |
| `beat` | 今（epoch 秒） |
| `pid` | Vectorworks のプロセス ID（`getpid` / `GetCurrentProcessId`） |
| `busy` / `busy_id` / `busy_until` | 長く走る道具の最中だけ |

- **本体の入れ替えでは印を消しません**（[ABI](abi.md#vw_payload_shutdown-で生存の印を消さない理由)）。
  Vectorworks が終了すると印は書き直されなくなり、15 秒で古びて「動いていない」と判定されます。

## 道具の表

```cpp
enum class ToolKind { Read, Write, Long, Shell };

struct Tool {
    const char* name;            // [a-z0-9_]+(\.[a-z0-9_]+)*
    const char* description;     // 日本語。呼ぶ側（人・MCP）がそのまま読む
    const char* inputSchema;     // JSON Schema（文字列）。tools の結果にそのまま載る
    ToolKind    kind;
    int         timeoutSeconds;  // 既定より長くかかりうるときだけ（0 = 既定）
    ToolFn      run;             // Json (const Json& args, std::string& error)。Shell は nullptr
};
```

- **道具を足すときに触るのは表の 1 行と中身 1 つだけ**です。CLI は道具を知らないので直しません。
- `tools` の結果は `{"protocol":1,"tools":[{"name","description","inputSchema","kind","timeoutSeconds"?}]}`。
  `kind` は `read` / `write` / `long` / `shell` を小文字で載せます（呼ぶ側が「図面を変えるか」を
  判定できるように。MCP のラッパーが確認を挟む手掛かりになる）。
- 引数の検査は道具の中で行います（JSON Schema の検証器は持たない）。知らない引数は失敗で返します。
- **種類 `write` の道具は、undo の作法を通すまで表に載せません**（[未決 2](open-questions.md)）。

## 見え方（view）

殻が読む JSON です。`phase`（`serving` / `paused` / `error`）・`spool`・
`served`・`failed`・`lastTool`・`secondsSinceRequest`・`message`・`version`・`reportDone`・
`action`。判断は持たせず、殻は `action` と `reportDone` だけを見ます。

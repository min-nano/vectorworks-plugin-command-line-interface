# 本体の受け付け（ブリッジ）

受け渡しの形は[受け渡しの作法](../protocol.md)が真実で、ここは本体側の作りです。元の
`元:src/core/Bridge.{h,cpp}`・`元:src/core/Json.{h,cpp}`・`元:src/draw/McpBridge.{h,cpp}` を
移し、名前と生存の印を作法に合わせます。

## 分け方

| ファイル | SDK | 中身 |
| --- | --- | --- |
| `src/core/Json.{h,cpp}` | 依らない | 小さな JSON（元のまま） |
| `src/core/Bridge.{h,cpp}` | 依らない | スプール 1 つ（`prepare` / `sweep` / `statusIsLive` / `claim` / `poll` / `reply` / `writeStatus` / `removeStatus`）・id の検査・要求と応答の形 |
| `src/core/Serve.{h,cpp}` | 依らない | **受け付け 1 回の手順**（下記）。道具の表と時刻を引数で受けるので無 SDK でテストできる |
| `src/tools/ToolTable.cpp` | 依る | 道具の表 |
| `src/tools/<道具>.cpp` | 依る | 道具の中身 |
| `src/payload/PayloadMain.cpp` | 依る | `vw_payload_serve` → `core::serve(…, tools::table())` |

元では受け付けの手順（`serveMcpBridge`）が SDK 依存の `draw/McpBridge.cpp` に入っていました。
ここでは**手順を `core/Serve` へ出し、偽の道具の表でテストします**（殻に頼む道具の
受け渡し・壊れた要求・`busy` の書き方・印の間隔・確保の失敗・別の `pid` の印での待機）。

## スプールの用意

- 場所は `VW2026_SPOOL` があればそれ、無ければ `<一時ディレクトリ>/vectorworks2026-cli-bridge`
  （開発版は `-dev`）。一時ディレクトリは `std::filesystem::temp_directory_path`。**探索しません**
  （探すのは呼ぶ側の役割）。
- `prepare`: 0700 で作り、既にあれば持ち主と権限を確かめる（POSIX）。失敗したら 10 秒後に
  やり直す（見え方に理由を出す）。
- **前の回の残骸は、有効な生存の印が無いときだけ消します**（`statusIsLive` が偽のときだけ
  `sweep`）。本体の入れ替えのたびに用意がやり直されるので、印が生きている間に消すと、
  入れ替えの直前に書いた応答や置かれたばかりの要求まで消えます。`sweep` は `*.req.json`・
  `*.work`・`*.res.json`・`*.tmp` を消します。

## 受け付け 1 回（`core::serve`）

1. `VwServeInput::shellReport` があれば、その応答を書く（`payload_version` を添える）。書けたら `reportDone`。
2. 印を読み、**別の `pid` の印が `down` でなければ待機**する（要求を取り出さず、印も書かず、
   見え方 `standby` を返して終わる。[1 つのスプールに橋は 1 つ](#1-つのスプールに橋は-1-つ)）。
3. 要求を名前の昇順で最大 16 件取り出す。1 件ずつ `<id>.req.json` → `<id>.work` へ rename で
   確保し（`claim`）、**失敗したら黙って飛ばす**。確保できたら読んで、すぐ消す。`claim` は
   POSIX では `rename`、Windows では共有なしで開いたハンドルで rename する（`MoveFileEx` は
   排他にならない。[作法](../protocol.md#プラグイン側の義務)）。
4. 確保したあとで読めなかった要求には、ファイル名の id が正しければ失敗で応える。
5. 要求ごとに:
   - `tools` → 道具の一覧を返す（表を検索しない）。
   - 種類 `shell` → action に載せて応答しない（2 件目は失敗で応える）。
   - 種類 `long` → 先に印へ `busy`（道具名）・`busy_id`（要求の id）・`busy_until`
     （今＋`timeoutSeconds`）を書いてから実行し、終わったら印をすぐ書き直す。
   - それ以外 → 実行して応答を書く。
   - **道具 1 つの例外はここで受けて失敗の応答にする**（橋を止めない）。
6. 印を 2 秒ごとに書き直す。書けなければ用意からやり直す（黙って続けない）。
7. 見え方（view）を返す。

- `shellReport` の応答は待機の判定より先に書きます。殻に頼んだ要求はこの橋が確保したもので、
  同時に起動した直後に待機へ回っても、応えるのはこの橋です。

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
| `restart_required` | 殻の ID の違う版が入っていて、再起動するまで効かないとき。殻が `VwServeInput::restartRequired` で渡し、本体が載せる（[ABI](abi.md#受け付け-1-回ごとに殻が渡すもの)） |

- **書く前に読みます。** 別の `pid` の印が `down` でなければ書かずに待機します（上の手順 2）。
  `removeStatus` も、印の `pid` が自分のときだけ消します（待機している側が、受け付けている
  側の印を消さないように）。
- **本体の入れ替えでは印を消しません**（[ABI](abi.md#vw_payload_shutdown-で生存の印を消さない理由)）。
  Vectorworks が終了すると印は書き直されなくなり、`pid` のプロセスも無くなるので「止まっている」と
  判定されます（[作法「生存の判定」](../protocol.md#生存の判定)）。
- 殻が受け付けを見送っている間（undo の記録・モーダル・長く走る道具）も印は書き直されません。
  呼ぶ側はこれを `pid` で「応えない（`unresponsive`）」と見分けるので、本体も殻も見送りの間に
  印を書く手段を持ちません（殻に書かせると殻の役割が増え、mac ではモーダルの最中にタイマー自体が
  刻まないので書けない）。

## 1 つのスプールに橋は 1 つ

Windows では Vectorworks 2026 を 2 つ起動でき、どちらのプラグインも同じスプールを使います
（[作法](../protocol.md#1-つのスプールに橋は-1-つ)）。先に印を書いていた方が受け付け、後から
来た方は待機します。

| 局面 | 起きること |
| --- | --- |
| 2 つ目の起動 | 開始時に有効な印があるので `sweep` しない。手順 2 で相手の `pid` を見て待機する |
| 同時に起動 | 両方が印を書きうる。後に書いた方の印が残り、次に読んだとき先に書いた方が待機に回る。その間の取り合いは確保（rename）で防ぐ |
| 受け付けている側がダイアログの最中など | 印は古びるが `pid` のプロセスがあるので `unresponsive`。引き継がない（戻った相手と入れ替わり続けないように） |
| 受け付けている側が終了・異常終了 | 印が消えるか、古びて `pid` のプロセスが無くなり `down` になる。待機している側が次の回に引き継ぐ。引き継ぎでは `sweep` しない |
| 待機している側が終了 | 何も起きない（印は相手のもの） |

- 同じ `pid` の印は自分のものとして扱います（本体の入れ替え後の続き）。
- 待機している Vectorworks の図面は CLI から操作できません。利用者は `vw2026 status` の
  `pid` でどちらが受け付けているかを見ます。

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
- **種類 `write` の道具は、undo の作法を通すまで表に載せません**（[未決 1](open-questions.md)）。

## 見え方（view）

殻が読む JSON です。`phase`（`serving` / `paused` / `standby` / `error`）・`spool`・
`served`・`failed`・`lastTool`・`secondsSinceRequest`・`message`・`version`・`reportDone`・
`action`。`standby` は別の Vectorworks が同じスプールを受け付けている間で、`message` に
相手の `pid` を出します。判断は持たせず、殻は `action` と `reportDone` だけを見ます。

- 見え方は**本体から殻への出力だけ**です。殻から本体へ伝えることは `VwServeInput` で渡します。
- 本体が動いていないときの失敗は見え方に出せないので、殻が[殻の診断](abi.md#殻の診断shelljson)
  （`shell.json`）に書きます。本体は `shell.json` を読みも消しもしません（`sweep` の対象外）。

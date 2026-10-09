# 受け付け（ブリッジ）

受け渡しの形は[受け渡しの作法](../protocol.md)が真実で、ここはプラグイン側の作りです。元の
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
| `src/Clock.cpp` | 依る | OS タイマーから `core::serve(…, tools::table())` を呼ぶ |

元では受け付けの手順（`serveMcpBridge`）が SDK 依存の `draw/McpBridge.cpp` に入っていました。
ここでは**手順を `core/Serve` へ出し、偽の道具の表でテストします**（終了の依頼の
返し方・壊れた要求・`busy` の書き方・印の間隔・確保の失敗・別の `pid` の印での待機）。

## スプールの用意

- 場所は `VW2026_SPOOL` があればそれ、無ければ `<一時ディレクトリ>/vectorworks2026-cli-bridge`
  （開発版は `-dev`）。一時ディレクトリは `std::filesystem::temp_directory_path`。**探索しません**
  （探すのは呼ぶ側の役割）。
- `prepare`: 0700 で作り、既にあれば持ち主と権限を確かめる（POSIX）。失敗したら 10 秒後に
  やり直す（理由は受け付けの結果に載せる）。
- **前の回の残骸は、有効な生存の印が無いときだけ消します**（`statusIsLive` が偽のときだけ
  `sweep`）。印が生きているのは、別の Vectorworks が同じスプールを受け付けているときで、
  そこで消すと相手に置かれたばかりの要求や相手が書いた応答まで消えます
  （[1 つのスプールに橋は 1 つ](#1-つのスプールに橋は-1-つ)）。`sweep` は `*.req.json`・
  `*.work`・`*.res.json`・`*.tmp` を消します。

## 受け付け 1 回（`core::serve`）

1. 印を読み、**別の `pid` の印が `down` でなければ待機**する（要求を取り出さず、印も書かず、
   結果 `standby` を返して終わる。[1 つのスプールに橋は 1 つ](#1-つのスプールに橋は-1-つ)）。
2. 要求を名前の昇順で最大 16 件取り出す。1 件ずつ `<id>.req.json` → `<id>.work` へ rename で
   確保し（`claim`）、**失敗したら黙って飛ばす**。確保できたら読んで、すぐ消す。`claim` は
   POSIX では `rename`、Windows では共有なしで開いたハンドルで rename する（`MoveFileEx` は
   排他にならない。[作法](../protocol.md#プラグイン側の義務)）。
3. 確保したあとで読めなかった要求には、ファイル名の id が正しければ失敗で応える。
4. 要求ごとに:
   - `tools` → 道具の一覧を返す（表を検索しない）。
   - 種類 `app`（`quit`）→ 応答を書き、終了の依頼を結果に載せて、**その回の残りの要求を
     取り出さずに戻る**（終了したら応えられないので、残りは置いたままにする）。
   - 種類 `long` → 先に印へ `busy`（道具名）・`busy_id`（要求の id）・`busy_until`
     （今＋`timeoutSeconds`）を書いてから実行し、終わったら印をすぐ書き直す。
   - それ以外 → 実行して応答を書く。
   - **道具 1 つの例外はここで受けて失敗の応答にする**（橋を止めない）。
5. 印を 2 秒ごとに書き直す。書けなければ用意からやり直す（黙って続けない）。
6. 結果（終了の依頼の有無と、受け付けの状態）を返す。

- 終了の依頼はタイマーが `core::serve` から戻ってから行います（[構成「受け付けの流れ」](architecture.md#受け付けの流れ)）。

## 生存の印

作法の[生存の印](../protocol.md#生存の印bridgejson)に、プラグインは次を書きます。

| フィールド | 値 |
| --- | --- |
| `plugin` | `cli` / `cli_dev`（`PLUGIN_VWR_ID`） |
| `channel` | `stable` / `dev` |
| `version` | `VW_BUILD_VERSION`（短い sha） |
| `branch` | `VW_BUILD_BRANCH` |
| `protocol` | 1 |
| `beat` | 今（epoch 秒） |
| `pid` | Vectorworks のプロセス ID（`getpid` / `GetCurrentProcessId`） |
| `busy` / `busy_id` / `busy_until` | 長く走る道具の最中だけ |

- **書く前に読みます。** 別の `pid` の印が `down` でなければ書かずに待機します（上の手順 2）。
- **印を消しません。** Vectorworks が終了すると印は書き直されなくなり、`pid` のプロセスも
  無くなるので「止まっている」と判定されます（[作法「生存の判定」](../protocol.md#生存の判定)）。
  終了の時機を捉える手段に頼らずに済みます。
- 受け付けを見送っている間（undo の記録・モーダル・長く走る道具）も印は書き直されません。
  呼ぶ側はこれを `pid` で「応えない（`unresponsive`）」と見分けるので、見送りの間に印を書く
  手段は持ちません（mac ではモーダルの最中にタイマー自体が刻まないので書けない）。

## 1 つのスプールに橋は 1 つ

Windows では Vectorworks 2026 を 2 つ起動でき、どちらのプラグインも同じスプールを使います
（[作法](../protocol.md#1-つのスプールに橋は-1-つ)）。先に印を書いていた方が受け付け、後から
来た方は待機します。

| 局面 | 起きること |
| --- | --- |
| 2 つ目の起動 | 開始時に有効な印があるので `sweep` しない。手順 2 で相手の `pid` を見て待機する |
| 同時に起動 | 両方が印を書きうる。後に書いた方の印が残り、次に読んだとき先に書いた方が待機に回る。その間の取り合いは確保（rename）で防ぐ |
| 受け付けている側がダイアログの最中など | 印は古びるが `pid` のプロセスがあるので `unresponsive`。引き継がない（戻った相手と入れ替わり続けないように） |
| 受け付けている側が終了・異常終了 | 印が古びて `pid` のプロセスが無くなり `down` になる。待機している側が次の回に引き継ぐ。引き継ぎでは `sweep` しない |
| 待機している側が終了 | 何も起きない（印は相手のもの） |

- 同じ `pid` の印は自分のものとして扱います。
- 待機している Vectorworks の図面は CLI から操作できません。利用者は `vw2026 status` の
  `pid` でどちらが受け付けているかを見ます。

## 道具の表

```cpp
enum class ToolKind { Read, Write, Long, App };

struct Tool {
    const char* name;            // [a-z0-9_]+(\.[a-z0-9_]+)*
    const char* description;     // 日本語。呼ぶ側がそのまま読む
    const char* inputSchema;     // JSON Schema（文字列）。tools の結果にそのまま載る
    ToolKind    kind;
    int         timeoutSeconds;  // 既定より長くかかりうるときだけ（0 = 既定）
    ToolFn      run;             // Json (const Json& args, std::string& error)
};
```

- **道具を足すときに触るのは表の 1 行と中身 1 つだけ**です。CLI は道具を知らないので直しません。
- `tools` の結果は `{"protocol":1,"tools":[{"name","description","inputSchema","kind","timeoutSeconds"?}]}`。
  `kind` は `read` / `write` / `long` / `app` を小文字で載せます（呼ぶ側が「図面を変えるか」を
  判定できるように）。
- 引数の検査は道具の中で行います（JSON Schema の検証器は持たない）。知らない引数は失敗で返します。
- **種類 `write` の道具は、undo の作法を通すまで表に載せません**（[未決 1](open-questions.md)）。

## 受け付けの結果

`core::serve` がタイマーへ返す値です。`phase`（`serving` / `standby` / `error`）・`message`
（`standby` なら相手の `pid`、`error` なら理由）・`quit`（終了の依頼。`restart` の値を添える）。
タイマーが見るのは `quit` だけで、ほかは単体テストで手順を確かめるためにあります。

# 受け付け（ブリッジ）

受け渡しの形は[受け渡しの作法](../protocol.md)が真実で、ここはプラグイン側の作りです。元の
`元:src/core/Bridge.{h,cpp}`・`元:src/core/Json.{h,cpp}`・`元:src/draw/McpBridge.{h,cpp}` を
移し、名前を作法に合わせ、印（状態のファイル）を除きます。

## 分け方

| ファイル | SDK | 中身 |
| --- | --- | --- |
| `src/core/Json.{h,cpp}` | 依らない | 小さな JSON（元のまま） |
| `src/core/Bridge.{h,cpp}` | 依らない | スプール 1 つ（`prepare` / `lock` / `sweep` / `poll` / `callerGone` / `reply`）・id の検査・要求と応答の形。場所は引数で受ける（テストは一時ディレクトリを渡す） |
| `src/core/Serve.{h,cpp}` | 依らない | **受け付け 1 回の手順**（下記）。道具の表と時刻を引数で受けるので無 SDK でテストできる |
| `src/tools/ToolTable.cpp` | 依る | 道具の表 |
| `src/tools/<道具>.cpp` | 依る | 道具の中身 |
| `src/Clock.cpp` | 依る | OS タイマーから `core::serve(…, tools::table())` を呼ぶ |

元では受け付けの手順（`serveMcpBridge`）が SDK 依存の `draw/McpBridge.cpp` に入っていました。
ここでは**手順を `core/Serve` へ出し、偽の道具の表でテストします**（終了の依頼の
返し方・壊れた要求・ロックを取れたときに 1 度だけ残骸を消すこと・ロックを取れないときに何も
しないこと・取り下げられた要求を飛ばすこと・待つ者の居ない要求を実行しないこと・失敗の `code`）。

## スプールの用意

- 場所は `<CLI>/spool`（[作法「スプールの場所」](../protocol.md#スプールの場所)）。環境変数は
  読まず、**探索しません**。
- `prepare`: 無ければ 0700 で作る（親も無ければ作る）。持ち主と権限は確かめない。失敗したら
  次の回にやり直す（理由は受け付けの結果に載せる）。
- `lock`: `bridge.lock` を掴む（POSIX は `flock(LOCK_EX|LOCK_NB)`、Windows は共有なしで開く。
  [作法「ロック」](../protocol.md#ロックbridgelock)）。取れたら終了まで放さない。POSIX は
  `O_CLOEXEC` を付けて開き、Windows は `lpSecurityAttributes` に null を渡して、子プロセスに
  受け継がせない（受け継がれると Vectorworks が終わってもロックが放されない）。
- `sweep`: **ロックを取れた直後に 1 度だけ**、`*.req.json`・`*.res.json`・`*.wait`・`*.tmp` を消す。
  ロックを取れたことは、ほかに受け付けている橋が居ないことを意味するので、相手の要求や
  応答を消すことはない。

## 受け付け 1 回（`core::serve`）

1. ロックを掴んでいなければ用意（`prepare` → `lock` → `sweep`）をする。
   **ロックを取れなければ何もせずに戻る**（結果 `waiting`。次の回に取り直す）。
2. 要求を名前の昇順で最大 16 件取り出す。1 件ずつ読み、**読めたら消し、消せたものだけを
   実行する**。読めない（呼ぶ側が取り下げた）・消せない（ほかのプロセスが一時的に掴んでいる）
   要求は黙って飛ばす。
   - 消した直後、実行する前に `<id>.wait` を掴めるか試す（`callerGone`。[作法「待つ印」](../protocol.md#待つ印idwait)）。
     掴めたら呼ぶ側は居ないので、`<id>.wait` を消し、**実行も応答もしない**。掴めない・無いなら
     次へ進む。
3. 読めたが壊れている要求には、ファイル名の id が正しければ `invalid_request` で応える。
4. 要求ごとに:
   - `tools` → 道具の一覧を返す（表を検索しない）。
   - 種類 `app`（`quit`）→ 応答を書き、終了の依頼を結果に載せて、**その回の残りの要求を
     取り出さずに戻る**（終了したら応えられないので、残りは置いたままにする）。
   - それ以外 → 実行して応答を書く。
   - 表に無い道具 → `unknown_tool` で応える。
   - **道具 1 つの例外はここで受けて `internal` の応答にする**（橋を止めない）。
5. 結果（終了の依頼の有無と、受け付けの状態）を返す。

- 終了の依頼はタイマーが `core::serve` から戻ってから行います（[構成「受け付けの流れ」](architecture.md#受け付けの流れ)）。

## 版の情報

**スプールに版の情報（印）を書きません。** 生死はロックで判定され（[作法「生存の判定」](../protocol.md#生存の判定)）、
作法の版は実行時に照合しない（[作法「版」](../protocol.md#版)）ので、ファイルとして置く理由が
ありません。版の情報は `ping` の結果で返します（[道具「ping」](tools.md#ping)）。

## 同じスプールを見る 2 つの Vectorworks

Windows では Vectorworks 2026 を 2 つ起動でき、どちらのプラグインも同じスプールを使います。
**先にロックを取った方が受け付け、後から来た方は毎回ロックを取りに行って失敗するだけです。**

| 局面 | 起きること |
| --- | --- |
| 2 つ目の起動 | ロックを取れないので何もしない（`sweep` もしない） |
| 同時に起動 | ロックはどちらか一方だけが取れる |
| 受け付けている側がダイアログの最中など | ロックは掴んだままなので引き継がない。終われば受け付けに戻る |
| 受け付けている側が終了・異常終了 | OS がロックを放す。もう一方が次の回にロックを取り、残骸を消して引き継ぐ |
| 受け付けていない側が終了 | 何も起きない |

- 受け付けていない Vectorworks の図面は CLI から操作できません。利用者は `vw2026 call ping` の
  `pid` でどちらが受け付けているかを見ます。
- 引き継ぎで残骸を消すので、前の側が受け付けていた間に置かれて処理されなかった要求は消えます。
  呼ぶ側は待ちきれずに終了コード 3 か 4 を受け取ります（前の側が終了しているので、どのみち
  応える者が居なかった要求です）。

## 道具の表

```cpp
enum class ToolKind { Read, Write, App };

struct Tool {
    const char* name;            // [a-z0-9_]+(\.[a-z0-9_]+)*
    const char* description;     // 日本語。呼ぶ側がそのまま読む
    const char* inputSchema;     // JSON Schema（文字列）。tools の結果にそのまま載る
    ToolKind    kind;
    ToolFn      run;             // Json (const Json& args, ToolError& error)
};

enum class ErrorCode { InvalidArgs, NoDocument, Internal };   // 道具が返す種別

struct ToolError {
    ErrorCode   code;
    std::string message;         // 日本語。応答の error にそのまま載る
};
```

- **道具を足すときに触るのは表の 1 行と中身 1 つだけ**です。CLI は道具を知らないので直しません。
- `tools` の結果は `{"protocol":4,"tools":[{"name","description","inputSchema","kind"}]}`。
  `kind` は `read` / `write` / `app` を小文字で載せます（呼ぶ側が「図面を変えるか」を
  判定できるように）。
- 引数の検査は道具の中で行います（JSON Schema の検証器は持たない）。知らない引数は
  `invalid_args` で返します（[作法「互換性」](../protocol.md#互換性)）。
- 失敗の種別は応答の `code` に小文字の綴り（`invalid_args` / `no_document` / `internal`）で
  載せます（[作法「応答」](../protocol.md#応答idresjson)）。`invalid_request` と `unknown_tool` は
  `core::serve` が付けます。
- **種類 `write` の道具は、undo の作法を通すまで表に載せません**（[未決 1](open-questions.md)）。

## 受け付けの結果

`core::serve` がタイマーへ返す値です。`phase`（`serving` / `waiting` / `error`）・`message`
（`error` なら理由）・`quit`（終了の依頼）。タイマーが見るのは `quit` だけで、ほかは単体テストで
手順を確かめるためにあります。

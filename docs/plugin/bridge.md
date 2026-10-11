# 受け付け（ブリッジ）

受け渡しの形は[受け渡しの作法](../protocol.md)が真実で、ここはプラグイン側の作りです。元の
`元:src/core/Bridge.{h,cpp}`・`元:src/core/Json.{h,cpp}`・`元:src/draw/McpBridge.{h,cpp}` を
移し、名前を作法に合わせ、印（状態のファイル）を除きます。

## 分け方

| ファイル | SDK | 中身 |
| --- | --- | --- |
| `src/core/Json.{h,cpp}` | 依らない | 小さな JSON（元から移し、入れ子の深さの上限を足す。[受け付け 1 回](#受け付け-1-回coreserve)の 3） |
| `src/core/Bridge.{h,cpp}` | 依らない | スプール 1 つ（`prepare` / `lock` / `sweep` / `poll` / `waitState` / `reply`）・id の検査・要求と応答の形。場所は引数で受ける（テストは一時ディレクトリを渡す） |
| `src/core/Serve.{h,cpp}` | 依らない | **受け付け 1 回の手順**（下記）と、占有の印（発行した 1 つをメモリに持つ）。道具の表を引数で受けるので無 SDK でテストできる |
| `src/tools/ToolTable.cpp` | 依る | 道具の表 |
| `src/tools/<道具>.cpp` | 依る | 道具の中身 |
| `src/Clock.cpp` | 依る | OS タイマーから `core::serve(…, tools::table())` を呼ぶ |

元では受け付けの手順（`serveMcpBridge`）が SDK 依存の `draw/McpBridge.cpp` に入っていました。
ここでは**手順を `core/Serve` へ出し、偽の道具の表でテストします**（`tests/CoreServeTests.cpp`。終了の依頼の
返し方・壊れた要求・ロックを取れたときに 1 度だけ残骸を消すこと・ロックを取れないときに何も
しないこと・無くなっていた要求を飛ばすこと・待つ者の居ない要求と待つ印の無い要求を実行しないこと・失敗の `code`・
回の始めに並べた要求だけを処理すること・綴りを満たさない id を取り出さないこと・大きすぎる要求と入れ子の深すぎる要求・
占有に照らすこと）。同じ手順を、作法の見本（[`protocol/fixtures/`](../../protocol/fixtures/README.md)）に
照らします（`tests/ProtocolFixturesTests.cpp`）。見本は Go の偽のプラグイン（`cli/internal/fakeplugin`）の
テストも読むので、両側の受け付けの振る舞いが食い違えばどちらかが落ちます。

## スプールの用意

- 場所は `<CLI>/spool`（[作法「スプールの場所」](../protocol.md#スプールの場所)）。環境変数は
  読まず、**探索しません**（`core::bridgeSpoolDir`。mac は `getpwuid_r(getuid())` の `pw_dir`、Windows は
  `SHGetKnownFolderPath(FOLDERID_LocalAppData)`）。パスは `std::filesystem::path` で持ち、Windows でも
  利用者の名前を ANSI のコードページに通しません。
- 開くファイル（`bridge.lock`・`<id>.wait`・要求・応答）は OS の呼び出しで開きます（POSIX は
  `O_CLOEXEC`、Windows は `lpSecurityAttributes` に null）。`std::fstream` は子プロセスに受け継がせない
  指定ができないので使いません。
- `prepare`: 無ければ 0700 で作る（親も無ければ作る）。持ち主と権限は確かめない。失敗したら
  次の回にやり直す（理由は受け付けの結果に載せる）。
- `lock`: `bridge.lock` を掴む（POSIX は `flock(LOCK_EX|LOCK_NB)`、Windows は共有なしで開く。
  [作法「ロック」](../protocol.md#ロックbridgelock)）。取れたら終了まで放さない。POSIX は
  `O_CLOEXEC` を付けて開き、Windows は `lpSecurityAttributes` に null を渡して、子プロセスに
  受け継がせない（受け継がれると Vectorworks が終わってもロックが放されない）。
- `sweep`: **ロックを取れた直後に 1 度だけ**、`*.req.json`・`*.res.json`・`*.wait`・`*.tmp` の
  うち、**`<id>.wait` を掴める・無い id のものだけ**を消す（id はファイル名の最初の `.` より前）。
  待つ印が掴まれている id のファイルは残す（[作法「ロック」](../protocol.md#ロックbridgelock)）。

## 受け付け 1 回（`core::serve`）

1. ロックを掴んでいなければ用意（`prepare` → `lock` → `sweep`）をする。
   **ロックを取れなければ何もせずに戻る**（結果 `waiting`。次の回に取り直す）。
2. 要求を名前の昇順で 1 件ずつ取り出す。id の綴りを満たさない要求は取り出さない（読まず・
   消さず・応えない）。1 件ずつ読み、**読めたら消し、消せたものだけを実行する**。読めない
   （無くなっていた）・消せない（ほかのプロセスが一時的に掴んでいる）要求は黙って飛ばす。
   - **並べるのは回の始めに 1 度だけ**で、並べた要求をすべて処理する（件数と時間の上限は設けない。
     [作法「プラグイン側の義務」](../protocol.md#プラグイン側の義務)）。その後に置かれた要求は次の回に
     回る。取り出した要求は同じ回の中で応えるまで処理する。
   - 消した直後、実行する前に `<id>.wait` を掴めるか試す（`waitState`。[作法「待つ印」](../protocol.md#待つ印idwait)）。
     掴めたら呼ぶ側は居ないので、`<id>.wait` を消し、**実行も応答もしない**。無ければ実行せずに
     `no_wait` で応える。掴めない（呼ぶ側が待っている）なら次へ進む。
3. 読めたが壊れている要求には `invalid_request` で応える（id は 2 で確かめてある）。壊れている
   とは、1 MiB を超える・入れ子の深さが 64 を超える・JSON として読めない・`tool` が無い・
   `tool`・`args`・`session` の型が違うこと（`tool` の `null`・空の文字列も壊れている。キーは大文字と
   小文字を区別して照合する）。`args` が無い・`null`
   なら既定値の `{}` を道具へ渡し、`session` の `null`・空の文字列は無いものとして扱う
   （[作法「要求」](../protocol.md#要求idreqjson)）。**深さは `core/Json` が解析の中で数え、上限を
   超えたら再帰を降りずに失敗する**（解析の前に別に数える必要はない。スタックを溢れさせない
   ことが目的）。`core/Json` は RFC 8259 のとおりに厳しく読み、UTF-8 として正しくないテキストも
   読まない。オブジェクトの鍵の重なりは表で判定する（1 MiB に鍵を十数万並べた要求で、受け付けを
   長く止めない）。
4. 要求ごとに:
   - `tools`・`ping` 以外は、まず占有に照らす（[作法「占有」](../protocol.md#占有セッション)の表）。
     占有の印は `core::serve` が持つ状態（1 つだけ。メモリだけで、ファイルに書かない）で、
     Vectorworks が終われば消える。印は推測されにくい乱数から作る。
   - `session_start` → 占有されていなければ印を発行して返す。`session_end` → 印を消す（表を検索しない）。
   - `tools` → 道具の一覧を返す（表を検索しない）。予約された道具（`tools`・`session_start`・
     `session_end`）は一覧に載せない（`ping` は中身が SDK に依るので表に載せ、一覧にも出る）。
   - 種類 `app`（`quit`）→ 応答を書き、終了の依頼を結果に載せて、**その回の残りの要求を
     取り出さずに戻る**（終了したら応えられないので、残りは置いたままにする）。
   - それ以外 → 実行して応答を書く。
   - 表に無い道具 → `unknown_tool` で応える。
   - **道具 1 つの例外はここで受けて `internal` の応答にする**（橋を止めない）。`std::exception`
     以外の例外も受ける。
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
| 受け付けている側が終了・異常終了 | OS がロックを放す。もう一方が次の回にロックを取り、待つ者の居ない残骸を消して引き継ぐ |
| 受け付けていない側が終了 | 何も起きない |

- 受け付けていない Vectorworks の図面は CLI から操作できません。利用者は `vw2026 call ping` の
  `pid` でどちらが受け付けているかを見ます。
- 前の側が受け付けていた間に置かれて処理されなかった要求は、呼ぶ側が待っていれば残り、引き継いだ
  側が処理します。前の側が書いて呼ぶ側がまだ読んでいない応答も残ります。

## 道具の表

```cpp
// src/core/Serve.h
enum class ToolKind { Read, Write, App };

struct ToolContext {
    bool session;                // 占有の印を発行しているか（ping の session）
};

struct ToolError {
    bool        failed;          // 真のときだけ応答を失敗にする
    BridgeCode  code;            // InvalidArgs か Internal（ほかを付けても internal になる）
    std::string message;         // 日本語。応答の error にそのまま載る
    Json invalidArgs(std::string why);   // failed を立てて null を返す
    Json internal(std::string why);
};

using ToolFn = std::function<Json(const Json& args, const ToolContext& context, ToolError& error)>;

struct Tool {
    std::string name;            // [a-z0-9_]+(\.[a-z0-9_]+)*
    std::string description;     // 日本語。呼ぶ側がそのまま読む
    std::string inputSchema;     // JSON Schema（オブジェクトのテキスト）。tools の結果に載る
    ToolKind    kind;
    ToolFn      run;
};

using ToolTable = std::vector<Tool>;
```

- **道具を足すときに触るのは表の 1 行と中身 1 つだけ**です。CLI は道具を知らないので直しません。
- 表の正しさ（名前の綴り・重複・予約された名前・`inputSchema` がオブジェクトの JSON か・`ping` が
  `read` か）は `core::validateToolTable` で確かめます（表のテストが呼ぶ）。
- `tools` の結果は `{"protocol":5,"tools":[{"name","description","inputSchema","kind"}]}`。
  `kind` は `read` / `write` / `app` を小文字で載せます（呼ぶ側が「図面を変えるか」を
  判定できるように）。
- 引数の検査は道具の中で行います（JSON Schema の検証器は持たない）。省略できる引数には
  既定値を設け（`inputSchema` の `default` にも書く）、知らない引数・知っている引数の未知の値は
  `invalid_args` で返します（[作法「互換性」](../protocol.md#互換性)）。
- 失敗の種別は応答の `code` に小文字の綴り（`invalid_args` / `internal`）で
  載せます（[作法「応答」](../protocol.md#応答idresjson)）。`invalid_request`・`unknown_tool`・`no_wait`・`busy`・`no_session` は
  `core::serve` が付けます（`busy`・`no_session` は占有に照らした結果。道具は付けない）。図面が開かれていないなど、道具によって失敗になるかが違う理由は
  `internal` とし、`error` に書きます。
- **種類 `write` の道具は、undo の作法を通すまで表に載せません**（[未決 1](open-questions.md)）。

## 受け付けの結果

`core::serve` がタイマーへ返す値です。`phase`（`serving` / `waiting` / `error`）・`message`
（`error` なら理由）・`quit`（終了の依頼）。タイマーが見るのは `quit` だけで、ほかは単体テストで
手順を確かめるためにあります。

# 受け付け（ブリッジ）

受け渡しの形は[通信規則](../protocol.md)が真実で、ここはプラグイン側の作りです。

## 分け方

| ファイル | SDK | 中身 |
| --- | --- | --- |
| `src/core/Protocol.h` | 依らない | 通信規則の定数（ファイルの綴り・上限・予約された道具・`code`）と id の検査。`protocol/fixtures/constants.json` と対（[通信規則の見本](../fixtures.md)） |
| `src/core/Json.{h,cpp}` | 依らない | 小さな JSON（入れ子の深さに上限を設ける。読み方は Go の `encoding/json` に合わせる。[受け付け 1 回](#受け付け-1-回coreserverserve)の 3） |
| `src/core/Files.h`・`FilesPosix.cpp`・`FilesWindows.cpp` | 依らない | スプールに使う OS の呼び出し（ロック・待つ印・読み書き・`<CLI>` の親の場所）。開くファイルは子プロセスに受け継がせない。開いて待たない（通常のファイルだけを読み書きし、リンクはたどらない。POSIX は `O_NONBLOCK`・`O_NOFOLLOW` で開く。Windows は `\\?\` と `FILE_FLAG_OPEN_REPARSE_POINT` で開き（パスは絶対パスに限る）、reparse point（OneDrive などのクラウドのファイル・重複除去を含む）と中身がローカルにないファイルを断る）。書きかけは消してから新しく作る（リンクの先を壊さない。[通信規則「プラグイン側の義務」](../protocol.md#プラグイン側の義務)） |
| `src/core/Bridge.{h,cpp}` | 依らない | スプール 1 つ（`prepare` / `lock` / `sweep` / `poll` / `take` / `waitState` / `reply`）と、スプールの場所（`defaultSpoolDir`）。場所は引数で受ける（テストは一時ディレクトリを渡す） |
| `src/core/Serve.{h,cpp}` | 依らない | **受け付け 1 回の手順**（`core::Server::serve`。下記）と、占有の印（`Server` が発行した 1 つをメモリに持つ）。道具の表と時刻を引数で受けるので無 SDK でテストできる |
| `src/core/Ping.{h,cpp}` | 依らない | 道具 `ping` の中身のうち SDK に依らない部分（引数の検査と結果の形）。SDK から読む値（現在のレイヤの名前）は引数で受ける |
| `src/tools/ToolTable.cpp` | 依る | 道具の表 |
| `src/tools/<道具>.cpp` | 依る | 道具の中身。SDK から値を読み、SDK に依らない部分は `src/core` の関数に渡す（無 SDK でテストするため） |
| `src/Clock.cpp` | 依る | OS タイマーから `core::Server::serve(tools::table(), …)` を呼ぶ |

**受け付けの手順は SDK に依らない `core/Serve` に置き、偽の道具の表でテストします**（終了の依頼の
返し方・壊れた要求・ロックを取れたときに 1 度だけ残骸を消すこと・ロックを取れないときに何も
しないこと・無くなっていた要求を飛ばすこと・待つ者の居ない要求と待つ印の無い要求を実行しないこと・失敗の `code`・
回の始めに並べた要求だけを処理すること・綴りを満たさない id を取り出さないこと・大きすぎる要求と入れ子の深すぎる要求・
占有に照らすこと）。表せるものは[通信規則の見本](../fixtures.md)に置き、CLI の偽のプラグイン
（`cli/internal/fakeplugin`）も同じ見本で確かめます。

## スプールの用意

- 場所は `<CLI>/spool`（[通信規則「スプールの場所」](../protocol.md#スプールの場所)）。環境変数は
  読まず、**探索しません**。
- `prepare`: 無ければ 0700 で作る（親も無ければ作る）。持ち主と権限は確かめない。失敗したら
  次の回にやり直す（理由は受け付けの結果に載せる）。
- `lock`: `bridge.lock` を掴む（POSIX は `flock(LOCK_EX|LOCK_NB)`、Windows は共有なしで開く。
  [通信規則「ロック」](../protocol.md#ロックbridgelock)）。取れたら終了まで放さない。POSIX は
  `O_CLOEXEC` を付けて開き、Windows は `lpSecurityAttributes` に null を渡して、子プロセスに
  受け継がせない（受け継がれると Vectorworks が終わってもロックが放されない）。`bridge.lock` が
  通常のファイルでない・中身がローカルにないとして断ったら、消して作り直す。作り直すのは
  1 つの Vectorworks（`Lock`）につき 1 度だけで、2 度目からは理由を返して失敗する（受け付けの
  たびに消して作るのを繰り返さない。[通信規則「ロック」](../protocol.md#ロックbridgelock)の例外）。
- `sweep`: **ロックを取れた直後に 1 度だけ**、`*.req.json`・`*.res.json`・`*.wait`・`*.tmp` の
  うち、**`<id>.wait` を掴める・無い id のものだけ**を消す（id はファイル名の最初の `.` より前）。
  待つ印が掴まれている id のファイルは残す（[通信規則「ロック」](../protocol.md#ロックbridgelock)）。

## 受け付け 1 回（`core::Server::serve`）

`serve` は道具の表・時刻と、終了の依頼の最中か（`core::Activity`。`Idle`・`Quitting`。
[構成「Vectorworks が処理中」](architecture.md#vectorworks-が処理中)）を受け取ります。

0. **`serve` の途中にまた呼ばれたら（受け付けの入れ子）**、外の回の状態に触れず、取り出せる要求を
   すべて `busy` で断って戻る。手順は 2 の取り出しと待つ印の確かめだけで、解析も道具の実行もしない
   （占有の印を取り合わない。道具の SDK 呼び出しの途中で SDK を呼ばない）。外の回がすでに並べて
   いた要求も断り、外の回は取り出せずに飛ばす。
1. ロックを掴んでいなければ用意（`prepare` → `lock` → `sweep`）をする。
   **ロックを取れなければ何もせずに戻る**（結果 `waiting`。次の回に取り直す）。
2. 要求を名前の昇順で 1 件ずつ取り出す。id の綴りを満たさない要求は取り出さない（読まず・
   消さず・応えない）。1 件ずつ読み、**読めたら消し、消せたものだけを実行する**。読めない
   （無くなっていた）・消せない（ほかのプロセスが一時的に掴んでいる）要求は黙って飛ばす。
   - **並べるのは回の始めに 1 度だけ**で、並べた要求をすべて処理する（件数と時間の上限は設けない。
     [通信規則「プラグイン側の義務」](../protocol.md#プラグイン側の義務)）。その後に置かれた要求は次の回に
     回る。取り出した要求は同じ回の中で応えるまで処理する。受け付けの入れ子が起きたときは、並べた
     要求のうち入れ子の回が先に取り出して断ったものを飛ばす（0）。
   - 消した直後、実行する前に `<id>.wait` を掴めるか試す（`waitState`。[通信規則「待つ印」](../protocol.md#待つ印idwait)）。
     掴めたら呼ぶ側は居ないので、`<id>.wait` を消し、**実行も応答もしない**。無ければ実行せずに
     `no_wait` で応える。掴めない（呼ぶ側が待っている）なら次へ進む。
3. 読めたが壊れている要求には `invalid_request` で応える（id は 2 で確かめてある）。壊れている
   とは、1 MiB を超える・入れ子の深さが 64 を超える・JSON として読めない・`tool` が無い・
   `tool`・`args`・`session` の型が違うこと（`tool` の `null`・空の文字列も壊れている。キーは大文字と
   小文字を区別して照合する）。`args` が無い・`null`
   なら既定値の `{}` を道具へ渡し、`session` の `null` と空の文字列は無いものとして扱う
   （[通信規則「要求」](../protocol.md#要求idreqjson)）。**深さは `core/Json` が解析の中で数え、上限を
   超えたら再帰を降りずに失敗する**（解析の前に別に数える必要はない。スタックを溢れさせない
   ことが目的）。文字列の不正な UTF-8 と対にならないサロゲートは U+FFFD に置き換えて読み、
   書き出すときも同じにする（応答を常に UTF-8 にする。道具が返す例外の文言が UTF-8 でなくてもよい）。
4. 要求ごとに（[通信規則「占有」](../protocol.md#占有セッション)の表）:
   - `tools` → 道具の一覧を返す（表を検索しない。占有に照らさない）。
   - `session_start` → 占有されていなければ印を発行して返す（されていれば `occupied`）。
     `session_end` → 占有に照らしてから印を消す（表を検索しない）。どちらも終了の依頼の最中かには照らさない。
     占有の印は `core::Server` が持つ状態（1 つだけ。メモリだけで、ファイルに書かない）で、
     Vectorworks が終われば消える。印は `<時刻のナノ秒 19 桁>-<乱数 16 桁の 16 進>` で、乱数は
     `std::random_device` から作る。
   - 表に無い道具 → `unknown_tool` で応える（占有に照らさない）。
   - 種類 `read`（`ping` を含む）→ 占有に照らさずに実行して応答を書く。
   - 種類 `write`・`app` → 占有に照らし（無ければ `no_session`、ほかの印なら `occupied`）、終了の
     依頼の最中（`Quitting`）なら `busy` で断る。通れば実行して応答を書く。
   - 道具が成功し、`ToolStatus::quit`（終了の依頼）を立てていて、応答を書けたら、終了の依頼を結果に載せて、
     **その回の残りの要求を取り出さずに戻る**（終了したら応えられないので、残りは置いたままにする）。
     種類では決めない（`quit` だけが立てる。`save`・`close` などの種類 `app` の道具は立てない）。
     道具が失敗した・応答を書けなかったら、終了を頼まずに続ける。道具が成功したが結果がオブジェクトで
     ない（`internal` で応える）ときは、道具は実行したあとなので、終了の依頼を取り消さない。
   - **道具 1 つの例外はここで受けて `internal` の応答にする**（橋を止めない）。道具の外（占有の印を
     作るときなど）で要求 1 件の処理の中で起きた例外も、`serve` が受けて `internal` で応える。
5. 結果（終了の依頼の有無と、受け付けの状態）を返す。

- 終了の依頼はタイマーが `serve` から戻ってから行います（[構成「受け付けの流れ」](architecture.md#受け付けの流れ)）。
- 要求 1 件の処理（解析・占有・道具）の例外は `serve` が受けて `internal` で応えます。その応答は
  前もって書き出しておき、`catch` の中では確保しません（メモリが足りないときにまた投げないため）。
  応答を書く（パスを組み立てる）ときにも確保できなければ、その例外は下の `Clock` へ出て、その要求には
  応えません。道具を実行したあと、応答を書き出す間に投げたときは、実行した要求に `internal` で
  応えることになります（避けられない）。
- それ以外（スプールの一覧を読むときにメモリが足りないなど）で `serve` そのものが投げた例外は、
  呼ぶタイマー（`Clock`）が受け、Vectorworks へ通しません。
- **占有の印の発行・終了と終了の依頼は、応答を書けたときだけ反映します**（[通信規則「占有」](../protocol.md#占有セッション)）。
  `answer` は状態を変えずに、変えるものを `Outcome` で返し、`serve` が `reply` に成功してから
  反映します。
- 解析は要求の大きさに対して線形の時間で終えます（同じキーは、キーが増えたら索引で探す）。1 MiB の
  要求で受け付けの 1 回が長引き、図面の操作を止めないためです。

## 版の情報

**スプールに版の情報（印）を書きません。** 生死はロックで判定され（[通信規則「生存の判定」](../protocol.md#生存の判定)）、
通信規則の版は実行時に照合しない（[通信規則「版」](../protocol.md#版)）ので、ファイルとして置く理由が
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
// Read: 図面を変えない。占有に照らさない。
// Write: 図面の中身を変える。undo の手順に従う（下記）。
// App: Vectorworks・図面のファイルや窓に働き、図面の中身は変えない（save・close・quit など）。
// Write と App は占有の中でだけ呼べ、終了の依頼の最中なら busy で断る。
// 終了の依頼は種類では決めず、道具が ToolStatus::quit で返す。
enum class ToolKind { Read, Write, App };

// 道具が返す失敗の種別。None は失敗していない。
enum class ErrorCode { None, InvalidArgs, Internal };

struct ToolStatus {
	ErrorCode code = ErrorCode::None;
	// 日本語。応答の error にそのまま載る。
	std::string message;
	// 失敗の詳細（JSON のオブジェクト）。あれば応答の detail にそのまま載る（通信規則「応答」）。
	// 空（null）なら detail を付けない。quit が開いている図面の一覧を返すのに使う。
	Json detail;
	// 終了の依頼。成功し、応答を書けたとき、受け付けの外で終了を頼む。quit だけが立てる。
	bool quit = false;
};

// 道具に渡す受け付けの状態。
struct ToolContext {
	// 占有の印を発行しているか（ping の session）。
	bool session = false;
};

// 失敗と終了の依頼は status に書き（戻り値は使わない）、例外は Server が internal で応える。
using ToolFn = Json (*)(const Json &args, const ToolContext &context, ToolStatus &status);

struct Tool {
	// [a-z0-9_]+(\.[a-z0-9_]+)*
	const char *name;
	// 日本語。呼ぶ側がそのまま読む。
	const char *description;
	// JSON Schema（文字列）。tools の結果にそのまま載る。
	const char *inputSchema;
	ToolKind kind;
	ToolFn run;
};
```

- **道具を足すときに触るのは表の 1 行と中身 1 つだけ**です。CLI は道具を知らないので直しません。
- `tools` の結果は `{"protocol":5,"tools":[{"name","description","inputSchema","kind"}]}`。
  `kind` は `read` / `write` / `app` を小文字で載せます（呼ぶ側が「図面を変えるか」を
  判定できるように）。予約された道具のうち `tools`・`session_start`・`session_end` は表に載せず、
  `tools` の結果にも出しません。`ping` と `quit` は表に載せます（[通信規則「予約された道具」](../protocol.md#予約された道具)）。
- 道具の結果は JSON のオブジェクトで返します（[通信規則「応答」](../protocol.md#応答idresjson)）。返す値が無ければ
  `Json()` でよく、`Server` が `{}` にします。オブジェクトでない値は道具の不具合として、`Server` が
  `internal` で応えます。
- 道具が受け付けの状態を知るのは `ToolContext` からだけです（`ping` の `session`）。
- 引数の検査は道具の中で行います（JSON Schema の検証器は持たない）。省略できる引数には
  既定値を設け（`inputSchema` の `default` にも書く）、知らない引数・知っている引数の未知の値は
  `invalid_args` で返します（[通信規則「互換性」](../protocol.md#互換性)）。
- 失敗の種別は応答の `code` に小文字の綴り（`invalid_args` / `internal`）で
  載せます（[通信規則「応答」](../protocol.md#応答idresjson)）。`invalid_request`・`unknown_tool`・`no_wait`・`busy`・`occupied`・`no_session` は
  `core::Server::serve` が付けます（`occupied`・`no_session` は占有に照らした結果、`busy` は終了の依頼の最中か・受け付けの入れ子かに照らした結果。道具は付けない）。図面が開かれていないなど、道具によって失敗になるかが違う理由は
  `internal` とし、`error` に書きます。呼ぶ側が機械的に読む原因は `ToolStatus::detail` に書き、
  `invalid_args`・`internal` の応答の `detail` として載せます。`detail` はオブジェクトに限り、オブジェクトで
  ない値は道具の不具合として、`Server` が `detail` を付けずに `internal` で応えます。成功したときの
  `detail` は載せません。
- **種類 `write` の道具は、undo の手順に対応するまで表に載せません**（[未決 1](open-questions.md)）。
- **種類 `write` の道具は、自分で undo の記録を開き、受け付けから戻る前に必ず閉じます**（要求 1 件で
  undo 1 つ）。閉じずに戻ると記録が数十秒開いたままになり、その間の書き込みがそこへ入る
  （[Findings「Timers and Notifications」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Timers%20and%20Notifications.md) の 7「落とし穴」）。自分で閉じる限り、
  ほかの記録と混ざらないので、受け付けは undo の記録が開いているかを見ません
  （[構成「Vectorworks が処理中」](architecture.md#vectorworks-が処理中)）。

## 受け付けの結果

`core::Server::serve` がタイマーへ返す値（`ServeResult`）です。`phase`（`serving` / `waiting` /
`error`）・`message`（`error` なら理由）・`quit`（終了の依頼）。タイマーが見るのは `quit` だけで、
ほかは単体テストで手順を確かめるためにあります。

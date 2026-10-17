# 実装の順序

**1 段 1 PR**で、各段の終わりに確かめ方を決めておきます。実機でしか確かめられない段
（プラグイン・スクリプト）は、**利用者が実機で「確認できた」と言うまでマージしません**
（CI が通ることも、道具が応えることも、実機の確認の代わりにはならない）。

| 段 | 中身 | 確かめ方 | 実機 |
| --- | --- | --- | --- |
| 1 | **SDK に依らない共通部**: `core/Json`（入れ子の深さに上限を設ける）・`core/Bridge`・`core/Serve`（偽の道具の表・占有）・CMake（共通部・テスト・ファジングだけ）・`protocol/fixtures/`・`cpp.yml`（`core/Json` のファジングを含む）・`format.sh` / `lint.sh` に C++ と PowerShell を足す（[整形](../format.md)・[検査](../lint.md)）・`CLAUDE.md` の追記（PR の進め方） | CI（ASan・UBSan）。Go 側のテストも同じ見本を読む | 不要 |
| 2 | **骨格と配布**: CMake にプラグインを足す・`BuildConfig`・`ModuleMain`・`Clock`（受け付けまで。終了の依頼と `Quitting` は段 5）・道具 `ping`（表に載せるのはこれだけ。使う `GetCurrentLayer()` の図面が無いときの挙動は [SDK の調査 4](../sdk-research.md#4-最初の道具が使う-sdk-呼び出し)）・リソース・`build.yml`（mac と Windows のビルド・梱包（`bin/vw2026`）・main のリリース・PR のプレリリース）・`cleanup-dev-release.yml` | CI。リリースの資産を取ってきて、zip の形（直下に `build.json` と `bin/`）と `bin/vw2026 version` が動くことを確かめる。実機で、PR のプレリリースの zip を `curl -L` か `gh release download` で取ってきて `bin/` 以外を `<Plug-Ins>/cli/` に置き、zip の `bin/vw2026` で `status` が `running:true` になり `call ping` が応える（**拡張機能なしで読み込まれることの確認を兼ねる**。[構成](architecture.md#拡張機能を登録しない)）。`ping` の `version` が PR の head の sha、`branch` がその PR のブランチ。`call tools` に `ping` だけが載る。起動の直後（起動画面の間）に置いた `call ping` が、落ちずに最初の刻みで応える。図面を 1 枚も開いていないときの `ping` が、`document_open` と `current_layer` について [道具](tools.md#ping)に書いたとおりに答える | 要 |
| 3 | **インストール・アンインストール**: `vw2026 install` / `uninstall`（資産の取得・新しいビルドの判定・付け替え・CLI の入れ替え・PATH）・初回のスクリプト（`get-vw2026.sh` / `.ps1`）・`CLAUDE.md` に消すコードの歯止めを写す | CI（Go の単体テスト。偽の HTTP サーバー。動いている CLI の入れ替えを Windows で押さえる。shellcheck・PSScriptAnalyzer）。実機で、PR のプレリリースの初回のスクリプトを `--tag dev-<slug>` を付けて curl からパイプで実行 → 新しい端末で `vw2026 status` が動き、起動して `call ping` の `version` が PR の head の sha になる。動いている間に版の違うタグ（`install --tag stable`）を指定すると「Vectorworks が動いている」終了コードで何も変わらない（同じタグでは版の比較が先に `up_to_date` を返すので、この経路を通らない）。Vectorworks を手で終了 → `install --tag dev-<slug>` で `up_to_date`。`install --tag dev-<slug> --plugins-dir <場所>` で既定以外へ入れられる（タグを付けないと `stable` の CLI に入れ替わり、それには `uninstall` が無い）。`uninstall` でリンクと PATH の項目だけが消える（既定以外の場所は `--plugins-dir` を付ける）。main（`stable`）へ戻す確認は、`stable` の CLI に `install` が無いので段 4 で行う | 要 |
| 4 | **共通部の変更**: 種類 `app` から終了の依頼を切り離し、`ToolError` を `ToolStatus`（`detail`・`quit` を持つ）に改める。失敗の応答に省略できる `detail` を足し、`spool.Response` に `Detail` を持たせ、`vw2026 call` が失敗の `detail` を標準出力に出して `Help()` を直す。`src/core`・偽のプラグイン・見本・[通信規則](../protocol.md)を同じ PR で合わせる（道具は足さない。`ping` だけ） | CI（C++ と Go のテストが同じ見本を読む。偽のプラグインが `detail` を返し、`vw2026 call` が標準出力に出す）。実機で、PR のプレリリースを入れて `call ping` が段 2 と同じに応える（退行が無い） | 要 |
| 5 | **読む道具と図面を扱う道具**: `layers` / `classes` / `layer_objects` / `object_counts` と `documents` / `save` / `close`（[道具](tools.md#図面を扱う道具documentssaveclose)。使う SDK 呼び出しが SDK リファレンスで確かめられてから。[SDK の調査 4](../sdk-research.md#4-最初の道具が使う-sdk-呼び出し)） | 実機で、レイヤ・クラス・オブジェクトの数と名前を用意した見本の図面に対して、結果が Vectorworks の画面（オーガナイザ・情報パレット）の表示と一致する。`documents` が開いている図面の一覧と一致し、`save` で無題の図面を一時フォルダへ・保存済みの図面を自分のパスへ保存でき（前面の図面が保存の前後で変わらない）、保存済みの図面に `to` を渡すと `invalid_args` で断られ、開いているファイルを保存先にすると `invalid_args` で断られ（警告のダイアログが出ない）、保存した図面の `close` は保存の確認なしに閉じ、保存していない無題の図面の `close` は保存の確認が出て取り消すと `internal` で失敗して図面が残る。確認のあと、Vectorworks を終了して `install`（タグなし）で main の版に戻り、`launch` のあと `call ping` の `version` が main の sha になる（段 3 の main へ戻す確認を兼ねる） | 要 |
| 6 | **終了の依頼と占有**: 道具 `quit`・`Clock` の終了の依頼（使う SDK 呼び出しが SDK リファレンスで確かめられてから。[SDK の調査 4](../sdk-research.md#4-最初の道具が使う-sdk-呼び出し)） | CI。実機で、図面（無題と、保存済みの図面を変更したもの）を開いて `session start` のあと `call quit` を呼ぶと、ダイアログが出ずに `internal` で失敗して `detail` に開いている図面の一覧が載り、`call ping` が応え、`session end` で占有が解ける。図面をすべて閉じてから `call quit` を呼ぶと、ダイアログが出ずに `status` が止まっていると答える。[インストール「流れ」](install-and-update.md#流れ)の組み合わせ（`session start` → `call quit` → 止まるのを待つ → `install` → `launch` → 動き出すのを待つ。待つのは `status` の繰り返し）で `call ping` の `version` が変わる | 要 |

**段 2・3 を先に置く理由。** 最小限のプラグイン（`ping` だけ）をリリースし、`vw2026 install` で
入れられるようにしてから道具を足します。段 4 以降の実機確認は PR のプレリリースを `install` で
入れて行えるので、手で zip を置く手間が段 2 だけで済みます。`quit` が入る段 6 までは、更新の前に
利用者が Vectorworks を手で終了します。占有（`session_start` / `session_end`）は段 1 の共通部に
あり段 2 から応えますが、実機で確かめるのは、占有の中でだけ呼べる `quit` と合わせて段 6 で行います。

**各段の前に片付けるレビューの項目**（[設計のレビュー](../design-review.md)）:

| 段 | 項目 |
| --- | --- |
| 2 | なし（A2 の `ping`・A3・C1 の `ping` は済） |
| 3 | B2（Windows の PATH） |
| 4 | A1（種類 `app` と終了の依頼。済） |
| 5 | C1（読む道具の結果） |
| 6 | A2（`quit`）・A4（終了の依頼の最中の判定を残すか）・B1・C3（終了の処理中のタイマー） |

段 1 で決めたこと（真実は各ページ）:

- **`core/Json` の入れ子の深さの上限。** 64 を超えたら再帰を降りずに失敗させ、`core::Server::serve` は
  「読めない要求」（`invalid_request`）で応える。1 MiB の `[[[[…` で Vectorworks ごと落ちて、利用者の
  未保存の作業が失われないため（[通信規則「要求」](../protocol.md#要求idreqjson)・[ブリッジ「受け付け 1 回」](bridge.md#受け付け-1-回coreserverserve)）。
  JSON の読み方は Go の `encoding/json` に合わせる。
- **`core/Json` のファジング。** ASan・UBSan と libFuzzer（clang の `-fsanitize=fuzzer`）でビルドした
  入口（`tests/JsonFuzzer.cpp`）を、`cpp.yml` が決まった時間だけ回す。種は要求の見本
  （`protocol/fixtures/requests/`。深い入れ子・長い文字列・不正な UTF-8 を含む）（[ビルド「CI」](build-and-release.md#ci)）。
- **`protocol/fixtures/` の中身。** 通信規則の定数・id の見本・要求の見本・受け付け 1 回の見本（置いた
  ファイル → 残るファイルと応答）。C++ のテストと Go の `cli/internal/spool`・`cli/internal/fakeplugin`
  のテストが同じものを読む（[通信規則の見本](../fixtures.md)）。

その先（順序は未定）:

- **他のプラグインの機能を名前で呼ぶ**（`CallPluginLibrary`。形は[未決 3](open-questions.md#3-ほかのプラグインの機能の呼び出しの形)）。
- **図面に書く道具**（undo の手順・人の操作との混在の扱いを決めてから。[未決 1](open-questions.md)）。
- 構造設計支援プラグインの開発版ブリッジの移行（当面は並存）。

## 書き方

**1 から書きます。** ほかのプラグインのソースは移しません。SDK に関わるところは SDK リファレンスの
`Findings/` と実機確認プラグイン（`plugin/`）を根拠にし、見本のコードは写さずにこのリポジトリの
決まりで書きます（[プラグインの設計「根拠」](README.md#根拠)）。

| 作るもの | 根拠 |
| --- | --- |
| `src/core/*`（Protocol・Json・Files・Bridge・Serve） | [通信規則](../protocol.md)・[ブリッジ](bridge.md)。SDK に依らない |
| `src/ModuleMain.cpp`・`PluginPrefix.h`・`Module-Info.plist.in`・`resources/cli.vwr`・`CMakeLists.txt` | SDK リファレンスの `plugin/`（実機で読み込まれている形）・[Findings「Plug-in Modules」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Plug-in%20Modules.md)（VCOM の初期化・SDK の静的ライブラリをリンクするモジュールが定義するもの） |
| `src/Clock.{h,cpp}` | [Findings「Timers and Notifications」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Timers%20and%20Notifications.md) の 4・6・7（[構成](architecture.md#受け付けの流れ)） |
| `src/tools/*` | [SDK の調査 4](../sdk-research.md#4-最初の道具が使う-sdk-呼び出し) |
| `scripts/fetch-vw-sdk.sh` と CI の共通部 | SDK リファレンスの `scripts/`（`fetch-vw-sdk.sh`・`ci-common.sh`・`ci-wait.sh`） |
| `.github/workflows/*` | [ビルドとリリース](build-and-release.md) |
| 整形と検査の設定 | [整形](../format.md)・[検査](../lint.md) |

## 実機の確かめ方

このプラグインの実機確認は `vw2026` で行います。ローカルの
Claude Code セッションから `vw2026` を呼び、結果の JSON を読みます。図面に何かが起きたかは
人に確かめてもらいます。PR の確認は、その PR のプレリリースを入れて行います（同じ名前で入れ替わる）。

実機の確認は、**道具の結果（応答・図面への作用・描画）が想定どおりか**を確かめるためのものです。
ビルドが通ることと単体テスト・通信規則の見本の照合は CI が保証します（CI は main と合わせた
マージのためのコミットで走る）。そのため、PR のブランチを main に追いつかせることは求めません。
プレリリースは head でビルドするので（[ビルドとリリース「リリースの形」](build-and-release.md#リリースの形)）、
ブランチを作ったあとの main の変更が**道具の振る舞い・描画に影響しうる**と判断したときだけ、
ブランチに main を取り込んでから確かめます。

- **段 2**: `install` がまだ無いので、PR のプレリリースの zip を `curl -L` か
  `gh release download` で取ってきて、`bin/` 以外を `<Plug-Ins>/cli/` に置き、`vw2026` は zip の
  `bin/` のものを使います。ブラウザで取ると隔離属性（mac）や Web からの印（Windows）が付き、
  アドホック署名のモジュールと CLI が止められて、プラグインと関係のない理由で確認が失敗します
  （`install` はこれらが付かない前提。[インストール「前提」](install-and-update.md#前提)）。
  スプールの場所は CLI の置き場所に依らないので、どこに置いた CLI でも同じスプールを使います。
- **段 3**: PR のプレリリースの初回のスクリプトに `--tag dev-<slug>` を付けて入れます。
- **段 4 以降**: `vw2026 install --tag dev-<slug>` で入れます（Vectorworks を終了してから。段 6 の
  あとは `quit` で終了させてもよい）。戻すときは `vw2026 install`（main の最新）。

# 実装の順序

**1 段 1 PR**で、各段の終わりに確かめ方を決めておきます。実機でしか確かめられない段
（プラグイン・スクリプト）は、**利用者が実機で「確認できた」と言うまでマージしません**
（CI が通ることも、道具が応えることも、実機の確認の代わりにはならない）。

| 段 | 中身 | 確かめ方 | 実機 |
| --- | --- | --- | --- |
| 1 | **SDK に依らない共通部**: `core/Json`（入れ子の深さに上限を設ける）・`core/Bridge`・`core/Serve`（偽の道具の表・占有）・CMake（共通部・テスト・ファジングだけ）・`protocol/fixtures/`・`cpp.yml`（`core/Json` のファジングを含む）・`format.sh` / `lint.sh` に C++ と PowerShell を足す（[整形](../format.md)・[検査](../lint.md)）・`CLAUDE.md` の追記（PR の進め方） | CI（ASan・UBSan）。Go 側のテストも同じ見本を読む | 不要 |
| 2 | **骨格**: CMake にプラグインを足す・`BuildConfig`・`ModuleMain`・`Clock`・道具 `tools` / `ping` / `quit`・リソース・`build.yml`（ビルドと PR のプレリリースまで） | CI。実機で zip を手で置き、`vw2026 status` が `running:true` になり `call ping` が応える（**拡張機能なしで読み込まれることの確認を兼ねる**。[構成](architecture.md#拡張機能を登録しない)）。`session start` のあと `call quit` で保存の確認が出て、取り消すと `call ping` がまた応え、保存すると `status` が止まっていると答える | 要 |
| 3 | **読む道具**: `layers` / `classes` / `layer_objects` / `object_counts`（使う SDK 呼び出しが SDK リファレンスで確かめられてから。[SDK の調査 4](../sdk-research.md#4-最初の道具が使う-sdk-呼び出し)） | 実機で、レイヤ・クラス・オブジェクトの数と名前を用意した見本の図面に対して、結果が Vectorworks の画面（オーガナイザ・情報パレット）の表示と一致する | 要 |
| 4 | **配布**: 梱包（`bin/vw2026`）・main のリリース・`cleanup-dev-release.yml` | CI。リリースの資産を取ってきて、zip の形（直下に `build.json` と `bin/`）と `bin/vw2026 version` が動くことを確かめる | 不要 |
| 5 | **インストールと更新・アンインストール**: `vw2026 install` / `uninstall`（資産の取得・新しいビルドの判定・付け替え・CLI の入れ替え・PATH）・初回のスクリプト（`get-vw2026.sh` / `.ps1`）・`CLAUDE.md` に消すコードの歯止めを写す | CI（Go の単体テスト。偽の HTTP サーバー。動いている CLI の入れ替えを Windows で押さえる。shellcheck・PSScriptAnalyzer）。実機で、初回のスクリプトを curl からパイプで実行 → 新しい端末で `vw2026 status` が動き、起動して `call ping` が応える。動いている間は「Vectorworks が動いている」終了コードで何も変わらず、`session start` → `call quit` → 止まるのを待つ → `install` → `launch` → 動き出すのを待つ（待つのは `status` の繰り返し）で `call ping` の `version` が変わる。`--tag dev-<slug>` で PR のプレリリースに入れ替わり、`install` で main に戻る。`--plugins-dir` で既定以外へ入れられる。`uninstall` でリンクと PATH の項目だけが消える | 要 |

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

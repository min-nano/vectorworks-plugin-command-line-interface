# CLAUDE.md

このリポジトリで作業するときの共通の規約です。**この指示は既定の挙動より優先されます。**

## このリポジトリについて

Vectorworks 2026 を外部から操作するための**プラグイン（C++・VW SDK）と CLI（Go）**です。
目的・決めたこと・構成は `docs/design.md`、通信規則は `docs/protocol.md` が真実です。

## 守ること

1. **通信規則の真実は `docs/protocol.md` だけ。** プラグイン側と `cli/internal/spool` はその対。
   どちらかを変えたら文書と両方を直す。両側のテストが読む通信規則の見本（`protocol/fixtures`。
   `docs/fixtures.md`）も合わせて直す。`protocol` の版を上げるかは `docs/protocol.md「互換性」`
   に従う。
2. **CLI はプリミティブに保つ。** 再試行・占有を待つこと・道具ごとの解釈を入れない（呼ぶ側の
   責任）。排他はプラグインの占有（通信規則の「占有」。CLI は印を受け取って載せるだけ）だけで持ち、
   CLI に足さない（`docs/design.md「CLI はプリミティブに保つ」`）。
3. **プラグインは単純に保つ。** 配置と更新は CLI（`vw2026 install`）が Vectorworks の終了後に行う。
   プラグインのフォルダは CLI の場所と同じドライブにある前提で、その外の場合の扱いを足さない
   （`docs/design.md「決めたこと」`）。
4. **SDK 呼び出しはメインスレッドからだけ。** 受け付け用のスレッドや受け付けの中のループを
   書かない。
5. **本番で常駐する前提（`docs/design.md「本番で常駐させる前提」`）を崩さない。** 勝手に
   動く処理を足さない。図面に書く道具は undo の手順に従う。利用者の図面を保存せずに
   閉じる・消す道具を持たない。
6. **プラグインは 1 から書く。** ほかのプラグイン（構造設計支援など）のコードや決めごとを
   引き継がない。根拠にするのは [SDK リファレンス](https://github.com/min-nano/vectorworks-developer-sdk-reference)
   の確かめられた仕様と挙動（`Findings/`・実機確認プラグイン `plugin/`・`scripts/`）だけで、
   参考にしたときは文書にその節を示す（`docs/plugin/README.md「根拠」`）。
7. **SDK の調査はリファレンス側で行う。** 「この API はあるか・どう振る舞うか」が分からない
   まま実装に入らない。[SDK リファレンス](https://github.com/min-nano/vectorworks-developer-sdk-reference)で
   issue を立て、`Findings/` に反映されてから実装する。待っているものは `docs/sdk-research.md`。
8. **新しく分かったことは書き残す。** SDK の挙動は SDK リファレンスの `Findings/`、この
   プラグイン固有のことは `docs/` へ。

## 整形と検査

- **整形（`scripts/format.sh`）と検査（`scripts/lint.sh`）を分ける。** 整形は直すだけ、検査は
  指摘するだけ。`format.sh` は既定で確かめるだけで、`--fix` で書き換える。CI は `format.sh` と
  `lint.sh` を走らせる。push の前に両方を通す。
- 言語をまたぐ決まり（UTF-8・LF・タブ・100 桁の目安）は `.editorconfig` に
  だけ書く。各言語の設定には、その言語にしかない項目だけを書く（`docs/format.md`）。
- ツールは言語ごとに標準のもの 1 つを、ほぼ既定のまま使い、版を固定する。Go で書かれたツールは
  `tools/go.mod`、pip のツール（clang-format・clang-tidy）は `tools/requirements.txt`、
  PSScriptAnalyzer は `scripts/powershell.ps1` に版を置く。ツールと対象は `docs/format.md`・`docs/lint.md`。
- **コメントは対象のコードの直前の行に置き、コードと同じ行の末尾には書かない**（どの言語でも。
  1 行で足りるものも同じ。複数行と 1 行で置き場所が混ざると読みにくいため）。

## Go（`cli/`）

- 実行ファイルは 1 つにする（cgo を使わない）。
- **外部の依存は、信頼できるかを確かめてから足す。** 標準ライブラリで足りるならそちらを使う。
  足す前に次を確かめ、結果をコミットメッセージに書く。
  - 保守されているか（最近のリリース・issue への応答）と、広く使われているか
  - ライセンスが配布と両立するか（MIT・BSD・Apache-2.0 など）
  - 推移的な依存が少ないか（`go mod graph` で、実行ファイルに入るものを見る）
  - 既知の脆弱性が無いか（`govulncheck ./...`）
  - 版を固定し、`go.sum` をコミットする
- `tools/go.mod` のツール（整形と検査）も同じように確かめる。ただし実行ファイルに入らないので、
  推移的な依存の数は問わない。
- いま入っている依存は [kong](https://github.com/alecthomas/kong)（コマンドラインの解析。MIT。
  実行ファイルに入る推移的な依存なし）だけ。
- 受け渡しは偽のプラグインに対する単体テストで確かめる（`cli/internal/fakeplugin`）。
- **CLI の使い方（コマンド・指定・出力・終了コード）は、コマンドの定義に英語で書く。**
  1 コマンド 1 ファイル（`cli/cmd/vw2026/call.go` 等）の構造体で、kong のタグ（`help`）と
  `Help()`（詳しい説明）を `run` の隣に書く。全体の説明と終了コードは `main.go` の `cli.Help()`。
  真実は `vw2026 --help`（`vw2026 help <command>`）で、Markdown や godoc に別に書かない。
- **指定（フラグ）は、環境変数でも与えられるようにする**（kong の `env` タグ。名前は
  `VW2026_<コマンド>_<指定>`。複数のコマンドで同じ意味のもの（`VW2026_SPOOL`・`VW2026_SESSION`）は
  コマンド名を付けない）。両方あれば引数が勝つ。オン・オフの指定は `negatable:""` を付け、
  `--no-<指定>` で打ち消せるようにする。位置の引数（道具名・引数の JSON）は対象外。
  付け忘れは `TestEveryFlagHasEnv` が落とす。
- コメントは日本語でも英語でもよく、**なぜ**を書く。CLI の出力・エラーの文言は英語（機械が
  読む・端末の文字コードに依らない）。

## C++（`src/`）

- **SDK に依らない部分は `src/core` に置き、無 SDK の単体テスト（`tests/`）で確かめる。** 受け付けの
  手順・JSON・スプールの OS の呼び出しはここ。SDK を呼ぶのは `src/core` の外だけ。
- **通信規則に関わる振る舞いは通信規則の見本（`protocol/fixtures`。`docs/fixtures.md`）に置く。** C++ と Go の
  両方のテストが同じ見本を読むので、片側だけを直すと落ちる。見本で表しにくいことだけを、各言語の
  テストに書く。
- 手元のテスト（CI の `test.yml` と同じ指定）:
  `cmake -S . -B build -DVW_BUILD_TESTS=ON -DVW_ENABLE_SANITIZERS=ON -DCMAKE_COMPILE_WARNING_AS_ERROR=ON -DCMAKE_BUILD_TYPE=Debug && cmake --build build && ctest --test-dir build --output-on-failure`。
  ファジングは clang（Apple の clang は不可）で `-DVW_BUILD_FUZZERS=ON` を足し、
  `mkdir -p corpus crashes && build/JsonFuzzer -max_total_time=120 -artifact_prefix=crashes/ corpus protocol/fixtures/requests`
  で回す（libFuzzer は見つけた入力を最初に渡したディレクトリへ書くので、空のコーパスを先に渡し、
  見本のディレクトリを汚さない）。
- 外部のライブラリ（テストの枠組みを含む）は足さない。標準ライブラリと OS の呼び出しで書く。

## 文書

- 日本語。1 ページ 1 トピック。技術文書の語で書き、くだけた言い回し・比喩を使わない。
- 決めたことの根拠が SDK の挙動なら、SDK リファレンスの `Findings/` の節へリンクする。
  経緯や実測の数値はここへ写さない。

## 進め方

- `main` へは直接 push せず、ブランチを切って PR で進める。
- コミットメッセージには Claude セッションの URL を入れる。
- **プラグインは `docs/plugin/plan.md` の段ごとに 1 PR で進める。** PR には、その段の確かめ方と、
  確かめた結果を書く。
- **実機でしか確かめられない段（プラグイン・スクリプト）は、利用者が実機で「確認できた」と言うまで
  マージしない。** CI が通ることも、道具が応えることも、実機の確認の代わりにはならない。実機の
  確かめ方は `docs/plugin/plan.md「実機の確かめ方」`（PR のプレリリースを入れ、`vw2026` で呼ぶ）。

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
- 受け渡しは偽のプラグインに対する単体テストで確かめる（`cli/internal/fakeplugin`）。スプールの
  場所は `env.spool` を差し替えて渡し、場所を変える指定（引数・環境変数）は足さない
  （プラグインは `<CLI>/spool` しか見ないので、本番で効かない。`docs/protocol.md「スプールの場所」`）。
- 手書きの字句処理（`spool.NestingDepth`）は Go の標準のファジングで、`encoding/json` と同じ深さを
  返すことを確かめる（`cli/internal/spool/fuzz_test.go`。任意のバイト列の `FuzzNestingDepth` と、
  `json.Marshal` した要求の `FuzzNestingDepthOfRequest`）。プラグインとの一致は通信規則の見本で確かめる。
  `go test` は種だけを走らせ、CI の `go.yml` が 30 秒ずつ回す。手元では `cli/` で
  `go test -run '^$' -fuzz '^FuzzNestingDepth$' -fuzztime 30s ./internal/spool`（対象は 1 回に 1 つ）。
  落ちた入力は `internal/spool/testdata/fuzz/` に書かれ、`go test -run` で再現できる。JSON の解析
  そのものは `encoding/json` に任せていて、ファジングしない。
- **CLI の使い方（コマンド・指定・出力・終了コード）は、コマンドの定義に英語で書く。**
  1 コマンド 1 ファイル（`cli/cmd/vw2026/call.go` 等）の構造体で、kong のタグ（`help`）と
  `Help()`（詳しい説明）を `run` の隣に書く。全体の説明と終了コードは `main.go` の `cli.Help()`。
  真実は `vw2026 --help`（`vw2026 help <command>`）で、Markdown や godoc に別に書かない。
- **指定（フラグ）は、環境変数でも与えられるようにする**（kong の `env` タグ）。名前は、使う
  コマンドではなく**値の通じる範囲**で決める。両方あれば引数が勝つ。オン・オフの指定は
  `negatable:""` を付け、`--no-<指定>` で打ち消せるようにする。位置の引数（道具名・引数の JSON）は
  対象外。付け忘れは `TestEveryFlagHasEnv` が落とす。
  - どのコマンドからも同じものを指す値（占有の印・Vectorworks の場所・リリースのタグ・プラグインの
    フォルダ）は `VW2026_<指定>`（`VW2026_SESSION`・`VW2026_APP`・`VW2026_TAG`・`VW2026_PLUGINS_DIR`）。いま 1 つのコマンドだけが読むものも含む。この名前は
    `VW2026_<コマンド>_` で始めない（コマンドごとの名前と重ならないように。`TestEnvNamesDoNotCollide`）。
  - それ以外（待つ上限・出力の形のオン・オフなど、コマンドごとに違う値を与えうるもの）は
    `VW2026_<コマンド>_<指定>`（`VW2026_CALL_TIMEOUT`・`VW2026_CALL_RAW`）。`<コマンド>` は値を共有する
    コマンドの範囲で決める。いくつかのコマンドが同じ値を共有するなら、それらをまとめる名前を使い
    （`session start` と `session end` の `VW2026_SESSION_TIMEOUT`）、1 つのコマンドだけのものなら
    そのコマンドの名前を使う（`session end` だけなら `VW2026_SESSION_END_<指定>`）。共通の親を
    持たないコマンド（`call` と `session start` など）は値を共有せず、コマンドごとに名前を付ける。
  - どちらか迷うものはコマンドごとにする。全体の名前は、どのコマンドでも同じ意味だと言えるときだけ
    付ける。
- コメントは日本語でも英語でもよく、**なぜ**を書く。CLI の出力・エラーの文言は英語（機械が
  読む・端末の文字コードに依らない）。

## C++（`src/`）

- **SDK に依らない部分は `src/core` に置き、無 SDK の単体テスト（`tests/`）で確かめる。** 受け付けの
  手順・JSON・スプールの OS の呼び出しはここ。SDK を呼ぶのは `src/core` の外だけ。
- **通信規則に関わる振る舞いは通信規則の見本（`protocol/fixtures`。`docs/fixtures.md`）に置く。** C++ と Go の
  両方のテストが同じ見本を読むので、片側だけを直すと落ちる。見本で表しにくいことだけを、各言語の
  テストに書く。
- 手元のテスト（CI の `cpp.yml` と同じ指定）:
  `cmake -S . -B build -DVW_BUILD_PLUGIN=OFF -DVW_BUILD_TESTS=ON -DVW_ENABLE_SANITIZERS=ON -DCMAKE_COMPILE_WARNING_AS_ERROR=ON -DCMAKE_BUILD_TYPE=Debug && cmake --build build && ctest --test-dir build --output-on-failure`。
  ファジングは clang（Apple の clang は不可）で `-DVW_BUILD_FUZZERS=ON` を足し、
  `mkdir -p corpus crashes && build/JsonFuzzer -max_total_time=120 -artifact_prefix=crashes/ corpus protocol/fixtures/requests`
  で回す（libFuzzer は見つけた入力を最初に渡したディレクトリへ書くので、空のコーパスを先に渡し、
  見本のディレクトリを汚さない）。
  カバレッジは clang（Apple の clang も可）で `-DVW_ENABLE_COVERAGE=ON` を足してテストを走らせ、
  `llvm-profdata merge -sparse build/coverage/*.profraw -o build/coverage.profdata && llvm-cov report build/VwCliTests -instr-profile=build/coverage.profdata src/core`
  で見る（macOS は `xcrun` を前に付ける。CI は同じものを lcov にして Codecov へ送る）。
  `llvm-profdata`・`llvm-cov` はビルドに使った clang と同じ版を使う（版が違うと結果を読めない
  ことがある。Linux では `llvm-profdata-18` のように版の付いた名前で入っていることが多い）。
  Windows（MSVC）は `-DVW_ENABLE_COVERAGE=ON`（`/PROFILE` を付けてリンクする。
  `VW_ENABLE_SANITIZERS` とは併用できない）でビルドし、Developer PowerShell で
  `Remove-Item build/Debug/VwCliTests.exe -ErrorAction Ignore; cmake --build build --config Debug; Microsoft.CodeCoverage.Console instrument --settings tests/MsvcCoverage.xml build/Debug/VwCliTests.exe; Microsoft.CodeCoverage.Console collect --settings tests/MsvcCoverage.xml -f cobertura -o build/coverage.cobertura.xml ctest --test-dir build --build-config Debug --output-on-failure`
  で集める（Visual Studio に付属。2022 は Enterprise だけ）。計装は実行ファイルをその場で
  書き換え、ソースが変わらなければ `cmake --build` は再リンクしないので、毎回 exe を消して
  リンクし直してから計装する（計装済みのものを重ねて計装しない）。
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
  確かめ方は `docs/plugin/plan.md「実機の確かめ方」`（PR のプレリリースを入れ、`vw2026` で呼ぶ。`install` が無い段 2 は zip を手で置く）。

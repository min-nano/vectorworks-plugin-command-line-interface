# CLAUDE.md

本リポジトリで作業する際の共通の規約です。**この指示は既定の挙動より優先されます。**

## 本リポジトリの概要

Vectorworks 2026 を外部から操作するための**プラグイン（C++・VW SDK）と CLI（Go）**です。
目的・決定事項・構成は `docs/design.md`、通信規則は `docs/protocol.md`、`vw2026 install` が読み込む
レジストリとマニフェストの記法は `docs/registry.md` が真実です。

## 遵守事項

1. **通信規則の真実は `docs/protocol.md` のみ。** プラグイン側と `cli/internal/spool` はその対。
   いずれかを変更した場合は、文書と両方を修正する。両側のテストが読み込む通信規則のフィクスチャ
   （`protocol/fixtures`。`docs/fixtures.md`）も併せて修正する。`protocol` の版を上げるか否かは
   `docs/protocol.md「互換性」`に従う。
2. **CLI はプリミティブに保つ。** 再試行・占有の待機・ツールごとの解釈を組み込まない（呼び出し元の
   責任）。排他はプラグインの占有（通信規則の「占有」。CLI はセッション ID を受け取って付与するのみ）でのみ
   実現し、CLI に追加しない（`docs/design.md「CLI のプリミティブ性の維持」`）。
3. **プラグインは単純に保つ。** 配置と更新は CLI（`vw2026 install`）が Vectorworks の終了後に行う。
   プラグインのフォルダは `vw2026` の作業領域（利用者ごとの一時フォルダの下）と同一のドライブにある前提とし、それ以外の場合の処理を追加しない
   （`docs/design.md「決定事項」`）。プラグインのフォルダの外に配置するもの（PATH）は `cli` のフック
   （`scripts/hooks/`）のみが配置し、`install` の Go のコード（`cli/internal/install`）は `cli` を特別に扱わない
   （`cli` の名前を使用するのは、ロックの配置場所と、他のプラグインを `cli` のある `<Plug-Ins>` にのみインストールする検証と、インストールされた CLI が自身のインストール先の `<Plug-Ins>` を導出することのみ）。
4. **SDK 呼び出しはメインスレッドからのみ。** 受付処理用のスレッドや、受付処理の中で非同期の
   処理の完了を待機するループを記述しない（`docs/protocol.md「予約ツール」`）。
5. **本番環境で常駐する前提（`docs/design.md「本番環境で常駐させる前提」`）を崩さない。** 自律的に
   動作する処理を追加しない。書き込みツールは undo の手順に従う。利用者が開いた図面とダイアログも、
   呼び出し元が指定すれば操作の対象とする（保存せずに閉じること・ダイアログのいずれのボタンを押すことも含む）。
   プラグインと CLI が独自の判断では行わない。図面のファイルは削除しない（削除するコードは 9 に従う）。
6. **プラグインは 1 から記述する。** 他のプラグインのコードや取り決めを引き継がない。
   根拠とするのは [SDK リファレンス](https://github.com/min-nano/vectorworks-developer-sdk-reference)
   の検証された仕様と挙動（`Findings/`・実機確認プラグイン `plugin/`・`scripts/`）のみであり、
   参考にした場合は文書にその節を示す（`docs/plugin/README.md「根拠」`）。
7. **SDK の調査はリファレンス側で行う。** 「この API は存在するか・どのように振る舞うか」が不明な
   まま実装に着手しない。[SDK リファレンス](https://github.com/min-nano/vectorworks-developer-sdk-reference)で
   issue を作成し、`Findings/` に反映されてから実装する。反映を待機しているものは、本リポジトリの issue で追跡する。
   利用者の判断で調査しないと決定した事項（ネットワーク上の場所でサーバーに到達できない場合に停止すること、保存の途中で接続が切断された場合に
   ダイアログが表示されるか）は例外とし、
   `docs/sdk-research.md` に記述する。
   6 と 7 が対象とするのは SDK（Vectorworks）の振る舞いであり、OS の呼び出しの振る舞い（返すエラーの番号・名前の長さの
   上限など）は対象外とする。OS の振る舞いに依存する箇所は、mac と Windows の `VwCliTests` が実際の呼び出しで検証し、CI の環境で再現できない場面
   （ネットワークの共有・名前の解決など）は、その変更の実機での確認（`docs/plugin/verification.md`）で確認する。
8. **新たに判明したことは記録する。** SDK の挙動は SDK リファレンスの `Findings/`、本
   プラグイン固有のことは `docs/` へ。
9. **利用者のものを削除するコードは、次の箇所のみとし、制約を緩めない**（`docs/plugin/install-and-update.md「削除処理の制約」`）。

   | 削除するコード | 削除してよいもの |
   | --- | --- |
   | `install`（構築・後処理・フックの失敗時の巻き戻し） | `<Work>/staging/<name>/`・`<Work>/staging/<name>.zip`（いずれも名前をそのまま用いて構築したパスのみ。前方一致では削除しない）・`<Work>/old/<name>/`（退避した版と前回の残存物は、記録の `name` が一致し、記録の `module` が存在し、`<Plug-Ins>/<name>/` も存在する場合のみ。`<Work>/staging/<name>/` へ移動してから削除する。条件に合致しないものは削除せずに `<Work>/old/<name>.kept-<時刻>/` へ移動する）・フックが失敗した場合の新しい `<Plug-Ins>/<name>/`（自身が配置したもの）・`<Work>/old/` の直下の `vw2026.old-*` / `vw2026.exe.old-*`（ファイルのみ。退避した CLI と前回までの残存物）・中断した `uninstall` の標識 `<Work>/old/<name>.uninstalling`（差し替えに成功した後と、`<Plug-Ins>/<name>/` が存在する場合。いずれも以前の回の残存物） |
   | `uninstall` | 記録の `name` が一致し、記録の `module` が存在する `<Plug-Ins>/<name>/`（`<Work>/old/<name>/` へ移動し、フックの後で `<Work>/staging/<name>/` へ移動してから削除する）・`<Work>/staging/<name>/`・`<Work>/staging/<name>.zip`（いずれも名前をそのまま用いて構築したパスのみ）・自身が書き込んだ標識 `<Work>/old/<name>.uninstalling`・`<Plug-Ins>/<name>/` も `<Work>/old/<name>/` も存在しない場合の以前の回の標識 `<Work>/old/<name>.uninstalling`・`<Work>/old/` の直下の `vw2026.old-*` / `vw2026.exe.old-*`（ファイルのみ）・空の `<Work>/staging`・`<Work>/old`・`<Work>`（内容のあるものは削除しない） |
   | `cli` の `install` のフック | なし |
   | `cli` の `uninstall` のフック | `<Plug-Ins>/cli/bin/vw2026` を指す `~/.local/bin/vw2026`・`Path` の `<Plug-Ins>\cli\bin` と一致する項目（いずれも `VW2026_HOOK_INSTALL_DIR` から決定し、同一の場所か否かはディレクトリの同一性で判定する） |

   2025 の `cli`（`vw2025`）では、表の名前を `docs/plugin/editions.md` の表に従って読み替える（`vw2025.old-*`・
   `~/.local/bin/vw2025`・`VW2025_HOOK_INSTALL_DIR` など）。削除してよいものの範囲は同一。

## 整形と検査

- **整形（`scripts/format.sh`）と検査（`scripts/lint.sh`）を分離する。** 整形は修正のみ、検査は
  指摘のみを行う。`format.sh` は既定では確認のみを行い、`--fix` で書き換える。CI は `format.sh` と
  `lint.sh` を実行する。push の前に両方を通過させる。
- 言語共通の規則（UTF-8・LF・タブ・100 桁の目安）は `.editorconfig` に
  のみ記述する。各言語の設定には、その言語固有の項目のみを記述する（`docs/format.md`）。
- ツールは言語ごとに標準のもの 1 つを、ほぼ既定の設定のまま使用し、版を固定する。Go で記述されたツールは
  `tools/go.mod`、pip のツール（clang-format・clang-tidy・shellcheck・ruff）は `tools/requirements.txt`、
  PSScriptAnalyzer と Pester（`cli` のフックのテスト。`hooks.yml`）は `scripts/powershell.ps1` に版を記述する。ツールと対象は `docs/format.md`・`docs/lint.md`。
- **コメントは対象のコードの直前の行に配置し、コードと同じ行の末尾には記述しない**（全ての言語で同様。
  1 行で足りるものも同様。複数行と 1 行で配置場所が混在すると読みにくいため）。

## Go（`cli/`）

- 実行ファイルは 1 つとする（cgo を使用しない）。
- **外部の依存は、信頼性を確認してから追加する。** 標準ライブラリで足りる場合はそちらを使用する。
  追加する前に次を確認し、結果をコミットメッセージに記述する。
  - 保守されているか（最近のリリース・issue への応答）と、広く使用されているか
  - ライセンスが配布と両立するか（MIT・BSD・Apache-2.0 など）
  - 推移的な依存が少ないか（`go mod graph` で、実行ファイルに含まれるものを確認する）
  - 既知の脆弱性が存在しないか（`govulncheck ./...`）
  - 版を固定し、`go.sum` をコミットする
- `tools/go.mod` のツール（整形と検査）も同様に確認する。但し実行ファイルに含まれないため、
  推移的な依存の数は問わない。
- 現在導入されている依存は [kong](https://github.com/alecthomas/kong)（コマンドラインの解析。MIT。
  実行ファイルに含まれる推移的な依存なし）のみ。
- 受け渡しは擬似プラグインに対する単体テストで検証する（`cli/internal/fakeplugin`）。スプールの
  場所は、本番環境では実行ファイルの場所から導出し（`spool.Dir`）、テストでは `env.spool` を差し替えて渡す。
  `install` / `uninstall` の場所も同様で、本番環境では `main()` のみが `env.workDir`（`install.WorkDir`）・
  `env.pluginsDir`（`install.DefaultPluginsDir`）・`env.executable`（`os.Executable`）を設定し、テストの
  `invoke` はいずれも既定で一時ディレクトリを返す（開発者の計算機の実際の `<Plug-Ins>`・`<Work>` に触れない）。
  配置の手順は `cli/internal/install` にあり、場所と既定のレジストリを `install.Options` で受け取り、擬似
  HTTPS サーバー（`httptest`）と、テストの実行ファイルを擬似フックとして実行する `Options.Interpreters` で検証する。
  場所を変更する指定（引数・環境変数）は追加しない（プラグインは自身のモジュールの場所から導出した
  `<Plug-Ins>/cli/spool` のみを参照するため、本番環境では効果がない。`docs/protocol.md「スプールの場所」`）。
- 手書きの字句処理（`spool.NestingDepth`）は Go の標準のファジングで、`encoding/json` と同じ深さを
  返すことを検証する（`cli/internal/spool/fuzz_test.go`。任意のバイト列の `FuzzNestingDepth` と、
  `json.Marshal` した要求の `FuzzNestingDepthOfRequest`）。プラグインとの一致は通信規則のフィクスチャで検証する。
  `go test` はシードのみを実行し、CI の `go.yml` が 30 秒ずつ実行する。ローカル環境では `cli/` で
  `go test -run '^$' -fuzz '^FuzzNestingDepth$' -fuzztime 30s ./internal/spool`（対象は 1 回に 1 つ）。
  失敗を引き起こした入力は `internal/spool/testdata/fuzz/` に書き込まれ、`go test -run` で再現できる。JSON の解析
  そのものは `encoding/json` に委ねており、ファジングの対象としない。
- **CLI の使用方法（コマンド・指定・出力・終了コード）は、コマンドの定義に英語で記述する。**
  1 コマンド 1 ファイル（`cli/cmd/vw2026/call.go` など）の構造体で、kong のタグ（`help`）と
  `Help()`（詳細な説明）を `run` の隣に記述する。全体の説明と終了コードは `main.go` の `cli.Help()`。
  真実は `vw2026 --help`（`vw2026 help <command>`）であり、Markdown や godoc に別途記述しない。
- **指定（フラグ）は、環境変数でも与えられるようにする**（kong の `env` タグ）。名前は、使用する
  コマンドではなく**値の通用する範囲**で決定する。両方ある場合は引数が優先される。オン・オフの指定には
  `negatable:""` を付け、`--no-<指定>` で打ち消せるようにする。位置引数（ツール名・引数の JSON）は
  対象外。付け忘れは `TestEveryFlagHasEnv` が検出して失敗させる。但し、初回スクリプトのみが一時フォルダの CLI に渡す
  隠し指定（`install` / `uninstall` の `--plugins-dir`。kong の `hidden`）は環境変数を持たない（持つと、インストール
  された CLI にも作用する。インストールされた CLI は自身のインストール先の `<Plug-Ins>` を使用し、この指定を拒否する）。
  - いずれのコマンドからも同じものを指す値（セッション ID・Vectorworks の場所・レジストリ）は `VW2026_<指定>`
    （`VW2026_SESSION`・`VW2026_APP`・`VW2026_REGISTRY`）。現在 1 つのコマンドのみが読み取るものも含む。この名前は
    `VW2026_<コマンド>_` で始めない（コマンドごとの名前と重複しないように。`TestEnvNamesDoNotCollide`）。
  - それ以外（待機の上限・出力形式のオン・オフなど、コマンドごとに異なる値を与えうるもの）は
    `VW2026_<コマンド>_<指定>`（`VW2026_CALL_TIMEOUT`・`VW2026_CALL_RAW`）。`<コマンド>` は値を共有する
    コマンドの範囲で決定する。複数のコマンドが同じ値を共有する場合は、それらを包括する名前を使用し
    （`session start` と `session end` の `VW2026_SESSION_TIMEOUT`）、1 つのコマンドのみのものは
    そのコマンドの名前を使用する（`session end` のみならば `VW2026_SESSION_END_<指定>`）。共通の親を
    持たないコマンド（`call` と `session start` など）は値を共有せず、コマンドごとに名前を付ける。
  - いずれとするか判断に迷うものはコマンドごとにする。全体の名前は、全てのコマンドで同じ意味であると
    言える場合のみ付ける。
- コメントは日本語でも英語でもよく、**理由**を記述する。CLI の出力・エラーの文言は英語（機械が
  読み取る・端末の文字コードに依存しない）。

## C++（`src/`）

- **SDK に依存しない部分は `src/core` に配置し、無 SDK の単体テスト（`tests/`）で検証する。** 受付処理の
  手順・JSON・スプールの OS の呼び出しはここに含める。SDK を呼び出すのは `src/core` の外のみ。
- **通信規則に関わる振る舞いは通信規則のフィクスチャ（`protocol/fixtures`。`docs/fixtures.md`）に記述する。** C++ と Go の
  両方のテストが同一のフィクスチャを読み込むため、片側のみを修正すると失敗する。フィクスチャで表現しにくい事項のみを、
  各言語のテストに記述する。
  ツールの結果の規則（実数の表記）は通信規則ではないため `tests/fixtures/` に配置し、C++
  （`tests/NumberTests.cpp`）と Go（`cli/internal/toolfixtures`）の両方のテストが読み込む。
- ローカル環境（mac）でプラグインをビルドする場合は、プリセット `vw2026` を使用する
  （`docs/plugin/build-and-release.md`「ローカル環境でのビルド」）。
- ローカル環境でのテスト（CI の `cpp.yml` と同じ指定）:
  `cmake -S . -B build -DVW_BUILD_PLUGIN=OFF -DVW_BUILD_TESTS=ON -DVW_ENABLE_SANITIZERS=ON -DCMAKE_COMPILE_WARNING_AS_ERROR=ON -DCMAKE_BUILD_TYPE=Debug && cmake --build build && ctest --test-dir build --output-on-failure`。
  ファジングは clang（Apple の clang は不可）で `-DVW_BUILD_FUZZERS=ON` を追加し、
  `mkdir -p corpus crashes && build/JsonFuzzer -max_total_time=120 -artifact_prefix=crashes/ corpus protocol/fixtures/requests`
  で実行する（libFuzzer は発見した入力を最初に渡したディレクトリへ書き込むため、空のコーパスを先に渡し、
  フィクスチャのディレクトリを汚染しない）。
  カバレッジは clang（Apple の clang も可）で `-DVW_ENABLE_COVERAGE=ON` を追加してテストを実行し、
  `llvm-profdata merge -sparse build/coverage/*.profraw -o build/coverage.profdata && llvm-cov report build/VwCliTests -instr-profile=build/coverage.profdata src/core`
  で確認する（mac は `xcrun` を前に付ける。CI は同じものを lcov 形式に変換して Codecov へ送信する）。
  `llvm-profdata`・`llvm-cov` はビルドに使用した clang と同じ版を使用する（版が異なると結果を読み取れない
  ことがある。Linux では `llvm-profdata-18` のように版の付いた名前でインストールされていることが多い）。
  Windows（MSVC）は `-DVW_ENABLE_COVERAGE=ON`（`/PROFILE` を付けてリンクする。
  `VW_ENABLE_SANITIZERS` とは併用できない）でビルドし、Developer PowerShell で
  `Remove-Item build/Debug/VwCliTests.exe -ErrorAction Ignore; cmake --build build --config Debug; Microsoft.CodeCoverage.Console instrument --settings tests/MsvcCoverage.xml build/Debug/VwCliTests.exe; Microsoft.CodeCoverage.Console collect --settings tests/MsvcCoverage.xml -f cobertura -o build/coverage.cobertura.xml ctest --test-dir build --build-config Debug --output-on-failure`
  で収集する（Visual Studio に付属。2022 は Enterprise のみ）。計装は実行ファイルをその場で
  書き換え、ソースが変更されなければ `cmake --build` は再リンクしないため、毎回 exe を削除して
  リンクし直してから計装する（計装済みのものを重ねて計装しない）。
- 外部のライブラリ（テストフレームワークを含む）は追加しない。標準ライブラリと OS の呼び出しで記述する。

## 文書

- 日本語。1 ページ 1 トピック。技術文書の語で記述し、くだけた言い回し・比喩を使用しない。
- 漢字で書ける語は漢字で表記する（他・全て・今・後・通り・間など。意味を読み取りやすくするため）。一般的でない
  当て字は使用しない。形式名詞（こと・とき・もの・ところ）と補助的な語（ている・など・まま）は仮名で表記する。
  コードの注釈も同様。
- 和語より漢語を主体とし、フォーマルな技術文書の表現で記述する。用語は既存の文書の訳語（ツール・フィクスチャ・
  呼び出し元・セッション ID・標識など）に統一する。
- 決定事項の根拠が SDK の挙動であれば、SDK リファレンスの `Findings/` の節へリンクする。
  経緯や実測の数値はここへ転記しない。
- `README.md` は、コードブロックを参照するだけで概ねの使用方法を想像できるように記述する。実行する
  コマンドと、その結果・次に実行するコマンドを同じブロックに配置し、結果はコメントで示す。
  結果のコメントはコマンドの次の行に配置する（「整形と検査」のコメントの配置場所の規則の例外）。

## 作業の進行

- `main` へは直接 push せず、ブランチを作成して PR で進める。
- コミットメッセージには Claude セッションの URL を記載する。
- **プラグインは、まとまり（ツール・機能）単位で 1 PR として進める。** 今後作成するものは issue で追跡する。
  PR には、検証方法と、検証の結果（実機で確認できなかった項目を含む）を記述する。
- **PR は下書き（draft）で作成する。下書きの PR をレビュー準備完了の状態に変更する前に、
  必ずレビューのスキル（`/code-review`）を使用し、マージの妨げとなる指摘が無いことを確認する。**
  妨げとなるのは `important` の指摘と、`severity` の無い指摘であり、`pre_existing` と `nit` は
  妨げとしない。妨げとなる指摘があれば修正してから再度レビューし、それが無くなるまで下書きのままにする。
  レビュー準備完了の状態の PR に push する場合も、push の前に同様にレビューし、妨げとなる
  指摘が無いことを確認してから push する。
- レビュー準備完了の状態の PR は、`review.yml` が自動でレビューし、判定（Approve / Request changes）を付与する
  （`docs/review.md`）。自動レビューは `/code-review` の代替としない。
- **実機でのみ検証できる変更（プラグイン・スクリプト）は、利用者が実機で「確認できた」と述べるまで
  マージしない。** CI を通過することも、ツールが応答することも、実機での確認の代替にはならない。実機での
  検証方法は `docs/plugin/verification.md`（PR のプレリリースをインストールし、`vw2026` で呼び出す）。

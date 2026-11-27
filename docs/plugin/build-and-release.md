# ビルドとリリース

SDK のリンクとバンドルの形は、SDK リファレンスの実機確認プラグイン（[`plugin/`](https://github.com/min-nano/vectorworks-developer-sdk-reference/tree/main/plugin)）の `CMakeLists.txt` で実機に読み込まれている形を見本にします（[プラグインの設計「根拠」](README.md#根拠)）。

## CMake

| 項目 | 内容 |
| --- | --- |
| プロジェクト | `project(VwCli CXX)`（Apple では OBJCXX も）。CMake は 3.24 以上（`cmake_minimum_required(VERSION 3.24)`。[CMake の版](#cmake-の版)） |
| 選択肢 | `VW_BUILD_PLUGIN`（ON）・`VW_BUILD_TESTS`（OFF）・`VW_BUILD_FUZZERS`（OFF。clang の libFuzzer が要る）・`VW_ENABLE_SANITIZERS`（OFF。ASan と UBSan。MSVC は ASan だけ）・`VW_ENABLE_COVERAGE`（OFF。clang は source-based coverage、MSVC は `/PROFILE` を付けてリンクし Visual Studio の `Microsoft.CodeCoverage.Console` で測る。clang-cl は不可）・`VW_ENABLE_PCH`（ON）。`VW_BUILD_PLUGIN` が ON で `VW_SDK_DIR` が無ければ構成で止める（黙ってプラグインを外さない）。SDK の無いところ（テスト・検査・ファジング。ubuntu を含む）は `-DVW_BUILD_PLUGIN=OFF` を付ける |
| SDK | `VW_SDK_DIR`（`SDKLib` を含む）。mac は `libVWSDK.a`＋VWMM・`BuildVWR`、Windows は x64 だけ（`VWSDK.lib`・`VWMM.lib`・`buildvwr.exe`）。どれかが無ければ構成で止める。配るプラグイン（CI）は Release でビルドする（`RELEASE_BLD`。Windows は `/MD`。SDK の Release のライブラリに合わせる。どちらも構成によらず付くので、[手元のビルド](#手元のビルド)の `RelWithDebInfo` でも SDK とずれない） |
| 警告 | `/W4 /permissive-`・`-Wall -Wextra -Wpedantic` は SDK に依らないターゲット（共通部・テスト・ファジング）にだけ付ける。SDK のヘッダは `SYSTEM` で読み、`/permissive-` を通る保証が無いのでプラグインには付けない |
| 共通部 | `VwCliCore`（STATIC。`src/core/*`。SDK に依らない。プラグインとテストがリンク。OS ごとの呼び出しは `FilesPosix.cpp` / `FilesWindows.cpp` の片方と、保存先をファイルシステムに聞く包みの `SaveTargetPosix.cpp` / `SaveTargetWindows.cpp` の片方、Windows だけの `WindowsPath.cpp`（スプールと保存先の確かめが共有する `extended()`）、パスを比べる包みの `PathCompareMac.cpp`（`-framework CoreFoundation` をリンク）/ `PathCompareWindows.cpp`（`Normaliz.lib` をリンク）の片方。Linux ではパスを比べる包みを持たない。[保存先の確かめ「実装とテストの分け方」](save-target.md#実装とテストの分け方)） |
| テスト | `VwCliTests`（`tests/*Tests.cpp`。外部のテストの枠組みに依らない）・`JsonFuzzer`（`tests/JsonFuzzer.cpp`）。テストは `protocol/fixtures` を読む |
| プラグイン | `add_library(<target> MODULE)`。`VW_PLUGIN_SOURCES`（`src/*.cpp`・`src/tools/*`） |
| 刻印 | `VW_BUILD_VERSION`（短い sha）・`VW_BUILD_BRANCH`。PR では、ブランチの head の sha とブランチの名前（[リリースの形](#リリースの形)）。CI では `meta` が決め、ビルドのジョブがその出力を `-D` で渡す。CMake は構成のたびに `<build>/generated/BuildStamp.h` へ C の文字列として書き（`"` と `\` を逃がす）、値をキャッシュに残さない（`-D` を付けずに構成し直せば `local` に戻る。古い sha が残らないように）。`ping` の結果に載る。ファイルとしての版の情報は `build.json` だけ（下記） |
| mac | `Module-Info.plist.in`（版の鍵は持たない）・`.vwlibrary` バンドル・リソースは `Contents/Resources/<name>.vwr`。arm64 だけ（`CMAKE_OSX_ARCHITECTURES`。SDK リファレンスの `plugin/` が実機で読み込まれている形に合わせ、確かめていない形を配らない。CLI は universal。Intel の mac（`macos-x64`）は、実機で確かめる環境が無いので対応を見送る）。配備の下限は macOS 14。ソースは Objective-C++ としてコンパイルする（SDK の mac のヘッダが CoreFoundation などを読むため） |
| Windows | `.vlb`・隣に `.vwr` |
| ターゲット | `add_vw_plugin(VwCli "cli" io.github.min-nano.cli)` の 1 つだけ（安定版と開発版を区別しない） |

`add_vw_plugin` は、見本にある殻と本体の分割・同梱スクリプト・版のファイル（plist の版の鍵など）を
持ちません。版の情報が `build.json` とコンパイル時の定義の 2 つ以外に散らばらないようにするためです。

### CMake の版

`cmake_minimum_required(VERSION 3.24)` にしています。CI と手元のテストが警告をエラーにする
`CMAKE_COMPILE_WARNING_AS_ERROR` は CMake 3.24 から効く変数で、3.20〜3.23 では黙って無視され、
警告があってもビルドが通るためです。3.23 以下で構成すると、最初の行で止まります。

最小版はポリシーの既定も決めます。3.21〜3.24 で入ったポリシー（CMP0121〜CMP0139）のうち、
`CMakeLists.txt` が使う書き方に関わるのは次のものだけで、どれも構成の結果を変えません。
ほかは `CMakeLists.txt` が使わないコマンドとモジュールと書き方（`list()`・`find_*()`・`while()`・
`cmake_dependent_option()`・project の形の `try_compile()`・`if()` の `PATH_EQUAL`・`ExternalProject`・
`CheckIPOSupported`・`CPack`・`UseSWIG` など）か、ほかのコンパイラー（ARMClang・MCST LCC・Watcom）のものです。

| ポリシー | NEW の振る舞い | このリポジトリ |
| --- | --- | --- |
| CMP0124（3.21） | `foreach()` のループ変数をループの外へ残さない | ループ変数（`stamp`・`required`・`test`）をループの後で読まない |
| CMP0126（3.21） | `set(CACHE)` が同じ名前の通常の変数を消さない | キャッシュに書く `CMAKE_OSX_ARCHITECTURES`・`CMAKE_OSX_DEPLOYMENT_TARGET` は `if(NOT DEFINED ...)` の中で書き、同じ名前の通常の変数が無い |
| CMP0128（3.22） | `<LANG>_EXTENSIONS` の既定をコンパイラーの既定から取り、要らない言語の版の指定を付けない | `CXX_STANDARD 20`・`CXX_STANDARD_REQUIRED ON`・`CXX_EXTENSIONS OFF` を明示している。`CXX_EXTENSIONS OFF` は GCC・clang の既定（GNU の拡張あり）と違うので、既定の版が C++20 のコンパイラー（GCC 16 など）でも `-std=c++20` が要る指定として付く（MSVC は既定が C++14 なので `/std:c++20` が付く）。mac のプラグインの Objective-C++ も、`OBJCXX_EXTENSIONS` を決めていないので `CXX_EXTENSIONS` に従い、どちらの既定でも `-std=c++20` |
| CMP0131（3.24） | `LINK_LIBRARIES` で `$<LINK_ONLY:...>` を使える | `$<LINK_ONLY>` を書かない |
| CMP0132（3.24） | 初めての構成で `CC`・`CXX` などの環境変数を設定しない | `execute_process()` などで、環境変数のコンパイラーを読む子のプロセスを構成の中で走らせない |

ubuntu（CMake 3.28・clang 18）で、テストの構成（`-DVW_BUILD_PLUGIN=OFF`）の
`compile_commands.json` とリンクの指定が 3.20 と 3.24 の既定で同じであること、OBJCXX を有効にした
最小の構成で上の版の指定が同じであることを確かめています。プラグインの構成（SDK が要る）は
CI の `build.yml` と実機で確かめます。

### 手元のビルド

mac の手元でプラグインをビルドするときは、プリセット `vw2026`（Vectorworks の版）を使います。
共通の設定は、コミットしてある `CMakePresets.json` の隠しプリセット `vw2026-base` にあります
（ビルド先 `build-vw2026/`・`RelWithDebInfo`・生成器 `Unix Makefiles`）。各自の Mac に固有のもの
——SDK の場所と並列数——だけを、コミットしない `CMakeUserPresets.json`（`.gitignore` 済み）に
書いて継ぎます。

```json
{
  "version": 2,
  "configurePresets": [
    {
      "name": "vw2026",
      "inherits": "vw2026-base",
      "cacheVariables": { "VW_SDK_DIR": "/Users/<you>/vw-sdk/2026-NNA-eng-mac-SDK" }
    }
  ],
  "buildPresets": [
    { "name": "vw2026", "inherits": "vw2026-base", "configurePreset": "vw2026", "jobs": 4 }
  ]
}
```

```sh
cmake --preset vw2026 && cmake --build --preset vw2026 --target VwCli
```

- **mac だけの手順です。** Windows は Visual Studio の生成器（`-A x64`）で、`CMAKE_BUILD_TYPE` が
  効かないので、このプリセットは使いません（上の CMake の表のとおり `--config Release`）。
- `RelWithDebInfo` は、Vectorworks が落ちたとき lldb で場所を追えるようにデバッグ情報を付けた
  ものです。clang では `Release` と最適化の段（`-O2` / `-O3`）とデバッグ情報が違います。
  最適化の違いで出方の変わる不具合は再現しないことがあるので、そのときは
  `cmake --preset vw2026 -DCMAKE_BUILD_TYPE=Release` で構成し直してビルドします。同じ
  `build-vw2026/` のキャッシュを書き換えるので、戻すときは `cmake --preset vw2026` だけで
  構成し直します（プリセットの `RelWithDebInfo` に戻ります）。
- `VW_SDK_DIR` はリポジトリの外の絶対パスで書きます（`$env{HOME}/...` でもよい）。
  `.worktreeinclude` で Claude Code の worktree へ写るのはこのファイルだけで、`/vw-sdk/` は
  写らないためです。`CMakeLists.txt` は環境変数 `VW_SDK_DIR` を読まないので、環境変数で
  渡したいときも `"VW_SDK_DIR": "$env{VW_SDK_DIR}"` と `cacheVariables` に書きます。
- `jobs` はマシンに合わせます（目安はコア数）。
- プリセットはスキーマの `version: 2`（CMake 3.20 から読める）ですが、`CMakeLists.txt` が CMake 3.24 以上を求めます（[CMake の版](#cmake-の版)）。
- **Vectorworks 2025 のプラグインは手元でビルドしません**（理由は[版「ビルド」](editions.md#ビルド)）。

## CI

| ワークフロー | 起動 | 中身 |
| --- | --- | --- |
| `go.yml`（既存） | push / PR・マージキュー | Go のテスト（macOS・Windows）と、配る 3 つのターゲットへのビルドの確かめ（出力は上げない）。テストとビルドは 2026 と 2025（`-tags vw2025`。[版](editions.md)）の両方で行う。両方の OS・両方の版のカバレッジを Codecov へ送る（どれもフラグ `go`）。ubuntu で `spool.NestingDepth` のファジングを 2 つの対象で 30 秒ずつ回す |
| `format.yml`（既存） | push / PR・マージキュー | `scripts/format.sh`（[整形](../format.md)。gofmt・shfmt・clang-format・PSScriptAnalyzer） |
| `cpp.yml` | push / PR・マージキュー | 無 SDK の単体テストと通信規則の見本の照合（macOS は ASan・UBSan 付き、Windows は MSVC で付けない）。macOS（clang の source-based coverage）と Windows（MSVC のビルドを `Microsoft.CodeCoverage.Console` で）の単体テストでカバレッジを測り、共通部（`src/core`）の分を Codecov へ送る（どちらもフラグ `cpp`。`FilesWindows.cpp` は Windows の分にだけ入る。パスを比べる包みも、`PathCompareMac.cpp` は macOS の分に、`PathCompareWindows.cpp` は Windows の分にだけ入る（どちらも `VwCliTests` の中で走るので、いまの測り方のままでよい）。Windows の範囲は `tests/MsvcCoverage.xml`。Windows はテストを計測の下で 1 回だけ走らせる）。ubuntu の clang で同じテストを ASan・UBSan 付きで走らせ（ただし `save` の `to` を開いている図面と比べる OS の呼び出しの包み（`PathCompareMac.cpp`・`PathCompareWindows.cpp`）は mac と Windows でだけビルドし、そのテストは Linux では中身の無い翻訳単位になる。[道具「パスの一致の判定」](tools.md#図面を扱う道具documentssaveclose)）、`core/Json` のファジングを 120 秒回す |
| `lint.yml`（既存） | push / PR・マージキュー | `scripts/lint.sh`（[検査](../lint.md)。go vet・actionlint・shellcheck・clang-tidy（無 SDK）・PSScriptAnalyzer） |
| `build.yml` | push（main）/ PR・マージキュー / 手動 | mac と Windows のビルド・梱包・リリースと、公開した資産の確かめ（下記） |
| `hooks.yml` | push / PR・マージキュー | `cli` のフック（`scripts/hooks/`）のテスト。macOS で `tests/hooks/hooks_test.sh`（`sh`）、Windows で `tests/hooks/Hooks.Tests.ps1`（Pester。版は `scripts/powershell.ps1` の `$pesterVersion` で、`-Mode Install` が入れ、`-Mode Test` が走らせる。Windows PowerShell 5.1）を走らせる（[インストール「テスト」](install-and-update.md#テスト)） |
| `cleanup-dev-release.yml` | PR が閉じたとき | その PR のプレリリース（`dev-<slug>-<PR 番号>-<短い sha>`。タグの末尾の `-<PR 番号>-<短い sha>` で見分ける。ブランチの名前が変わって slug が違うものも含む。短い sha の無い以前の形 `dev-<slug>-<PR 番号>` にも当たる）をリリースとタグごとすべて消す。**その PR の下書きのリリースも、タグ名で見分けて消す**（push で止められた `release` の下書きは、まだタグが無いので、タグで探すだけでは漏れる）。`build.yml` と同じ `concurrency` のグループ（`build-<PR 番号>`。両方に同じ式を書く）で走り、その PR の走っているビルドを止めてから消す（消したあとに公開されないため）。グループは `github.ref` ではなく PR 番号で組む（マージで閉じた PR では `github.ref` が `refs/heads/main` になり、main の push のビルドを止めて main のリリースが作られないため）。`build.yml` は開き直し（`reopened`）では走っている片付けを止めずに待つ（途中で止めるとタグや消しかけのリリースが残るため） |

`build.yml` のジョブ:

| ジョブ | 中身 |
| --- | --- |
| `meta` | ビルドする sha・刻印・公開先のタグ（`0.0.0-<短い sha>` / `dev-<slug>-<PR 番号>-<短い sha>` / 公開しない）を決め、`build.json` を書く（mac と Windows の zip に同じものを入れる）。ビルドのジョブは取り出すコミットを `meta` の出力に頼らず、同じ条件（同じリポジトリの PR だけ head）を自分の `checkout` に書き、取り出したものが `meta` の sha と同じかを確かめる（CodeQL が信頼できないコードの取り出しでないと読めるように。`meta` を変えても取り出すものが変わらないように） |
| `build-mac` | `macos-15`・Xcode 16.2（SDK リファレンスの `plugin/` と同じ）。プラグインのビルドとアドホック署名、Go の arm64 と amd64 を `lipo` で universal に、zip。版の行列で走る（ジョブの名前と版ごとの違いは[版「ビルド」](editions.md#ビルド)） |
| `build-windows` | `windows-latest`・MSVC。プラグインのビルド、Go の amd64、zip（.NET の `ZipFile`。項目の名前の区切りを `/` にするため。Windows PowerShell 5.1 の `Compress-Archive` は `\` にする）。版の行列で走る（同上） |
| `release` | 公開するときだけ。版ごとに 2 つの zip の sha256 を求めてマニフェスト（`vw2026.json`・`vw2025.json`。[レジストリ「マニフェスト」](../registry.md#マニフェストvw2026json)）を書き、**下書きのリリース**を作って、zip・マニフェスト・初回のスクリプトを上げ終えてから公開する（下書きは利用者の URL から見えないので、資産が揃わない間を見せない。下書きは `--target <sha>` で作り、タグは公開したときにできる。`latest` は公開した時点で進む）。**そのタグの公開済みのリリースが既にあれば、タグが指すコミットが今回の完全な sha と同じなら何もせず（同じ sha の実行し直し。タグを使い回さない）、違えば失敗にする**（短い sha が重なった。[リリースの形](#リリースの形)）。下書きのまま残ったもの（前の実行が途中で止まった）は消して作り直す。PR では、同じ PR の別のタグの下書き（push で止められた前の実行のもの）も消す |
| `verify` | 公開した資産を利用者と同じ道筋（既定のレジストリの `cli` の規則が指すマニフェスト → その `asset`）で mac と Windows で、版ごとに取ってきて、マニフェストの `tag` が公開したタグで sha256 が zip と合うこと、zip の直下が `build.json`・`bin/`・`hooks/`・プラグインだけで `bin/` の中が CLI だけであること、`build.json` と `bin/vw2026 version` の版がビルドした sha であること（mac は署名と `.vwr` も）を確かめる。mac では、公開した初回のスクリプト（`get-vw2026.sh`・`get-vw2025.sh`）で一時の `<Plug-Ins>` に入れて外す（Windows の初回のスクリプトは `install-windows` が確かめる）。main で `--latest` を付けて公開したときは、`latest` の規則（`releases/latest/download/vw2026.json`）でも取り、公開したタグを指すことを確かめる |
| `verify-result` | `verify` の結果を 1 つのチェックにまとめる（必須のチェックにするため）。公開するときは `release` と `verify` が成功したこと、公開しないときはどちらも飛ばされたことを確かめる。`verify` は行列のジョブで、公開しないときは行列を展開する前に飛ばされ、行列の値の入った名前（`verify (macos-latest, macos-arm64)` 等）のチェックを出さないので、`verify` そのものは必須にできない。前のジョブが失敗・中止しても飛ばされないよう `always()` で走らせる |
| `install-windows` | PR のプレリリースを公開したときだけ（`needs: [meta, release]`。main では走らせない）。`windows-latest` で、公開した PR のビルドを実物の exe とレジストリで入れ替え・外す（動いている CLI のよけ方・入った CLI の `uninstall cli` が断ること・利用者の `Path` の型と既存の項目が保たれること。手順は[インストール「テスト」](install-and-update.md#テスト)の「実物の exe の入れ替え」）。版の行列のジョブ `install-windows-editions` で走らせ、`install-windows` がその結果を 1 つのチェックにまとめる（必須のチェック。`verify-result` と同じ理由） |

fork の PR・マージキュー・main 以外の手動の実行は、マージのためのコミット（fork の PR）またはその ref を
ビルドするだけで、公開しません（`build.yml` も必須のチェックにできるよう、`merge_group` を付ける）。

必須のチェックになるワークフローには `merge_group` を付けます（付けないとキューの中で
チェックが始まらず、マージが進まない）。また `paths` で絞りません。必須のチェックはパスごとに
必須かどうかを分けられないため、絞ると対象外の変更だけの PR（文書だけ等）でチェックが報告されず、
マージできなくなります。

- **Windows のカバレッジの道具の版は固定しない。** `Microsoft.CodeCoverage.Console` は単独で
  配られず、ランナーのイメージの Visual Studio に付属するものだけを使える。MSVC と同じく
  イメージに従い、使った版はジョブのログ（`--version` の出力）で確かめる。イメージの更新で
  Codecov の `cpp` の数値が、このリポジトリのコミット無しに動くことがある。
- **Codecov は、1 つのコミットの結果が 4 つそろってから判定する**（`codecov.yml` の
  `after_n_builds`。`go.yml` と `cpp.yml` を、それぞれ macOS と Windows で）。片方の OS でだけ通る行が
  あるので、途中の値で判定すると必須の `codecov/patch` が失敗になり、マージキューがその最初の失敗で
  PR を外すため。カバレッジを送るワークフローや OS を足し引きしたら、この数を合わせる（多すぎると
  判定が出ず、マージキューが時間切れで外す）。
- **持たないもの**（要ると分かってから足す）: SDK に依存する clang-tidy とそのキャッシュ、
  CI の上で調べるためのワークフロー、リリースの定期的な確認。

- **SDK の取得に Secrets は要りません。** SDK リファレンスの [`scripts/fetch-vw-sdk.sh`](https://github.com/min-nano/vectorworks-developer-sdk-reference/tree/main/scripts) と同じく（このリポジトリの `scripts/fetch-vw-sdk.sh`）、公開の URL
  （`https://release.vectorworks.net/latest/Vectorworks/2026-NNA-eng-{mac,win}-SDK.zip`）から
  取ります。
- **SDK のキャッシュのキーは、配布されている SDK の版から決めます**（SDK リファレンスの
  `scripts/vw-sdk-cache-key.sh` と同じく、HEAD の `ETag` か `Last-Modified` を混ぜる。このリポジトリの
  `scripts/vw-sdk-cache-key.sh`）。URL は
  `latest` で、SDK が差し替わっても同じなので、固定のキーでは古い SDK でビルドし続けます。
  版を読めないときは、実行ごとに違うキー（`<前半>-unknown-<実行の ID>`）で直近のキャッシュを戻します
  （キャッシュは上書きできないので、固定のキーでは版が読めない間ずっと同じ SDK が戻るため）。
- リリースは `GITHUB_TOKEN`（`contents: write`）で行います。
- 自動レビューは差し当たり持ちません（持つなら Secrets に `CLAUDE_CODE_OAUTH_TOKEN` が要る）。
- 署名はアドホック（`codesign --force --deep --sign -`）。Developer ID と公証はしません。

## 梱包

`build.yml` の mac ジョブで Go もビルドし、`lipo` で universal にします（Go は arm64 の
バイナリをリンク時にアドホック署名する。`lipo` は各アーキテクチャの署名を保つ）。

```
cli-macos-arm64.zip                     cli-windows-x64.zip
└─ （zip の直下）                       └─ （zip の直下）
   ├─ cli.vwlibrary/                       ├─ cli.vlb ・ cli.vwr
   ├─ build.json                           ├─ build.json
   ├─ bin/vw2026          （universal）    ├─ bin\vw2026.exe
   └─ hooks/（install.sh・uninstall.sh・    └─ hooks\（install.ps1・uninstall.ps1・
            lib.sh）                                 lib.ps1）
```

2025 の zip（`cli-vw2025-macos-arm64.zip`・`cli-vw2025-windows-x64.zip`）も同じ形で、`bin/` の CLI が `vw2025`
（`vw2025.exe`）です（[版](editions.md)）。

zip の中身はそのまま `<Plug-Ins>/cli/` に置かれ、`bin/` の CLI もそこから動かします。`hooks/` のフックは
PATH だけを置きます（[インストール「CLI とスプールの置き場所」](install-and-update.md#cli-とスプールの置き場所)）。zip の項目の区切りは
`/` にします（`\` を区切りに使った zip を `install` は断る。[レジストリ「zip の形」](../registry.md#zip-の形)）。

`build.json`（`{"version","branch"}`）は CI が書きます。`ping` の `version` / `branch` と同じ値で、
`bin/vw2026 version` と突き合わせるためのものです。`vw2026 install` が新しいかを比べるのは
`build.json` ではなく、タグです（[インストール「流れ」](install-and-update.md#流れ)）。

## リリースの形

**タグはビルドごとに新しく付け、使い回しません**（[レジストリ「名前・タグ・ターゲット」](../registry.md#名前タグターゲット)）。
`vw2026 install` は既定のレジストリの `cli` の規則（`latest` は `releases/latest/download/vw2026.json`、
それ以外は `releases/download/<tag>/vw2026.json`）でマニフェストを取り、その `asset` を取ります。
初回のスクリプトは同じ形の URL で zip を取ります。**この URL の形と資産名（`vw2026.json`・
`cli-<target>.zip`・`get-vw2026.{sh,ps1}`）は変えません。**

- main: main への push ごとに `0.0.0-<短い sha>` のリリースを作る。タイトル `0.0.0 (<sha>)`・`--latest`。
  `latest`（`cli@latest`）はこれを指す。**`--latest` を付けるのは、ビルドしたコミットが公開する時点の
  `main` の先頭のときだけ**で、そうでなければ `--latest=false` で公開する（失敗した古い push の
  ワークフローを後から実行し直したときに、`latest` が古いコミットへ戻らないため）。`verify` の
  `latest` の確かめも、`--latest` を付けたときだけ行う。タグの形と、これからの版の付け方は[名前と識別子](identifiers.md)。
  `install` はタグの大小を比べない（違えば入れる）ので、版の付け方を
  変えても `install` は変わらない。
- PR: 同じリポジトリの PR で、**変更後のコードでビルドしたもの**を push ごとに
  `dev-<slug>-<PR 番号>-<短い sha>`（slug は[名前と識別子](identifiers.md)）にプレリリースする。
  タイトル **`Dev: <branch> (<sha>)`**・`--prerelease`（`latest` を動かさない）。中身は main のものと
  **同じ名前のプラグイン**で、入れると入れ替わる（`vw2026 install cli@<そのタグ>`）。戻すときは
  `vw2026 install cli@latest`（main の最新）。PR を閉じると、その PR のプレリリースはすべて消える。
- **既に同じタグの公開済みのリリースがあれば、そのリリースのタグが指すコミットが今回ビルドした
  完全な sha と同じかを確かめます。** 同じなら何もせず（同じ sha の実行し直し）、違えば（短い sha が
  以前のコミットと重なった）`release` を失敗にします。黙って飛ばすと `latest` が古いビルドを指したまま
  になるためです。重なったときは、タグの sha の文字数を増やすことを人が決めます。
- 古い main のリリースは消しません（入っているタグを `install` が後から取ることはないが、
  `cli@<そのタグ>` で前の版へ戻せるように残す）。
- PR の版は**ブランチの head** で決める。`pull_request` の `GITHUB_SHA` は main と合わせた
  マージのためのコミットで、ブランチのどのコミットとも違うので使わない。ビルドは head
  （`github.event.pull_request.head.sha`）を取り出して行い、`VW_BUILD_VERSION`・`build.json` の
  `version`・タイトルの `<sha>` にはその短い sha を、`VW_BUILD_BRANCH`・`build.json` の `branch`・
  タイトルの `<branch>` には `github.head_ref` を載せる。`ping` の `version` を
  `git log` の sha と突き合わせられるようにするため。
- CI は `pull_request` のマージのためのコミットで走り、プレリリースは head でビルドするので、
  ブランチが main から遅れていると両者の中身は違う。ブランチを main に追いつかせることは
  求めない（並行して進める PR ごとに取り込みを強いないため）。扱いは
  [実機の確かめ方「確かめること」](verification.md#確かめること)。
- head を取り出してプレリリースする手順は、同じリポジトリの PR
  （`github.event.pull_request.head.repo.full_name == github.repository`）でだけ走らせる。fork の
  head は信頼できないコードで、`github.head_ref` はどのリポジトリのブランチかを区別しない。
- 資産: `vw2026.json`（マニフェスト）・`cli-macos-arm64.zip` / `cli-windows-x64.zip`・
  `get-vw2026.{sh,ps1}`（初回のスクリプト。利用者は `releases/latest/download/` のものを使う）と、2025 の
  同じもの（`vw2025.json`・`cli-vw2025-macos-arm64.zip` / `cli-vw2025-windows-x64.zip`・`get-vw2025.{sh,ps1}`。[版](editions.md)）。
  初回のスクリプトはビルドしたコミットの雛形 `scripts/get-vw.{sh,ps1}` の `@VW_TAG@` をそのリリースのタグに、`@VW_YEAR@` を
  版に置き換えて上げる（どのリリースから取ったスクリプトも、そのリリースのその版の `cli` を入れる・外す。置き換えたかは `release` が確かめる。
  [インストール「初回のスクリプト」](install-and-update.md#初回のスクリプトget-vw2026sh--ps1)）。PowerShell のものは ASCII だけで書き、
  BOM を除いて上げる（Windows PowerShell 5.1 の `Invoke-RestMethod` は charset の無い本文を ISO-8859-1 で読むので、
  BOM や ASCII の外の文字が `iex` に崩れて渡る。`release` が確かめる）。本文に機械が読む情報は載せません
  （入れるのに要る情報はマニフェストが持つ）。CLI 単体の資産は持ちません（zip の `bin/` にあり、
  初回のスクリプトもそこから取り出す）。

# 名前と識別子

名前と配置場所の真実は本ページです（他のページは本ページを参照する）。本ページは 2026 の値で記述し、
2025 の `cli`（`vw2025`）の値は[版](editions.md)に記載しています。
**スプールの場所・コマンド名・リンクの名前・インストール先は変更しません**（変更すると呼び出し元や更新が発見できなくなる）。

**安定版と開発版を区別しません。** PR では変更後のコードでビルドしたものをプレリリースし、
同じ名前のプラグインとして置換します（`vw2026 install cli@<そのタグ>`。[ビルド「リリースの構成」](build-and-release.md#リリースの構成)）。
現在何が動作しているかは、`vw2026 call ping` の `version` / `branch` で判別できます。

| 項目 | 値 |
| --- | --- |
| プラグイン名（ファイル名・フォルダ名・`PLUGIN_VWR_ID`） | `cli` |
| モジュール | `cli.vwlibrary` / `cli.vlb` |
| リソース | `cli.vwr` |
| バンドル ID（mac） | `io.github.min-nano.cli` |
| コマンド | `vw2026` |
| レジストリの名前（プラグインのフォルダ名） | `cli`（既定のレジストリの名前。[レジストリ](../registry.md)） |
| CMake のターゲット | `VwCli` |
| リリースのタグ | **ビルドごとに新しいタグを付与し、再利用しない**（[レジストリ「名前・タグ・ターゲット」](../registry.md#名前タグターゲット)）。main は `0.0.0-<短い sha>`（`latest` のリリース）、PR は `dev-<ブランチの slug>-<PR 番号>-<短い sha>`（プレリリース）。短い sha はビルドした sha（PR はブランチの head）。slug はブランチ名から git の ref 名に使用できる文字のみを残して 40 文字までに切り詰めたもの（タグを 128 文字の上限（[レジストリ「名前・タグ・ターゲット」](../registry.md#名前タグターゲット)）に収めるため）で、可読性のための装飾。別のブランチが同じ slug になりうるので、一意にするのは PR 番号と sha。slug が空（日本語のみの名前など）なら `dev-<PR 番号>-<短い sha>`。生成するのは `scripts/dev-tag.sh` のみで（生成方法の詳細はその注釈）、`build.yml` が使用する（`cleanup-dev-release.yml` は末尾の `-<PR 番号>-<短い sha>` で識別する）。最新は `latest`（GitHub のリリースの最新。`cli@latest`）で指定する |
| ターゲット | `macos-arm64`・`windows-x64`（本プラグインが配布するもの。`macos-x64` は配布しない。[ビルド「CMake」](build-and-release.md#cmake)） |

## 配置場所

| 配置するもの | mac | Windows |
| --- | --- | --- |
| プラグイン（`<Plug-Ins>/<name>/`。本プラグインは `<Plug-Ins>/cli/`） | `~/Library/Application Support/Vectorworks/2026/Plug-Ins/cli/` | `%APPDATA%\Nemetschek\Vectorworks\2026\Plug-Ins\cli\` |
| インストールの記録 | `<Plug-Ins>/<name>/vw2026-install.json` | `<Plug-Ins>\<name>\vw2026-install.json` |
| CLI（PATH に登録する） | `<Plug-Ins>/cli/bin/vw2026` | `<Plug-Ins>\cli\bin\vw2026.exe` |
| スプール（プラグインが作成する） | `<Plug-Ins>/cli/spool/` | `<Plug-Ins>\cli\spool\` |
| `vw2026` の作業領域（`<Work>`。[インストール「作業領域」](install-and-update.md#作業領域)） | `/var/folders/…/T/vectorworks2026-cli-<鍵>/` | `%LOCALAPPDATA%\Temp\vectorworks2026-cli-<鍵>\` |
| 構築の場所 | `<Work>/staging/<name>/`（zip は `<Work>/staging/<name>.zip`） | `<Work>\staging\<name>\` |
| 退避の場所 | `<Work>/old/<name>/` | `<Work>\old\<name>\` |
| `uninstall` の途中の標識 | `<Work>/old/<name>.uninstalling` | `<Work>\old\<name>.uninstalling` |
| 退避した CLI（[インストール「実行中の CLI の退避」](install-and-update.md#実行中の-cli-の退避)） | `<Work>/old/vw2026.old-<時刻>` | `<Work>\old\vw2026.exe.old-<時刻>` |
| PATH | `~/.local/bin/vw2026`（`<Plug-Ins>/cli/bin/vw2026` へのリンク） | 利用者の `Path` に `<Plug-Ins>\cli\bin` |

- **場所の起点は環境変数を読み取らずに算出します。** スプールは、プラグインは自身のモジュールの場所から、
  `vw2026` は自身の実行ファイルの場所から算出します（[通信規則「スプールの場所」](../protocol.md#スプールの場所)）。
  `<Work>` と既定の `<Plug-Ins>` は OS の定められた場所から算出し、GUI アプリの Vectorworks と、ssh や別のアプリ
  から起動した `vw2026` とで相違しないようにします。
  - `<Work>`: Windows は `SHGetKnownFolderPath(FOLDERID_LocalAppData)` の下の `Temp`（`%TEMP%` の既定の場所。環境変数 `TMP` /
    `TEMP` は読み取らない。`vw2026` は `syscall` 経由）です。mac は `/usr/bin/getconf DARWIN_USER_TEMP_DIR` の出力
    （`confstr(_CS_DARWIN_USER_TEMP_DIR)`。利用者ごとの一時フォルダ。環境変数 `TMPDIR` は読み取らない。cgo を使用しないので
    `getconf` を実行する）です。その下の `vectorworks2026-cli-<鍵>`（`<Plug-Ins>` ごと。[インストール「作業領域」](install-and-update.md#作業領域)）に配置します。
  - 既定の `<Plug-Ins>`: Windows は `%APPDATA%` の指す場所を `SHGetKnownFolderPath(FOLDERID_RoamingAppData)` で
    算出します（環境変数 `APPDATA` は読み取らない。`syscall` 経由）。mac はホームディレクトリの `Library/Application Support` の下で、
    ホームは利用者の passwd の項目（`getpwuid_r(getuid())` の `pw_dir`）から算出します（環境変数 `HOME` は読み取らない。
    `vw2026` は `os/user`）。
- `<Plug-Ins>` は Vectorworks の利用者フォルダの中です。Vectorworks の設定で利用者フォルダを
  移動できるので、標準以外の場所も、`<Work>` と同じドライブにあれば扱えます（初回スクリプトに
  `--plugins-dir` で渡し、その後はインストールされた `vw2026` が自身の場所から算出する。インストールした場所は記録しない。[インストール](install-and-update.md#前提)）。
  但し `cli` は利用者ごとの `Plug-Ins` にのみ配置します（ワークグループのフォルダなど、複数の利用者が読み込む
  場所に配置するとスプールも共有される。[設計「決定事項」](../design.md#決定事項)）。
- `<Work>` は、本プラグインのみでなく、`vw2026` でインストールする全てのプラグインの構築・退避の場所です
  （`<name>` ごとに区別する）。CLI とスプールは配置しません（`cli` のフォルダの中にある。
  [インストール「CLI とスプールの配置場所」](install-and-update.md#cli-とスプールの配置場所)）。
  PATH は本プラグインのフックが設定します（[インストール「`cli` のフック」](install-and-update.md#cli-のフック)）。

## `src/BuildConfig.h`

```cpp
#define PLUGIN_VWR_ID   "cli"
// VW_BUILD_VERSION / VW_BUILD_BRANCH は CMake が構成の度に書く BuildStamp.h にある
// （-D で渡さなければ "local"）
#include "BuildStamp.h"
```

スプールの名前（`spool`）は、スプールの場所を算出する共通部（SDK に依らない）が使用するので、
`src/core/Protocol.h` の `kSpoolDirName` に配置します。CLI 側の `cli/internal/spool` の `SpoolDirName` と
対であり、両側のテストが[通信規則のフィクスチャ](../fixtures.md)の `constants.json` と照合します
（[通信規則](../protocol.md#スプールの場所)）。`<Work>` のフォルダ名の接頭辞（`vectorworks2026-cli`）は
`vw2026` のみが使用するので、`cli/internal/install` の `workDirName` に配置します（フィクスチャには含めない）。
CLI の実行ファイルを配置する `bin` は、`vw2026` が実行ファイルのあるフォルダの親をスプールの親とするのみで、
名前としては使用しません。

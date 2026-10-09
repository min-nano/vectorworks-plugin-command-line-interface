# 名前と識別子

名前と置き場所の真実はこのページです（ほかのページはここを参照する）。
**スプールの場所・コマンド名・リンクの名前・インストール先は、最初の配布のあとは変えません**
（変えると呼ぶ側や更新が見つけられなくなる）。

**安定版と開発版を区別しません。** PR では変更後のコードでビルドしたものをプレリリースし、
同じ名前のプラグインとして入れ替えます（[ビルド「リリースの形」](build-and-release.md#リリースの形)）。
いま何が動いているかは、`vw2026 call ping` の `version` / `branch` で分かります。

| 項目 | 値 |
| --- | --- |
| プラグイン名（ファイル名・フォルダ名・`PLUGIN_VWR_ID`） | `cli` |
| モジュール | `cli.vwlibrary` / `cli.vlb` |
| リソース | `cli.vwr` |
| バンドル ID（mac） | `io.github.min-nano.cli` |
| コマンド | `vw2026` |
| CMake のターゲット | `VwCli` |
| リリースのタグ | `stable`（main）・`dev-<ブランチの slug>`（PR のプレリリース。slug はブランチ名の `/:@ ` を `-` にし、ほかの記号を落としたもの） |

## 置き場所

| 置くもの | mac | Windows |
| --- | --- | --- |
| プラグイン（`<Plug-Ins>/cli/`） | `~/Library/Application Support/Vectorworks/2026/Plug-Ins/cli/` | `%APPDATA%\Nemetschek\Vectorworks\2026\Plug-Ins\cli\` |
| CLI の場所（`<CLI>`） | `~/Library/Application Support/vectorworks2026-cli/` | `%LOCALAPPDATA%\vectorworks2026-cli\` |
| CLI（PATH に載せる） | `<CLI>/bin/vw2026` | `<CLI>\bin\vw2026.exe` |
| スプール（プラグインが作る） | `<CLI>/spool/` | `<CLI>\spool\` |
| 組み立ての場所 | `<CLI>/staging/cli/` | `<CLI>\staging\cli\` |
| 退避の場所 | `<CLI>/old/cli/` | `<CLI>\old\cli\` |
| 入れた場所の記録 | `<CLI>/cli.plugins-dir` | `<CLI>\cli.plugins-dir` |
| PATH | `~/.local/bin/vw2026`（`<CLI>/bin/vw2026` へのリンク） | 利用者の `Path` に `<CLI>\bin` |

- `<Plug-Ins>` は Vectorworks の利用者フォルダの中です。Vectorworks の設定で利用者フォルダを
  移せるので、標準以外の場所も、`<CLI>` と同じドライブにあれば扱えます（入れた場所の記録。
  [インストールと更新](install-and-update.md#インストーラvw-installsh--ps1)）。
- 組み立てと退避の場所を `<CLI>` に置くのは、`Plug-Ins` の外（Vectorworks に読まれない）で、
  前提により `<Plug-Ins>` と同じドライブにあるためです。

## 拡張機能

**登録しません**（[構成](architecture.md#拡張機能を登録しない)）。拡張機能なしで読み込まれない
ことが実機で分かったときだけ、メニューコマンドを 1 つ登録し、そのユニバーサル名と UUID を
ここに足します。

## `src/BuildConfig.h`

```cpp
#define PLUGIN_VWR_ID   "cli"
#define PLUGIN_APP_DIR  "vectorworks2026-cli"   // <CLI> のディレクトリ名。スプールはその下の spool
// VW_BUILD_VERSION / VW_BUILD_BRANCH は CMake が渡す（無ければ "local"）
```

`PLUGIN_APP_DIR` は CLI 側の `cli/internal/spool` の `AppDirName` / `SpoolDirName` と対です
（[作法](../protocol.md#スプールの場所)）。

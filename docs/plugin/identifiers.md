# 名前と識別子

**スプールの場所・コマンド名・リンクの名前・インストール先は、最初の配布のあとは変えません**
（変えると呼ぶ側や更新が見つけられなくなる）。

**安定版と開発版を区別しません。** PR では変更後のコードでビルドしたものをプレリリースし、
同じ名前のプラグインとして入れ替えます（[ビルド「リリースの形」](build-and-release.md#リリースの形)）。
いま何が入っているかは、生存の印の `version` / `branch` で分かります。

| 項目 | 値 |
| --- | --- |
| プラグイン名（ファイル名・フォルダ名・`PLUGIN_VWR_ID`） | `cli` |
| モジュール | `cli.vwlibrary` / `cli.vlb` |
| リソース | `cli.vwr` |
| バンドル ID（mac） | `io.github.min-nano.cli` |
| スプール | `<CLI>/spool/` |
| コマンド | `vw2026` |
| CMake のターゲット | `VwCli` |
| リリースのタグ | `stable`（main）・`dev-<ブランチの slug>`（PR のプレリリース） |
| インストール先 | `<Plug-Ins>/cli/` |
| CLI の置き場所 | `<CLI>/bin/`（PATH に載せる） |
| 組み立て・退避の場所 | `<CLI>/staging/cli/`・`<CLI>/old/cli/` |
| 入れた場所の記録 | `<CLI>/cli.plugins-dir` |

`<CLI>` は mac `~/Library/Application Support/vectorworks2026-cli/`、Windows
`%LOCALAPPDATA%\vectorworks2026-cli\`（[インストールと更新「置き場所」](install-and-update.md#置き場所)）。

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

# 版（Vectorworks 2026 と 2025）

`cli` は、同一のソースから Vectorworks の版ごとに生成して配布します。現在配布するのは **2026**（既定）と
**2025** です。版ごとの差異の真実は本ページです。

**他のページは 2026 の名前（`vw2026`・`VW2026_*`・`vw2026.json` など）で記述します。** 2025 の CLI では、
下記の「[名前と配置場所](#名前と配置場所)」の表で読み替えます。手順・終了コード・通信規則は版に依らず同一です。

## 決定事項

| 項目 | 決定 | 理由・補足 |
| --- | --- | --- |
| CLI | **版ごとに別の CLI**（2025 は `vw2025`） | コマンド名はインストール先の Vectorworks の版とする（[設計「決定事項」](../design.md#決定事項)の「名前」）。2025 と 2026 の Vectorworks が同一の計算機に存在しても、コマンド・環境変数・PATH・作業領域が衝突しない |
| 版の決定方法（Go） | **ビルドタグ**（`-tags vw2025`）。版から定まる名前は `cli/internal/edition` に配置し、他のコードはそこから読み取る | Go のコードは 1 つにする。テスト（`go test`）と検査（`go vet`）は 2026 と 2025 の両方で実行する（`go.yml`・`scripts/lint.sh`） |
| プラグイン | **ソースは版に依らない**。ビルドに使用する SDK のみが異なる | 2025 の SDK でも同一のソースがビルドでき、ツールが応答することを実機で確認した（mac・Vectorworks 2025 30.8.0） |
| リリース | **同一のリリースに併置する**。1 つのタグのリリースに、2026 と 2025 の zip・マニフェスト・初回スクリプトをアップロードする | 2 つの版が同一のコミットのビルドになり、`latest` も同時に進む。2025 の `latest` の規則（`releases/latest/download/vw2025.json`）も、GitHub の最新のリリースを指すのみで足りる |
| 資産の名前 | 2026 は従来のまま（`cli-macos-arm64.zip` など）。2025 は `cli-vw2025-` を前に付加する | 2026 の資産名は変更しない（[ビルド「リリースの構成」](build-and-release.md#リリースの構成)）。資産はマニフェストが絶対の参照で指すので、名前の差異は `install` に影響しない |
| フック | **同一のフックを両方の zip に格納する。** フックは渡された変数の名前（`VW2026_HOOK_INSTALL_DIR` か `VW2025_HOOK_INSTALL_DIR`）で版を判定する | CLI は自身の版の名前の変数のみを渡し、呼び出し元の環境に存在する他の版の名前（`VW<4 桁>_HOOK_*`）は渡さない。フックは、いずれも存在しない場合・両方存在する場合は何も変更する前に失敗する |
| 初回スクリプト | **1 つの雛形**（`scripts/get-vw.sh`・`.ps1`）から、リリースがタグと版を書き込んで `get-vw2026.*` と `get-vw2025.*` を生成する | スクリプトの内容（引数・展開・環境変数を渡さないこと）を 2 か所に記述しない |

## 名前と配置場所

2026 の値は[名前と識別子](identifiers.md)が真実であり、2025 の値はそれを読み替えたものです。

| 項目 | 2026 | 2025 |
| --- | --- | --- |
| コマンド | `vw2026` | `vw2025` |
| 指定の環境変数 | `VW2026_<指定>`（`VW2026_SESSION`・`VW2026_CALL_TIMEOUT` など） | `VW2025_<指定>` |
| フックに渡す環境変数 | `VW2026_HOOK_*` | `VW2025_HOOK_*` |
| 既定の `<Plug-Ins>`（mac） | `~/Library/Application Support/Vectorworks/2026/Plug-Ins/` | `~/Library/Application Support/Vectorworks/2025/Plug-Ins/` |
| 既定の `<Plug-Ins>`（Windows） | `%APPDATA%\Nemetschek\Vectorworks\2026\Plug-Ins\` | `%APPDATA%\Nemetschek\Vectorworks\2025\Plug-Ins\` |
| CLI | `<Plug-Ins>/cli/bin/vw2026`（`vw2026.exe`） | `<Plug-Ins>/cli/bin/vw2025`（`vw2025.exe`） |
| インストールの記録 | `<Plug-Ins>/<name>/vw2026-install.json` | `<Plug-Ins>/<name>/vw2025-install.json` |
| 作業領域（`<Work>`） | `vectorworks2026-cli-<鍵>` | `vectorworks2025-cli-<鍵>` |
| 退避した CLI | `<Work>/old/vw2026.old-<時刻>`（`vw2026.exe.old-<時刻>`） | `<Work>/old/vw2025.old-<時刻>`（`vw2025.exe.old-<時刻>`） |
| PATH（mac） | `~/.local/bin/vw2026` | `~/.local/bin/vw2025` |
| PATH（Windows） | 利用者の `Path` に `<Plug-Ins>\cli\bin` | 同一（`<Plug-Ins>` が 2025 のもの） |
| `launch` の既定（mac） | `open -a "Vectorworks 2026"` | `open -a "Vectorworks 2025"` |
| `launch` の既定（Windows） | `%ProgramFiles%\Vectorworks 2026\Vectorworks2026.exe` | `%ProgramFiles%\Vectorworks 2025\Vectorworks2025.exe` |
| 既定のレジストリ | `cli/internal/registry/default-vw2026.json`（`cli` → `vw2026.json`） | `cli/internal/registry/default-vw2025.json`（`cli` → `vw2025.json`） |
| マニフェスト | `vw2026.json` | `vw2025.json` |
| zip | `cli-macos-arm64.zip`・`cli-windows-x64.zip` | `cli-vw2025-macos-arm64.zip`・`cli-vw2025-windows-x64.zip` |
| 初回スクリプト | `get-vw2026.sh`・`get-vw2026.ps1` | `get-vw2025.sh`・`get-vw2025.ps1` |
| SDK | `2026-NNA-eng-{mac,win}-SDK.zip` | `2025-NNA-eng-{mac,win}-SDK.zip` |

mac の 2025 の `<Plug-Ins>` は、確認した計算機では `Plug-ins`（`i` が小文字）でした。mac の既定のボリュームは
大文字と小文字を区別しないので、表の `Plug-Ins` と同一のフォルダを指します。

プラグインの名前（`cli`）・モジュール・リソース・バンドル ID・スプール（`<Plug-Ins>/cli/spool/`）・通信規則は、
版に依らず同一です。`<Plug-Ins>` が版ごとに異なるので、スプールも混在しません。

2025 をインストールする場合は、2025 の初回スクリプトを使用します。

```bash
curl -fsSL https://github.com/min-nano/vectorworks-plugin-command-line-interface/releases/latest/download/get-vw2025.sh | sh
```

## ビルド

- `build.yml` の `build-mac` と `build-windows` は、版の行列で 2 つずつ実行されます。2026 はジョブ名を `build-mac`・
  `build-windows` のまま（必須のチェック）とし、2025 は `build-mac-vw2025`・`build-windows-vw2025` とします。
  2025 は 2025 の SDK でプラグインを、`-tags vw2025` で `vw2025` をビルドします。SDK のキャッシュの接頭辞は
  `vw-sdk-2025-mac`・`vw-sdk-2025-win` です。
- `release` は版ごとにマニフェストと初回スクリプトを生成し、全てを同一の下書きのリリースにアップロードしてから公開します。
  `verify` は版ごと・OS ごとに、利用者と同じ経路で取得して検証します。`install-windows` も版ごとに実行されます
  （行列のジョブ `install-windows-editions` の結果を、必須のチェック `install-windows` が集約する）。
- **2025 のプラグインはローカル環境（mac）でビルドしません。** 2025 の mac の SDK のヘッダ
  （`Kernel/Base/TypesArray.h` の `TXGenericArray<T>::SetSize`）は、クラスに存在しないメンバー `RemoveAt` を呼び出しており、
  テンプレートの定義の時点でこれを誤りとする clang 19 以降ではヘッダを読み込むのみで停止します（2026 の SDK では
  この関数が存在しない）。CI は 2026 と同じ Xcode 16.2 でビルドします。SDK のヘッダは書き換えません。
- CLI（Go）はローカル環境でも両方の版をビルド・テストできます（`go test ./...` と `go test -tags vw2025 ./...`）。

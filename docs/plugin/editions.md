# 版（Vectorworks 2026 と 2025）

`cli` は、同じソースから Vectorworks の版ごとに作って配ります。いま配るのは **2026**（既定）と
**2025** です。版ごとの違いの真実はこのページです。

**ほかのページは 2026 の名前（`vw2026`・`VW2026_*`・`vw2026.json` など）で書きます。** 2025 の CLI では、
下の「[名前と置き場所](#名前と置き場所)」の表で読み替えます。手順・終了コード・通信規則は版に依らず同じです。

## 決めたこと

| 項目 | 決定 | 理由・補足 |
| --- | --- | --- |
| CLI | **版ごとに別の CLI**（2025 は `vw2025`） | コマンド名はインストール先の Vectorworks の版にする（[設計「決めたこと」](../design.md#決めたこと)の「名前」）。2025 と 2026 の Vectorworks が同じ計算機にあっても、コマンド・環境変数・PATH・作業の場所が衝突しない |
| 版の決め方（Go） | **ビルドタグ**（`-tags vw2025`）。版から決まる名前は `cli/internal/edition` に置き、ほかのコードはそこから読む | Go のコードは 1 つにする。テスト（`go test`）と検査（`go vet`）は 2026 と 2025 の両方で走らせる（`go.yml`・`scripts/lint.sh`） |
| プラグイン | **ソースは版に依らない**。ビルドに使う SDK だけが違う | 2025 の SDK でも同じソースがビルドでき、道具が応えることを実機で確かめた（mac・Vectorworks 2025 30.8.0） |
| リリース | **同じリリースに並べる**。1 つのタグのリリースに、2026 と 2025 の zip・マニフェスト・初回のスクリプトを上げる | 2 つの版が同じコミットのビルドになり、`latest` も一緒に進む。2025 の `latest` の規則（`releases/latest/download/vw2025.json`）も、GitHub の最新のリリースを指すだけで済む |
| 資産の名前 | 2026 は今までのまま（`cli-macos-arm64.zip` など）。2025 は `cli-vw2025-` を前に付ける | 2026 の資産名は変えない（[ビルド「リリースの形」](build-and-release.md#リリースの形)）。資産はマニフェストが絶対の参照で指すので、名前の違いは `install` に影響しない |
| フック | **同じフックを両方の zip に入れる。** フックは渡された変数の名前（`VW2026_HOOK_INSTALL_DIR` か `VW2025_HOOK_INSTALL_DIR`）で版を決める | CLI は自分の版の名前の変数だけを渡し、呼ぶ側の環境にあるほかの版の名前（`VW<4 桁>_HOOK_*`）は渡さない。フックは、どちらも無いとき・両方あるときは何も変える前に失敗する |
| 初回のスクリプト | **1 つの雛形**（`scripts/get-vw.sh`・`.ps1`）から、リリースがタグと版を書き込んで `get-vw2026.*` と `get-vw2025.*` を作る | スクリプトの中身（引数・取り出し・環境変数を渡さないこと）を 2 か所に書かない |

## 名前と置き場所

2026 の値は[名前と識別子](identifiers.md)が真実で、2025 の値はそれを読み替えたものです。

| 項目 | 2026 | 2025 |
| --- | --- | --- |
| コマンド | `vw2026` | `vw2025` |
| 指定の環境変数 | `VW2026_<指定>`（`VW2026_SESSION`・`VW2026_CALL_TIMEOUT` など） | `VW2025_<指定>` |
| フックに渡す環境変数 | `VW2026_HOOK_*` | `VW2025_HOOK_*` |
| 既定の `<Plug-Ins>`（mac） | `~/Library/Application Support/Vectorworks/2026/Plug-Ins/` | `~/Library/Application Support/Vectorworks/2025/Plug-Ins/` |
| 既定の `<Plug-Ins>`（Windows） | `%APPDATA%\Nemetschek\Vectorworks\2026\Plug-Ins\` | `%APPDATA%\Nemetschek\Vectorworks\2025\Plug-Ins\` |
| CLI | `<Plug-Ins>/cli/bin/vw2026`（`vw2026.exe`） | `<Plug-Ins>/cli/bin/vw2025`（`vw2025.exe`） |
| 入れたときの記録 | `<Plug-Ins>/<name>/vw2026-install.json` | `<Plug-Ins>/<name>/vw2025-install.json` |
| 作業の場所（`<Work>`） | `vectorworks2026-cli-<鍵>` | `vectorworks2025-cli-<鍵>` |
| よけた CLI | `<Work>/old/vw2026.old-<時刻>`（`vw2026.exe.old-<時刻>`） | `<Work>/old/vw2025.old-<時刻>`（`vw2025.exe.old-<時刻>`） |
| PATH（mac） | `~/.local/bin/vw2026` | `~/.local/bin/vw2025` |
| PATH（Windows） | 利用者の `Path` に `<Plug-Ins>\cli\bin` | 同じ（`<Plug-Ins>` が 2025 のもの） |
| `launch` の既定（mac） | `open -a "Vectorworks 2026"` | `open -a "Vectorworks 2025"` |
| `launch` の既定（Windows） | `%ProgramFiles%\Vectorworks 2026\Vectorworks2026.exe` | `%ProgramFiles%\Vectorworks 2025\Vectorworks2025.exe` |
| 既定のレジストリ | `cli/internal/registry/default-vw2026.json`（`cli` → `vw2026.json`） | `cli/internal/registry/default-vw2025.json`（`cli` → `vw2025.json`） |
| マニフェスト | `vw2026.json` | `vw2025.json` |
| zip | `cli-macos-arm64.zip`・`cli-windows-x64.zip` | `cli-vw2025-macos-arm64.zip`・`cli-vw2025-windows-x64.zip` |
| 初回のスクリプト | `get-vw2026.sh`・`get-vw2026.ps1` | `get-vw2025.sh`・`get-vw2025.ps1` |
| SDK | `2026-NNA-eng-{mac,win}-SDK.zip` | `2025-NNA-eng-{mac,win}-SDK.zip` |

プラグインの名前（`cli`）・モジュール・リソース・バンドル ID・スプール（`<Plug-Ins>/cli/spool/`）・通信規則は、
版に依らず同じです。`<Plug-Ins>` が版ごとに違うので、スプールも混ざりません。

2025 を入れるときは、2025 の初回のスクリプトを使います。

```bash
curl -fsSL https://github.com/min-nano/vectorworks-plugin-command-line-interface/releases/latest/download/get-vw2025.sh | sh
```

## ビルド

- `build.yml` の `build-mac` と `build-windows` は、版の行列で 2 つずつ走ります。2026 はジョブ名を `build-mac`・
  `build-windows` のまま（必須のチェック）にし、2025 は `build-mac-vw2025`・`build-windows-vw2025` にします。
  2025 は 2025 の SDK でプラグインを、`-tags vw2025` で `vw2025` をビルドします。SDK のキャッシュの前置きは
  `vw-sdk-2025-mac`・`vw-sdk-2025-win` です。
- `release` は版ごとにマニフェストと初回のスクリプトを作り、すべてを同じ下書きのリリースに上げてから公開します。
  `verify` は版ごと・OS ごとに、利用者と同じ道筋で取って確かめます。`install-windows` も版ごとに走り、2025 は
  `install-windows-vw2025` です。
- **2025 のプラグインは手元（mac）でビルドしません。** 2025 の mac の SDK のヘッダ
  （`Kernel/Base/TypesArray.h` の `TXGenericArray<T>::SetSize`）は、クラスに無いメンバー `RemoveAt` を呼んでいて、
  テンプレートの定義の時点でこれを誤りとする clang 19 以降ではヘッダを読むだけで止まります（2026 の SDK では
  この関数が無い）。CI は 2026 と同じ Xcode 16.2 でビルドします。SDK のヘッダは書き換えません。
- CLI（Go）は手元でも両方の版をビルド・テストできます（`go test ./...` と `go test -tags vw2025 ./...`）。

## 実機で確かめていないこと

- **Windows の 2025 の既定の場所**（`%APPDATA%\Nemetschek\Vectorworks\2025\Plug-Ins`・
  `%ProgramFiles%\Vectorworks 2025\Vectorworks2025.exe`）は、2026 と同じ形とみなしたものです。違えば
  `--plugins-dir`（初回のスクリプト）・`--app`（`launch`）で渡せますが、既定を直します。
- mac の 2025 の `<Plug-Ins>` は、確かめた計算機では `Plug-ins`（`i` が小文字）でした。mac の既定のボリュームは
  大文字と小文字を区別しないので、同じフォルダを指します。

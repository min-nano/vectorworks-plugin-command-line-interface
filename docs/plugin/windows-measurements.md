# Windows の実機での確かめ方

Windows の Vectorworks の起動・終了と、プラグインの有無による違いを実機で確かめる手順です。段 2 の実機確認で
使い、[実装の順序「実機の確かめ方」](plan.md#実機の確かめ方)から指しています。

段 2 で測った結果は SDK リファレンスの `Findings/` へ移しました（下記「結果の移った先」）。このプラグインでの扱いは
[SDK の調査 0](../sdk-research.md#0-拡張機能を-1-つも登録しないプラグインの読み込み) にあります。

## 環境

| 項目 | 値 |
| --- | --- |
| Vectorworks | 2026 Update 6（Build 872399）x64。`C:\Program Files\VW2026\` に入っている |
| OS | Windows 11 Pro ARM64（UTM の VM）。Vectorworks・`cli.vlb`・`vw2026` は x64 のエミュレーションで動く |
| 主記憶 | 約 6 GB |
| 権限 | UAC が無効（`EnableLUA=0`）。エクスプローラから起動しても Vectorworks は管理者の権限で動く |
| 描画 | Direct3D 11 を WARP（`d3d10warp.dll`。ソフトウェアの描画）で動かしている |
| プラグイン | `0683df0` と `296a797`（どちらもタイマーの前にモジュールを留める版） |

- `vw2026 launch` は既定で `C:\Program Files\Vectorworks 2026\Vectorworks2026.exe` を探すので、この PC では
  `VW2026_APP` に実際の場所を与えた。
- Vectorworks を 1 つ起動すると、`Vectorworks2026.exe` のプロセスが 4 つ立つ（本体と、CEF の GPU・
  ネットワーク・ストレージの補助プロセス）。二重に起動したのではない。
- 起動からホーム画面までは数分かかる。

## 確かめ方

- **起動・終了**: Vectorworks を起動し（`Start-Process` か `vw2026 launch`）、ホーム画面まで待ってから
  メインウィンドウへ閉じる指示を送り（`CloseMainWindow`。メニューの「終了」と同じ）、終了コードを読む。
  終了コードは、プロセスが終わる前にハンドルを開いておいて読む（後から開くと読めない）。
- **図面の描画**: ホーム画面の「新規用紙」で新しい図面を開き、タブを閉じてから終了する。
- **プラグインを外す**: `%APPDATA%\Nemetschek\Vectorworks\2026\Plug-Ins\cli\` を Plug-Ins の外へ移し、
  同じ手順を行った後、元へ戻す。
- **落ちた場所**: Vectorworks 同梱の Sentry のダンプ（`%APPDATA%\Nemetschek\Vectorworks\2026\Sentry\reports\`）と、
  Windows エラー報告の LocalDumps（`HKLM\SOFTWARE\Microsoft\Windows\Windows Error Reporting\LocalDumps\Vectorworks2026.exe`
  に `DumpType=2`。`%LOCALAPPDATA%\CrashDumps\` に 1 つ 2.5 GB 前後の完全なダンプが書かれる）を読む。
  VM ごと止まったかは System のイベント 1001（バグチェック）で見る。

## 結果の移った先

段 2 の実測（上の環境。プラグインは `0683df0` と `296a797`）は、次の調査で `Findings/` に反映しました。

| 実測 | 移った先 |
| --- | --- |
| 拡張機能を登録しないプラグインが読み込まれ、受け付けること | [Findings「Plug-in Modules」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Plug-in%20Modules.md)の「拡張機能を 1 つも登録しないモジュールも、有効なら起動時から常駐する」（[#252](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/252)） |
| 終了時の `0xC0000005`（すでに外された `libcef.dll` の中で落ちる） | 同じページの「OS タイマーを持つ常駐プラグイン（留めたモジュール）は、終了時の落ち方を変えない」（[#257](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/257)） |
| 新しい図面の最初の描画中に `MaxonDocumentBuilder.vlb` の中で落ちた 1 回 | 同じページの「新しい図面の最初の描画——30 回で 1 度も落ちなかった」（[#270](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/270)） |
| VM ごとの停止（`viogpudo.sys` のバグチェック `0xD1`） | 同じページの「【罠】Windows の VW は起動時にモジュールを一度降ろして読み込み直す」の「読み違えやすいところ」 |
| 開発元不明のプラグイン | 同じページの「開発元不明のプラグイン」のダイアログ（[#258](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/258)・[#264](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/264)）。このプラグインでの扱いは[構成「拡張機能を登録しない」](architecture.md#拡張機能を登録しない) |

## 分かっていないこと

- **この VM（ARM64 上の x64 エミュレーション・WARP の描画・UTM の GPU のドライバ）に固有のことかどうか。**
  段 2 の実測も、移った先の SDK リファレンスの実測（#257・#269・#270）も、上の環境の VM で測った。x64 の実機では
  測っていない。終了までの時間や落ち方が実機で違っても、それは測っていないことによる。

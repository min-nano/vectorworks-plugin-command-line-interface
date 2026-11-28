# Windows の実機での検証方法

Windows の Vectorworks の起動・終了と、プラグインの有無による差異を実機で検証する手順です。プラグインの骨格
（ツールが `ping` のみの版）の実機の確認で使用し、[実機での検証方法](verification.md#windows)から参照しています。

その際に測定した結果は SDK リファレンスの `Findings/` へ移転しました（下記「結果の移転先」）。このプラグインでの扱いは
[SDK の調査 0](../sdk-research.md#0-拡張機能を-1-つも登録しないプラグインの読み込み判明) にあります。

## 環境

| 項目 | 値 |
| --- | --- |
| Vectorworks | 2026 Update 6（Build 872399）x64。`C:\Program Files\VW2026\` にインストールされている |
| OS | Windows 11 Pro ARM64（UTM の VM）。Vectorworks・`cli.vlb`・`vw2026` は x64 のエミュレーションで動作する |
| 主記憶 | 約 6 GB |
| 権限 | UAC が無効（`EnableLUA=0`）。エクスプローラから起動しても Vectorworks は管理者の権限で動作する |
| 描画 | Direct3D 11 を WARP（`d3d10warp.dll`。ソフトウェアによる描画）で動作させている |
| プラグイン | `0683df0` と `296a797`（いずれもタイマーの前にモジュールを留める版） |

- `vw2026 launch` は既定で `C:\Program Files\Vectorworks 2026\Vectorworks2026.exe` を探索するため、この計算機では
  `VW2026_APP` に実際の場所を指定しました。
- Vectorworks を 1 つ起動すると、`Vectorworks2026.exe` のプロセスが 4 つ生成されます（本体と、CEF の GPU・
  ネットワーク・ストレージの補助プロセス）。二重に起動したのではありません。
- 起動からホーム画面の表示までは数分を要します。

## 検証方法

- **起動・終了**: Vectorworks を起動し（`Start-Process` か `vw2026 launch`）、ホーム画面の表示まで待機してから
  メインウィンドウへ閉じる指示を送り（`CloseMainWindow`。メニューの「終了」と同じ）、終了コードを読み取る。
  終了コードは、プロセスが終了する前にハンドルを開いておいて読み取る（後から開くと読み取れない）。
- **図面の描画**: ホーム画面の「新規用紙」で新しい図面を開き、タブを閉じてから終了する。
- **プラグインの除外**: `%APPDATA%\Nemetschek\Vectorworks\2026\Plug-Ins\cli\` を Plug-Ins の外へ移動し、
  同じ手順を行った後、元の場所へ戻す。
- **異常終了した箇所**: Vectorworks 同梱の Sentry のダンプ（`%APPDATA%\Nemetschek\Vectorworks\2026\Sentry\reports\`）と、
  Windows エラー報告の LocalDumps（`HKLM\SOFTWARE\Microsoft\Windows\Windows Error Reporting\LocalDumps\Vectorworks2026.exe`
  に `DumpType=2`。`%LOCALAPPDATA%\CrashDumps\` に 1 つ 2.5 GB 前後の完全なダンプが書き込まれる）を読み取る。
  VM ごと停止したかは System のイベント 1001（バグチェック）で確認する。

## 結果の移転先

その際の実測（上の環境。プラグインは `0683df0` と `296a797`）は、次の調査で `Findings/` に反映しました。

| 実測 | 移転先 |
| --- | --- |
| 拡張機能を登録しないプラグインが読み込まれ、受付処理を行うこと | [Findings「Plug-in Modules」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Plug-in%20Modules.md)の「拡張機能を 1 つも登録しないモジュールも、有効なら起動時から常駐する」（[#252](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/252)） |
| 終了時の `0xC0000005`（既にアンロードされた `libcef.dll` の内部で異常終了する） | 同じページの「OS タイマーを持つ常駐プラグイン（留めたモジュール）は、終了時の落ち方を変えない」（[#257](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/257)） |
| 新しい図面の最初の描画中に `MaxonDocumentBuilder.vlb` の内部で異常終了した 1 回 | 同じページの「新しい図面の最初の描画——30 回で 1 度も落ちなかった」（[#270](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/270)） |
| VM ごとの停止（`viogpudo.sys` のバグチェック `0xD1`） | 同じページの「【罠】Windows の VW は起動時にモジュールを一度降ろして読み込み直す」の「読み違えやすいところ」 |
| 開発元不明のプラグイン | 同じページの「開発元不明のプラグイン」のダイアログ（[#258](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/258)・[#264](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/264)）。このプラグインでの扱いは[構成「拡張機能の非登録」](architecture.md#拡張機能の非登録) |

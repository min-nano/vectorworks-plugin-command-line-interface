# Windows の実機での実測（段 2）

段 2 の実機確認で、Windows の Vectorworks の起動・終了と、プラグインの有無による違いを測った記録です。
**解決していないことを含みます**（下記「分かっていないこと」）。結論のうち設計に関わるものは
[SDK の調査 0](../sdk-research.md#0-拡張機能を-1-つも登録しないプラグインの読み込み) にも書いています。

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

## 結果

### プラグインが読み込まれ、受け付けること

- 留めた版は、起動から 10〜50 秒前後で `cli.vlb` がプロセスに載り、`vw2026 status` が `running:true` に
  なってから 0.2〜2.4 秒で `call ping` が応えた。`running:true` になるまでは起動から 36〜308 秒だった
  （ホーム画面の前のこともある）。
- `ping` は図面が無いとき `document_open:false`・`current_layer:null`、新しい図面を開いたとき
  `document_open:true`・`current_layer:"レイヤ-1"` を返した。

### 起動・終了

| プラグイン | 図面 | 回数 | 終了コード 0 | `0xC0000005` | 途中で落ちた |
| --- | --- | --- | --- | --- | --- |
| `0683df0` | 開かない | 5 | 5 | 0 | 0 |
| `296a797` | 開かない | 11 | 8 | 3 | 0 |
| `296a797` | 新しい図面を開いて閉じる | 2 | —（1 回は正常に閉じたが終了コードを読めなかった。ダンプ無し） | 0 | 1（最初の描画中） |
| なし | 開かない | 4 | 4 | 0 | 0 |
| なし | 新しい図面を開いて閉じる | 2 | 2 | 0 | 0 |

- **終了時の `0xC0000005`** は、閉じる指示から終わるまでに 45 秒前後かかり、Sentry のダンプは残らなかった。
  LocalDumps で取った 1 件では、落ちたスレッドは**すでに外された `libcef.dll` の中を実行していた**
  （例外は実行の違反で、番地はどの読み込まれたモジュールにも入らず、外されたモジュールの一覧の
  `libcef.dll` の範囲に入る。スレッドの開始番地も同じ範囲）。スタックは `kernel32.dll` と `ntdll.dll` の
  スレッドの始まりだけで、`cli.vlb` の番地は無かった。`cli.vlb` は落ちた時点でも読み込まれたままだった。
  → **Vectorworks の終了の途中で CEF のスレッドが残ったまま `libcef.dll` が外された**と読める。
- **新しい図面の最初の描画中に落ちた 1 回**は、`VW User Log.txt` が「ホーム画面を閉じる → 新しい図面 →
  Direct3D 11 の初期化 → `PaintArea begin`」の直後に Sentry へ送っている。Sentry のダンプの例外は
  `MaxonDocumentBuilder.vlb` の中での番地 0 の読み出しで、落ちたスレッドのスタックには Direct3D 11・
  WARP・Cineware・Maxon のモジュールが並び、`cli.vlb` の番地は無かった。

### VM ごとの停止

起動や終了の前後で、VM ごと止まったことが 2 回あった（バグチェック `0xD1`。番地は前の記録と同じ
`viogpudo.sys` のもの。UTM の virtio GPU ドライバ。SDK リファレンスの
[Findings「Plug-in Modules」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Plug-in%20Modules.md)
の「読み違えやすいところ」）。そのときの Vectorworks の終了コードは取れていない。

### 開発元不明のプラグイン

- 新しい `cli` を置いた最初の起動で「開発元不明のプラグイン」のダイアログが出て、`cli` は「無効」と
  示され読み込まれなかった。「有効化」の後の起動から読み込まれた。
- ファイルを別のビルドへ入れ替えても「有効」は残った。入れ替えの後の 2 回の起動でダイアログは出たが、
  `cli` は読み込まれ、ダイアログが開いている間も `call ping` が応えた。その後、画面を確かめた起動では出なかった
  （入れ替えの後の 1 回目は図面の描画中に落ちて終わっている）。
- ダイアログは「認識されないプラグインがインストールされている限り、この警告は起動のたびに表示され
  続けます」と書いているが、毎回は出なかった。

## 分かっていないこと

- **終了時の `0xC0000005` とプラグインの関係。** 落ちたのは `libcef.dll` の後始末で、`cli.vlb` は
  関わっていない。ただし、プラグインありの 11 回中 3 回に対し、なしは 6 回中 0 回で、プラグインが
  終了の順序や時間を変えて起こりやすくしているかは、回数が少なく判断できない。
  [SDK リファレンス #257](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/257)
  で調べる。
- **新しい図面の描画中に落ちたこととプラグインの関係。** 1 回だけで、なしでは 2 回とも落ちていない。
- 開発元不明のダイアログが出る・出ないの条件。
- この VM（ARM64 上の x64 エミュレーション・WARP の描画・GPU のドライバ）に固有のことかどうか。
  x64 の実機では測っていない。

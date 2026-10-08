# 構成

## 殻と本体

```
Vectorworks ──読み込む──▶ 殻 cli.vwlibrary / cli.vlb          … 起動時に 1 度だけ
                              │ 複製を dlopen / LoadLibrary
                              ▼
                          本体 cli.vwpayload                   … いつでも降ろして読み直せる
```

| | 入るもの | 入れないもの |
| --- | --- | --- |
| **殻** | 本体の読み込み（複製・入れ替えの判定）・**OS タイマー**・**殻に頼む道具の実行**（終了・再起動）・本体を動かせない間の[殻の診断](abi.md#殻の診断shelljson)（`shell.json`） | 道具の中身・スプールの要求と応答と生存の印・JSON の組み立て（診断の決まった形を除く）・更新（CLI が行う） |
| **本体** | ブリッジ（スプールの受け付け）・道具の表と中身・生存の印 | 登録の定義。本体は `.vwr` を持たない |

元のプラグインの決めごとをそのまま守ります（元:`CLAUDE.md`「殻と本体」）。

1. **境界は C の ABI。** 例外・C++ のオブジェクト・`std::string` を越えて渡さない（[ABI](abi.md)）。
2. **境界を越えた構造体は、受け取った側がその場で複製し、渡した側もアンロードまで保持する。**
3. **本体のコードがスタックに載っている間はアンロードしない。** 入れ替えの判定は入口で、
   入れ子の深さが 0 のときだけ（元:`src/PayloadSession.h` の `PayloadUse`）。
4. **本体は一時ディレクトリへ複製してから読み込む**（Windows は読み込み中の DLL を置き換えられない）。
   **本体が変わったかは、本体のファイルの刻印で判定する。** 刻印は更新時刻・大きさ・ファイルの
   識別子（mac は `stat` の `st_dev` と `st_ino`、Windows は `GetFileInformationByHandle` の
   volume serial number と file index）の組です。インストーラは本体を名前の変更で置き換え、
   そのとき更新時刻と大きさは元と同じことがあるので、識別子を含めて変化を見逃さないようにする
   （[更新「殻の ID が同じとき」](install-and-update.md#殻の-id-が同じときファイルの上書き)）。
   Windows で刻印を取るときは `CreateFileW` を `FILE_SHARE_READ | FILE_SHARE_WRITE |
   FILE_SHARE_DELETE` で開き、取ったらすぐ閉じる（OS タイマーのたびに開くので、インストーラの
   名前の変更と共有違反を起こさないようにする）。本体を一時ディレクトリへ複製するときの読み取りも
   同じ共有モードで開く。
5. **本体をバンドルの中に置かない**（mac の署名の対象がリソースにまで及ぶ）。殻の隣に置く。
6. **殻の ID（`VW_SHELL_ID`）が「再起動が要るか」を決める。** 殻にコンパイルされるもの
   だけを `VW_SHELL_INPUTS` に並べる（[ビルド](build-and-release.md#殻の-id)）。
   **殻は、フォルダの `shell-id` が自分の ID と一致するときだけ本体を読み直す**（一致しなければ
   今の本体のまま動き、`VwServeInput::restartRequired` で本体に伝える。本体が生存の印に `restart_required` を出す。[更新「別の手段で再起動されたとき」](install-and-update.md#別の手段で再起動されたとき)）。
7. 殻は SDK に依らない共通部（`src/core/`）をリンクしない（殻に入れてよいものの境界を保つ）。

## 入口

| 入口 | 殻の処理 | 本体へ |
| --- | --- | --- |
| `plugin_module_main`（起動時） | VCOM の初期化・SDK のコールバックを記憶・**OS タイマーの開始**（拡張機能は登録しない） | なし（本体は最初の受け付けで読み込む） |
| OS タイマー（250 ms ごと） | 見送りの判定 → `PayloadUse` で本体を確保 → `vw_payload_serve` → 殻に頼む道具を実行 | `vw_payload_serve` |

**プラグインは更新を確認しません。** 更新は CLI の `vw2026 update` だけが行います。
OS タイマーは起動時に開始します（最初の 10 秒は受け付けない）。

## 受け付けの流れ

```
OS タイマー（殻）
  ├─ gSDK が無い・開始から 10 秒以内 → 何もしない
  ├─ undo の記録が開いている（IsCurrentlyBuildingAnUndoEvent）→ 見送る
  ├─ 本体が使用中（PayloadInUse）・入れ子（受け付け中にまた呼ばれた）→ 見送る
  └─ PayloadUse（入れ替えの判定・読み込み）
        ├─ 読み込めない・ABI が違う・初期化に失敗 → shell.json を書く（2 秒ごと）
        └─ vw_payload_serve({shellReport, restartRequired}, &out)   … 本体
              ├─ 前の回の殻の結果（shellReport）を応答として書く
              ├─ 要求を名前の昇順で取り出す（1 回 16 件まで）
              ├─ 道具を実行して応答を書く／殻に頼む道具は action として返す
              ├─ 生存の印を 2 秒ごとに書き直す
              └─ 見え方（view）の JSON を out へ
     action があれば（1 回のタイマーで 3 件まで）
        ├─ 殻が実行する（quit）
        └─ 結果を shellReport にして、すぐ vw_payload_serve をもう一度呼ぶ
           （入れ替わったなら新しい本体が応答を書く）
     quit が頼まれていたら、応答を書かせてから CloseAllFilesAndQuitVectorworks
```

- **SDK 呼び出しはメインスレッドからだけ**行います。受け付け用のスレッドは持ちません。
- **本体の中にループを書きません**（図面が操作できなくなる）。長く走る道具は、走る前に
  生存の印へ `busy` / `busy_id` / `busy_until` を書きます。
- タイマーの実装は元:`src/Extensions/ExtMcpPalette.cpp` の `StartMcpBridgeClock` / `ClockTick` /
  `ServeOnce` / `RunShellAction` を移します。mac は `CFRunLoopTimer` を
  **`kCFRunLoopDefaultMode` にだけ**登録します（common modes だとモーダルダイアログの最中にも
  呼ばれ、開いている undo の記録へ書き込みが混ざる）。Windows はスレッドタイマー
  （`SetTimer(nullptr, 0, 250, …)`）。経緯は元:`docs/dev-notes/milestones/m41-bridge-os-timer.md`。
- 元のプラグインにあったパレット（JS タイマー）は**持ちません**。M41 で受け付けは OS タイマーへ
  移っており、パレットは状態を見せるだけでした。状態は `vw2026 status` で見ます。

## 受け付けを止めない・処理中は見送る

**利用者が受け付けを止める手段は持ちません**（止めたければアンインストールする）。代わりに、
Vectorworks の処理の妨げにならないよう、次のときはその回の受け付けを**見送ります**。

| 見送る条件 | 理由 |
| --- | --- |
| undo の記録が開いている（`gSDK->IsCurrentlyBuildingAnUndoEvent()`） | その間の書き込みは開いている記録へ混ざる。VW のモーダルダイアログの最中はここに当たる（[Findings「Timers and Notifications」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Timers%20and%20Notifications.md) の 6・7） |
| 本体が使用中（`PayloadInUse()`）・受け付けの入れ子 | 本体のコードがスタックに載っている（長く走る道具の最中など） |
| 開始から 10 秒以内 | Vectorworks の起動処理と重ねない |

- 何も要求が無いときの 1 回の費用は、スプールの一覧を読むことと、2 秒に 1 回の生存の印の
  書き直しだけです。利用者がドラッグやレンダリングをしている最中にも刻みは届きますが、
  そのとき VW は undo の記録を開いていないので、読む道具はそのまま応えます（同 Findings の表）。
- mac は `kCFRunLoopDefaultMode` にだけ登録するので、モーダルの最中はそもそも刻みません。
  **Windows の `SetTimer` はモーダルの最中も刻む**ので、上の undo の条件が見送りを担います。
- 見送った要求は消さずに残るので、次に受け付けたときに処理されます（呼ぶ側の待ち時間の内なら
  応答が届く。過ぎれば呼ぶ側が取り下げる）。
- 見送りの間は生存の印も書き直されません。15 秒を超えると印は古びますが、呼ぶ側は `pid` の
  プロセスが動いていれば「応えない（`unresponsive`）」と判定し、「止まっている」とは誤りません
  （[作法「生存の判定」](../protocol.md#生存の判定)）。

## 拡張機能を登録しない

**メニューもパレットも PIO も登録しません。** 操作はすべて CLI から行い、更新も CLI が行う
（[更新](install-and-update.md#更新vw2026-update)）ので、殻に利用者の入口は要りません。

- プラグインの読み込みと `plugin_module_main` の呼び出しは、拡張機能の有無に関わらず起きる
  見込みです（PIO だけのプラグインもあるように、読み込みは登録の種類に依らない）。ただし
  **拡張機能を 1 つも登録しない形は実機で確かめていません**（SDK リファレンスにも記載が無い）。
  段 2（骨格）の実機確認で、起動後に `vw2026 status` が `live:true` になることで確かめます。
- **確かめられなかったときの代わり**: 拡張機能を 1 つだけ登録します。メニューコマンドは
  ワークスペースに加えない限り利用者の画面に出ないので、登録しても目に触れません。
  そのときは UUID とユニバーサル名を [識別子](identifiers.md) に足します。

## ソースの配置

```
src/
├─ PluginPrefix.h / BuildConfig.h          … PCH・名前と識別子（殻と本体で共有）
├─ ModuleMain.cpp                          … 殻: 登録・タイマーの開始
├─ PayloadAbi.h                            … 境界（殻と本体で共有。SDK を include しない）
├─ PayloadHost.{h,cpp} / PayloadSession.{h,cpp}   … 殻: 本体の読み込み・入れ替え
├─ ShellDiag.{h,cpp}                       … 殻: 殻の診断（shell.json）
├─ SpoolDir.{h,cpp}                        … スプールの場所と持ち主・権限の確かめ（殻と本体で共有。SDK に依らない）
├─ PayloadHostHolder.h                     … 本体: 受け取った VwPayloadHost の複製
├─ Clock.{h,cpp}                           … 殻: OS タイマー・殻に頼む道具の実行
├─ payload/PayloadMain.cpp                 … 本体: エクスポート関数
├─ core/                                   … SDK に依らない（Json・Bridge）。本体とテストがリンク
└─ tools/                                  … 本体: 道具の表（ToolTable.cpp）と中身（SDK 依存）
scripts/                                   … vw-install / vw-uninstall（.sh / .ps1）
resources/                                 … cli.vwr / cli_dev.vwr（拡張機能を登録しないので最小限）
tests/                                     … 無 SDK の単体テスト・スクリプトのテスト
protocol/fixtures/                         … 作法の見本（C++ と Go の両方のテストが読む）
cli/                                       … vw2026（Go）。更新（update）もここ
```

- 名前空間は `VwCli`（`VwCli::core` / `VwCli::tools` / `VwCli::payload`）。図面にも配布物にも
  現れない内部の綴りで、改名しても据え置きます。
- 元の `draw/` に当たるものは `tools/` にします（このプラグインは描画しない）。

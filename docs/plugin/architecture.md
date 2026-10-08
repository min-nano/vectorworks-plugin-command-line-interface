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
| **殻** | Vectorworks にアドレスを保持されるものの登録（メニューの `SMenuDef`・UUID）・本体の読み込み（複製・入れ替えの判定）・**OS タイマー**・**殻に頼む道具の実行**（更新・再起動）・自動アップデート | 道具の中身・スプールの読み書き・JSON の組み立て |
| **本体** | ブリッジ（スプールの受け付け）・道具の表と中身・生存の印 | 登録の定義。本体は `.vwr` を持たない |

元のプラグインの決めごとをそのまま守ります（元:`CLAUDE.md`「殻と本体」）。

1. **境界は C の ABI。** 例外・C++ のオブジェクト・`std::string` を越えて渡さない（[ABI](abi.md)）。
2. **境界を越えた構造体は、受け取った側がその場で複製し、渡した側もアンロードまで保持する。**
3. **本体のコードがスタックに載っている間はアンロードしない。** 入れ替えの判定は入口で、
   入れ子の深さが 0 のときだけ（元:`src/PayloadSession.h` の `PayloadUse`）。
4. **本体は一時ディレクトリへ複製してから読み込む**（Windows は読み込み中の DLL を置き換えられない）。
5. **本体をバンドルの中に置かない**（mac の署名の対象がリソースにまで及ぶ）。殻の隣に置く。
6. **殻の ID（`VW_SHELL_ID`）が「再起動が要るか」を決める。** 殻にコンパイルされるもの
   だけを `VW_SHELL_INPUTS` に並べる（[ビルド](build-and-release.md#殻の-id)）。
7. 殻は SDK に依らない共通部（`src/core/`）をリンクしない（殻に入れてよいものの境界を保つ）。

## 入口

| 入口 | 殻の処理 | 本体へ |
| --- | --- | --- |
| `plugin_module_main`（起動時） | VCOM の初期化・SDK のコールバックを記憶・拡張機能の登録・**OS タイマーの開始** | なし（本体は最初の受け付けで読み込む） |
| OS タイマー（250 ms ごと） | 見送りの判定 → `PayloadUse` で本体を確保 → `vw_payload_serve` → 殻に頼む道具を実行 | `vw_payload_serve` |
| メニュー「アップデートを確認 (CLI)」 | 自動アップデートの流れ（`UpdaterFlow`） | なし |

**起動時に更新を確認しません。** 元のプラグインと同じく、確認はメニューと `update` 道具の
ときだけです（再起動を SDK に頼めるのは Vectorworks が完全に動いている最中だけ）。
OS タイマーの開始は確認ではないので起動時に行います（最初の 10 秒は受け付けない）。

## 受け付けの流れ

```
OS タイマー（殻）
  ├─ gSDK が無い・開始から 10 秒以内 → 何もしない
  ├─ undo の記録が開いている（IsCurrentlyBuildingAnUndoEvent）→ 見送る
  ├─ 本体が使用中（PayloadInUse）・入れ子（受け付け中にまた呼ばれた）→ 見送る
  └─ PayloadUse（入れ替えの判定・読み込み）
        └─ vw_payload_serve(shellReport, &out)        … 本体
              ├─ 前の回の殻の結果（shellReport）を応答として書く
              ├─ 要求を名前の昇順で取り出す（1 回 16 件まで）
              ├─ 道具を実行して応答を書く／殻に頼む道具は action として返す
              ├─ 生存の印を 2 秒ごとに書き直す
              └─ 見え方（view）の JSON を out へ
     action があれば（1 回のタイマーで 3 件まで）
        ├─ 殻が実行する（update / restart）
        └─ 結果を shellReport にして、すぐ vw_payload_serve をもう一度呼ぶ
           （入れ替わったなら新しい本体が応答を書く）
     restart が頼まれていたら、応答を書かせてから CloseAllFilesAndQuitVectorworks
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

## メニューを持つ理由

メニューは「アップデートを確認」の 1 つだけを持ちます。

- **本体が読み込めないときの復旧の経路**になるため。殻まで変わる更新のあと、新しい本体を古い殻が
  読めない（ABI の版が違う）と、ブリッジは動かず `vw2026 call update` が届きません。殻だけで
  動くメニューなら、そこから入れ直せます。
- **拡張機能を 1 つも登録しないプラグインが読み込まれ、`plugin_module_main` が呼ばれるかは
  確かめていません**（Findings に無い）。タイマーの開始は元のプラグインと同じく
  `plugin_module_main` で行うので、実機で通った形（メニューを登録する）に揃えます。

## ソースの配置

```
src/
├─ PluginPrefix.h / BuildConfig.h          … PCH・名前と識別子（殻と本体で共有）
├─ ModuleMain.cpp                          … 殻: 登録・タイマーの開始
├─ PayloadAbi.h                            … 境界（殻と本体で共有。SDK を include しない）
├─ PayloadHost.{h,cpp} / PayloadSession.{h,cpp}   … 殻: 本体の読み込み・入れ替え
├─ PayloadHostHolder.h                     … 本体: 受け取った VwPayloadHost の複製
├─ Clock.{h,cpp}                           … 殻: OS タイマー・殻に頼む道具の実行
├─ Updater.{h,cpp} / UpdaterFlow.cpp / UpdaterHost.h / UpdaterParse.h   … 殻: 自動アップデート
├─ Extensions/                             … 殻: メニューの拡張機能
├─ payload/PayloadMain.cpp                 … 本体: エクスポート関数
├─ core/                                   … SDK に依らない（Json・Bridge）。本体とテストがリンク
└─ tools/                                  … 本体: 道具の表（ToolTable.cpp）と中身（SDK 依存）
scripts/                                   … vw-install / vw-uninstall / vw-update / vw-token（.sh / .ps1）
resources/                                 … cli.vwr / cli_dev.vwr（文字列）
tests/                                     … 無 SDK の単体テスト・スクリプトのテスト
protocol/fixtures/                         … 作法の見本（C++ と Go の両方のテストが読む）
cli/                                       … vw2026（Go）
```

- 名前空間は `VwCli`（`VwCli::core` / `VwCli::tools` / `VwCli::payload`）。図面にも配布物にも
  現れない内部の綴りで、改名しても据え置きます。
- 元の `draw/` に当たるものは `tools/` にします（このプラグインは描画しない）。

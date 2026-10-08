# 殻と本体の境界（`src/PayloadAbi.h`）

元:`src/PayloadAbi.h`（ABI 7）から、このプラグインに要るものだけを残した**新しい系列の版 1**
です。元のプラグインと本体を取り違えることはありません（ファイル名が違う）が、版は 1 から
数え直します。

## 決めごと

- **C の ABI だけ。** SDK・OS のヘッダを include しない。SDK の型は `void*` で渡す。
- 返す `const char*` は**次に本体を呼ぶまで・アンロードまで**有効。殻はすぐ複製する。
- **例外は境界を越えない。** 本体のエクスポート関数はすべて本体を try/catch で包み、状態コードで返す。
- **境界の形を変えたら `VW_PAYLOAD_ABI_VERSION` を上げる。** 殻と本体は別々に配られうる
  （本体だけ入れ替わる）ので、食い違いは実行時にしか分からない。殻は版が合わない本体を
  読み込まず、「プラグインを入れ直してください」と見え方に出す。

## 版

```c
#define VW_PAYLOAD_ABI_VERSION 1u
```

## 殻が本体へ渡すもの

```c
struct VwPayloadHost {
    unsigned size;         // sizeof(VwPayloadHost)。二重の確かめ
    unsigned abiVersion;   // VW_PAYLOAD_ABI_VERSION
    void*    callbacks;    // SDK の CallBackPtr。本体は GS_InitializeVCOM に渡す
};
```

- 元にあった `runBundledScript`（同梱スクリプトの実行）は**持ちません**。本体に同梱
  スクリプトを走らせる用が無いため（更新は殻が行う）。要るようになったら版を上げて足します。
- 本体は受け取った構造体を `PayloadHostHolder` で複製します（元:`src/PayloadHostHolder.h`）。
  殻は `Payload` のメンバとしてアンロードまで保持します。

## 本体の素性

```c
struct VwPayloadInfo {
    unsigned    size;
    const char* version;   // VW_BUILD_VERSION（短い sha）
    const char* branch;    // VW_BUILD_BRANCH
};
```

## エクスポートする関数

| 名前 | 型 | 役目 |
| --- | --- | --- |
| `vw_payload_abi_version` | `unsigned (void)` | 版を返す。殻はまずこれを確かめる |
| `vw_payload_init` | `int (const VwPayloadHost*)` | 受け取りを複製し、VCOM を初期化する |
| `vw_payload_info` | `int (VwPayloadInfo*)` | 素性を返す |
| `vw_payload_serve` | `int (const char* shellReport, const char** out)` | 受け付けを 1 回行い、見え方（view）の JSON を `out` へ |
| `vw_payload_shutdown` | `void (void)` | アンロードの直前。生存の印は消さない（下記） |

- 殻はこの 5 つを**すべて必須**として解決します。1 つでも無ければ読み込み失敗です。
- 元にあった `vw_payload_run_import` / `run_test` / `recalculate` と PIO の種別は持ちません。

### `vw_payload_shutdown` で生存の印を消さない理由

アンロードは**入れ替え**のときにも起きます。そこで印を消すと、入れ替えの間に呼ぶ側が
「止まった」と判断します。印は古びれば「動いていない」と判定されるので、消すのは
「受け付けを停止したとき」だけにします（[ブリッジ](bridge.md#生存の印)）。

## 状態コード

```c
enum VwPayloadStatus {
    kVwPayloadOk = 0, kVwPayloadErrAbi = 1, kVwPayloadErrHost = 2, kVwPayloadErrVcom = 3,
    kVwPayloadErrNotInit = 4, kVwPayloadErrException = 5
};
```

## `vw_payload_serve` の約束（殻に頼む道具）

本体は自分をアンロードできないので、更新と再起動は殻が行います（元の M38・ABI 7 と同じ）。

1. 本体は要求の中に殻に頼む道具（[道具の表](bridge.md#道具の表)の種類 `shell`）を見つけたら、
   **応答を書かずに**見え方へ `"action":{"id","tool","args"}` を載せて返す。1 回の呼び出しで
   受け取るのは 1 件だけで、2 件目には「別の頼みごとを処理しています」と失敗で応える。
2. 殻は action を実行し、結果 `{"id","ok","result","error"}` を次の呼び出しの
   `shellReport` に渡す。
3. **そのとき読み込まれている本体**（入れ替えたなら新しい本体）が応答を書き、見え方に
   `"reportDone":true` を立てる。結果には書いた本体の `payload_version` を添える。
4. 殻は `reportDone` が立つまで（最長 120 秒）同じ結果を渡し直す。

// Package edition は、CLI が対象とする Vectorworks の版と、版から決定される名前を保持する。
//
// 版ごとに別個の CLI をビルドする（docs/plugin/editions.md）。既定は 2026 で、ビルドタグ vw2025 を
// 付けると 2025 になる。コマンド名・環境変数・配置場所・既定のレジストリが版ごとに区別されるので、
// 2025 と 2026 の Vectorworks が同一の計算機に存在しても、CLI 同士が衝突しない。
package edition

// Command はコマンド名（vw2026）。実行ファイル・出力の接頭辞・ヘルプ・mac のリンクの名前になる。
const Command = "vw" + Year

// EnvPrefix は、指定の環境変数とフックに渡す環境変数の名前の接頭辞（VW2026_）。
const EnvPrefix = "VW" + Year + "_"

// HookEnvPrefix はフックに渡す環境変数の名前の接頭辞（VW2026_HOOK_）。
const HookEnvPrefix = EnvPrefix + "HOOK_"

// Product は Vectorworks の名前（Vectorworks 2026）。mac の既定のアプリの名前でもある。
const Product = "Vectorworks " + Year

// Manifest は cli のマニフェストのファイル名（vw2026.json）。
const Manifest = Command + ".json"

// Bootstrap は初回スクリプトの名前（拡張子なし。get-vw2026）。
const Bootstrap = "get-" + Command

// Vars は、ヘルプとコマンドの定義（kong のタグ）に挿入する値。${cmd}・${Cmd}（文頭）・
// ${year}・${prefix}・${product}・${bootstrap}・${manifest} と記述する（kong が予約した
// ${default}・${env} とは重複させない）。
func Vars() map[string]string {
	return map[string]string{
		"cmd":       Command,
		"Cmd":       "Vw" + Year,
		"year":      Year,
		"prefix":    EnvPrefix,
		"product":   Product,
		"bootstrap": Bootstrap,
		"manifest":  Manifest,
	}
}

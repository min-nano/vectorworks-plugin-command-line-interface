// Package edition は、CLI が対象にする Vectorworks の版と、版から決まる名前を持つ。
//
// 版ごとに別の CLI をビルドする（docs/plugin/editions.md）。既定は 2026 で、ビルドタグ vw2025 を
// 付けると 2025 になる。コマンド名・環境変数・置き場所・既定のレジストリが版ごとに分かれるので、
// 2025 と 2026 の Vectorworks が同じ計算機にあっても、CLI どうしが衝突しない。
package edition

// Command はコマンド名（vw2026）。実行ファイル・出力の頭・ヘルプ・mac のリンクの名前になる。
const Command = "vw" + Year

// EnvPrefix は、指定の環境変数とフックに渡す環境変数の名前の頭（VW2026_）。
const EnvPrefix = "VW" + Year + "_"

// HookEnvPrefix はフックに渡す環境変数の名前の頭（VW2026_HOOK_）。
const HookEnvPrefix = EnvPrefix + "HOOK_"

// Product は Vectorworks の名前（Vectorworks 2026）。mac の既定のアプリの名前でもある。
const Product = "Vectorworks " + Year

// Manifest は cli のマニフェストのファイル名（vw2026.json）。
const Manifest = Command + ".json"

// Bootstrap は初回のスクリプトの名前（拡張子なし。get-vw2026）。
const Bootstrap = "get-" + Command

// Vars は、ヘルプとコマンドの定義（kong のタグ）に差し込む値。${cmd}・${Cmd}（文の頭）・${year}・${prefix}・
// ${product}・${bootstrap}・${manifest} と書く（kong の予約した ${default}・${env} とは重ねない）。
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

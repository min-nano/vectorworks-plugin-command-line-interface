//go:build vw2025

package registry

import _ "embed"

// defaultJSON は既定のレジストリ。CLI に埋め込み、必ず先頭に引く（docs/registry.md「レジストリの順」）。
// 版ごとに分け、cli はその版のマニフェスト（vw2025.json）を指す（docs/plugin/editions.md）。
//
//go:embed default-vw2025.json
var defaultJSON []byte

//go:build vw2025

package registry

import _ "embed"

// defaultJSON は既定のレジストリ。CLI に埋め込み、必ず最初に参照する
// （docs/registry.md「レジストリの優先順位」）。版ごとに分割し、cli はその版の
// マニフェスト（vw2025.json）を指す（docs/plugin/editions.md）。
//
//go:embed default-vw2025.json
var defaultJSON []byte

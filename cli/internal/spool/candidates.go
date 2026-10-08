package spool

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// スプールのディレクトリ名。安定版と開発版のプラグインが同居しても取り違えないよう、
// 開発版は別の名前にする（docs/protocol.md「スプールの場所」）。
const (
	StableSpoolName = "vectorworks-cli-bridge"
	DevSpoolName    = "vectorworks-cli-bridge-dev"
)

// SpoolName は配布の系列（"stable" / "dev"）からスプールのディレクトリ名を返す。
// 知らない系列なら空。
func SpoolName(channel string) string {
	switch channel {
	case "", "stable":
		return StableSpoolName
	case "dev":
		return DevSpoolName
	default:
		return ""
	}
}

// Candidates はスプールの候補を確からしい順に返す。
//
// channel は "stable"（既定）か "dev"。知らない系列なら候補は空。
// override（VW2026_SPOOL）が空でなければそれだけを返す。そうでなければ、プラグイン側が
// 一時ディレクトリを決めるのと同じ仕組みから出した場所だけを並べる——利用者ごとの
// 一時ディレクトリ（macOS の DARWIN_USER_TEMP_DIR）と、環境変数（TMPDIR / TMP / TEMP）。
//
// **推測による候補は持たない**（/tmp への退避や /var/folders の総当たりはしない）。
// 前者は誰でも書ける場所で、後者は同じ利用者の別のログインのブリッジに繋がりうる。
// 理由: 一時ディレクトリは環境変数で決まるので、GUI アプリの Vectorworks と、ssh や
// 別のアプリから起動された CLI とで食い違いうる（macOS の $TMPDIR は GUI には渡るが、
// 渡らない起動経路がある）。探すのは CLI 側の役割で、プラグインは自分の一時ディレクトリへ
// 素直に置く。
func Candidates(channel, override string) []string {
	if override != "" {
		return []string{override}
	}
	name := SpoolName(channel)
	if name == "" {
		return nil
	}
	var roots []string
	add := func(root string) {
		root = strings.TrimRight(root, `/\`)
		if root == "" {
			return
		}
		for _, existing := range roots {
			if existing == root {
				return
			}
		}
		roots = append(roots, root)
	}

	userTemp := darwinUserTempDir()
	add(userTemp)
	add(os.TempDir())
	for _, name := range []string{"TMPDIR", "TMP", "TEMP"} {
		add(os.Getenv(name))
	}
	if userTemp != "" {
		// macOS で利用者ごとの場所が取れたなら /tmp は調べない（GUI アプリの一時
		// ディレクトリになりえない）。
		kept := roots[:0]
		for _, root := range roots {
			if root != "/tmp" {
				kept = append(kept, root)
			}
		}
		roots = kept
	}

	out := make([]string, 0, len(roots))
	for _, root := range roots {
		out = append(out, filepath.Join(root, name))
	}
	return out
}

// darwinUserTempDir は macOS の利用者ごとの一時ディレクトリ（/var/folders/…/T/）を
// 環境変数に頼らずに取得する。この値は利用者から決まるので、GUI アプリの Vectorworks と
// 必ず一致する。macOS 以外・取得できなければ空。
func darwinUserTempDir() string {
	if runtime.GOOS != "darwin" {
		return ""
	}
	out, err := exec.Command("/usr/bin/getconf", "DARWIN_USER_TEMP_DIR").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

#!/bin/sh
# 初回のスクリプト（mac）。取ってきたリリースの CLI を取り出して vw2026 install cli@<そのリリースのタグ>
# （または uninstall cli）を走らせるだけで、配置の手順・PATH・判断は install と cli のフックが持つ
# （docs/plugin/install-and-update.md「初回のスクリプト」）。入れる・外すタグは、スクリプトを取ってきた
# リリースのタグに決まっている（ほかのタグはそのタグのスクリプトを取る）。
#
#   curl -fsSL https://github.com/min-nano/vectorworks-plugin-command-line-interface/releases/latest/download/get-vw2026.sh | sh
#   curl -fsSL …/releases/download/<tag>/get-vw2026.sh | sh                          # そのタグを入れる
#   curl -fsSL …/get-vw2026.sh | sh -s -- --plugins-dir <Plug-Ins>                   # 既定以外の場所へ
#   curl -fsSL …/releases/download/<tag>/get-vw2026.sh | sh -s -- uninstall          # そのタグの CLI で外す
#
# 本体は関数に入れ、最後の行で呼ぶ。パイプで受け取る途中で切れたときに、途中までを走らせないため。
set -eu

repo="https://github.com/min-nano/vectorworks-plugin-command-line-interface"

fail() {
	echo "get-vw2026: $*" >&2
	exit 1
}

# タグに使える文字だけか（docs/registry.md「名前・タグ・ターゲット」）。リリースが置き換えていない
# スクリプト（リポジトリのものをそのまま走らせた）を断り、URL に埋め込めることを確かめる。
valid_tag() {
	case "$1" in
	"" | [!A-Za-z0-9]* | *[!A-Za-z0-9._-]*) return 1 ;;
	*) return 0 ;;
	esac
}

main() {
	# タグは、リリースが公開するときにそのリリースのタグに置き換える（docs/plugin/build-and-release.md
	# 「リリース」）。どのリリースから取ったスクリプトも、そのリリースの cli を入れる・外す。
	tag='@VW2026_TAG@'
	latest="$repo/releases/latest/download/get-vw2026.sh"
	valid_tag "$tag" || fail "this script is not the one a release published; use $latest"

	# 位置の引数は uninstall（uninstall cli）だけ、指定は --plugins-dir（--plugins-dir=<dir> も）だけを
	# 受け付ける。--registry は持たない（install は既定のレジストリから先に引き、cli は既定のレジストリに
	# あるので、ほかのレジストリは使われない。uninstall はレジストリを読まない）。指定は、書いた位置に依らず
	# CLI のコマンドの後ろへそのまま渡す（vw2026 はコマンドの前の指定を受け付けない）。
	command=install
	positional=0
	skip=false
	for arg in "$@"; do
		shift
		if [ "$skip" = true ]; then
			skip=false
		else
			case "$arg" in
			--plugins-dir) skip=true ;;
			--plugins-dir=*) ;;
			-*) fail "unknown flag: $arg (this script takes only --plugins-dir)" ;;
			*)
				positional=$((positional + 1))
				if [ "$positional" -eq 1 ] && [ "$arg" = uninstall ]; then
					command=uninstall
					continue
				fi
				# uninstall cli とも書ける（vw2026 uninstall cli と同じ形）。
				if [ "$positional" -eq 2 ] && [ "$command" = uninstall ] && [ "$arg" = cli ]; then
					continue
				fi
				fail "unexpected argument: $arg (this script installs cli@$tag;" \
					"for another tag, use $repo/releases/download/<tag>/get-vw2026.sh)"
				;;
			esac
		fi
		set -- "$@" "$arg"
	done
	if [ "$command" = uninstall ]; then
		set -- uninstall cli "$@"
	else
		set -- install "cli@$tag" "$@"
	fi

	# ターゲットは計算機で決める（Rosetta の下の端末でも、Vectorworks は arm64 で動く。
	# docs/registry.md「名前・タグ・ターゲット」）。
	[ "$(uname -s)" = Darwin ] || fail "this script is for macOS; on Windows, use get-vw2026.ps1"
	if [ "$(sysctl -n hw.optional.arm64 2>/dev/null || true)" = 1 ]; then
		target=macos-arm64
	else
		target=macos-x64
	fi
	# 既定のレジストリの cli の規則と同じ形の場所（マニフェストは読まない。JSON を読まずに済むように）。
	url="$repo/releases/download/$tag/cli-$target.zip"

	tmp="$(mktemp -d "${TMPDIR:-/tmp}/get-vw2026.XXXXXX")"
	trap 'rm -rf "$tmp"' EXIT
	trap 'exit 130' INT
	trap 'exit 143' TERM
	echo "get-vw2026: downloading $url" >&2
	curl -fsSL --retry 3 -o "$tmp/cli.zip" "$url" || fail "cannot download $url"
	# CLI だけを取り出す（実行ビットは zip のものを保つ）。
	unzip -q "$tmp/cli.zip" bin/vw2026 -d "$tmp" || fail "no bin/vw2026 in $url"

	# 標準入力は渡さない（curl | sh では標準入力がこのスクリプトそのもの）。
	code=0
	"$tmp/bin/vw2026" "$@" </dev/null || code=$?
	if [ "$code" -eq 6 ]; then
		echo "get-vw2026: Vectorworks is running. Quit Vectorworks and run this script again." >&2
	fi
	exit "$code"
}

main "$@"

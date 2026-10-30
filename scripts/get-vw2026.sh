#!/bin/sh
# 初回のスクリプト（mac）。CLI を取ってきて vw2026 install cli@<tag>（または uninstall cli）を
# 走らせるだけで、配置の手順・PATH・判断は install と cli のフックが持つ
# （docs/plugin/install-and-update.md「初回のスクリプト」）。
#
#   curl -fsSL https://github.com/min-nano/vectorworks-plugin-command-line-interface/releases/latest/download/get-vw2026.sh | sh
#   curl -fsSL …/get-vw2026.sh | sh -s -- cli@<tag> --plugins-dir <Plug-Ins>
#   curl -fsSL …/get-vw2026.sh | sh -s -- uninstall cli
#
# 本体は関数に入れ、最後の行で呼ぶ。パイプで受け取る途中で切れたときに、途中までを走らせないため。
set -eu

repo="https://github.com/min-nano/vectorworks-plugin-command-line-interface"

fail() {
	echo "get-vw2026: $*" >&2
	exit 1
}

# タグに使える文字だけか（docs/registry.md「名前・タグ・ターゲット」）。URL に埋め込むので確かめる。
valid_tag() {
	case "$1" in
	"" | [!A-Za-z0-9]* | *[!A-Za-z0-9._-]*) return 1 ;;
	*) return 0 ;;
	esac
}

main() {
	# 位置の引数（指定とその値のほか）を順に見る。値を取る指定（--plugins-dir・--registry）の次の
	# 引数は値なので飛ばす。--name=value の形は 1 つの引数で、ここでは指定として飛ばす。
	uninstall=false
	tag=""
	positional=0
	skip=false
	for arg in "$@"; do
		if [ "$skip" = true ]; then
			skip=false
			continue
		fi
		case "$arg" in
		--plugins-dir | --registry)
			skip=true
			continue
			;;
		-*) continue ;;
		esac
		positional=$((positional + 1))
		if [ "$positional" -eq 1 ] && [ "$arg" = uninstall ]; then
			uninstall=true
			continue
		fi
		if [ "$uninstall" = true ]; then
			# uninstall cli がフックの途中で止まり、CLI が消えたときに抜け出すためのもので、cli だけを
			# 受け付ける。
			if [ "$positional" -ne 2 ] || [ "$arg" != cli ]; then
				fail "only \"uninstall cli\" is accepted"
			fi
			continue
		fi
		# このスクリプトは cli を入れるためのもので、ほかの名前は受け付けない。
		case "$arg" in
		cli@*) ;;
		*) fail "only cli@<tag> is accepted: $arg" ;;
		esac
		[ -z "$tag" ] || fail "give cli@<tag> only once"
		tag="${arg#cli@}"
		valid_tag "$tag" || fail "not a valid tag: $tag"
	done
	if [ "$uninstall" = true ]; then
		[ "$positional" -eq 2 ] || fail "only \"uninstall cli\" is accepted"
		# 一時フォルダの CLI は latest のもの（手順 4 から続ける）。
		tag=latest
	else
		if [ -z "$tag" ]; then
			tag=latest
			set -- "$@" cli@latest
		fi
		set -- install "$@"
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
	if [ "$tag" = latest ]; then
		url="$repo/releases/latest/download/cli-$target.zip"
	else
		url="$repo/releases/download/$tag/cli-$target.zip"
	fi

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

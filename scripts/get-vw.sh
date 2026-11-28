#!/bin/sh
# 初回のスクリプト（mac）。取ってきたリリースの CLI を取り出して vw2026 install cli@<そのリリースのタグ>
# （又は uninstall cli）を走らせるだけで、配置の手順・PATH・判断は install と cli のフックが持つ
# （docs/plugin/install-and-update.md「初回のスクリプト」）。入れる・外すタグは、スクリプトを取ってきた
# リリースのタグに決まっている（他のタグはそのタグのスクリプトを取る）。
#
# このファイルは雛形で、リリースがタグと版を書き込んで get-vw2026.sh と get-vw2025.sh を作る
# （docs/plugin/editions.md）。2025 のものは vw2025 を取り出して Vectorworks 2025 の cli を入れる・外す。
#
#   curl -fsSL https://github.com/min-nano/vectorworks-plugin-command-line-interface/releases/latest/download/get-vw2026.sh | sh
#   curl -fsSL …/releases/download/<tag>/get-vw2026.sh | sh                          # そのタグを入れる
#   curl -fsSL …/get-vw2026.sh | sh -s -- --plugins-dir <Plug-Ins>                   # 既定以外の場所へ
#   curl -fsSL …/releases/download/<tag>/get-vw2026.sh | sh -s -- uninstall          # そのタグの CLI で外す
#
# 本体は関数に入れ、最後の行で呼ぶ。パイプで受け取る途中で切れたときに、途中までを走らせないため。
set -eu

repo="https://github.com/min-nano/vectorworks-plugin-command-line-interface"

# name はこのスクリプトの名前（get-vw2026）。版が書き込まれる前は雛形の名前。
name=get-vw

fail() {
	echo "$name: $*" >&2
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
	# 版も同じく、リリースが書き込む（2026 か 2025）。コマンド・環境変数・資産の名前が版で決まる。
	tag='@VW_TAG@'
	year='@VW_YEAR@'
	case "$year" in
	2025 | 2026) ;;
	*) fail "this script is not the one a release published; use $repo/releases/latest/download/get-vw2026.sh" ;;
	esac
	cli="vw$year"
	name="get-$cli"
	latest="$repo/releases/latest/download/$name.sh"
	valid_tag "$tag" || fail "this script is not the one a release published; use $latest"

	# 位置の引数は uninstall（uninstall cli）だけ、指定は --plugins-dir（--plugins-dir=<dir> も）だけを
	# 受け付ける。--registry は持たない（install は既定のレジストリから先に引き、cli は既定のレジストリに
	# あるので、他のレジストリは使われない。uninstall はレジストリを読まない）。指定は、書いた位置に依らず
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
					"for another tag, use $repo/releases/download/<tag>/$name.sh)"
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
	[ "$(uname -s)" = Darwin ] || fail "this script is for macOS; on Windows, use $name.ps1"
	# リリースが配るのは macos-arm64 だけ（docs/plugin/identifiers.md）。他は取りに行く前に止める
	# （無い zip を取りに行って、分かりにくい「取れない」で終わらないように）。
	if [ "$(sysctl -n hw.optional.arm64 2>/dev/null || true)" = 1 ]; then
		target=macos-arm64
	else
		fail "cli is not built for this Mac (macos-x64, an Intel Mac); it runs on an Apple silicon Mac"
	fi
	# 既定のレジストリの cli の規則と同じ形の場所（マニフェストは読まない。JSON を読まずに済むように）。
	# 2026 の資産は cli-<ターゲット>.zip、2025 は cli-vw2025-<ターゲット>.zip（docs/plugin/editions.md）。
	if [ "$year" = 2026 ]; then
		asset="cli-$target.zip"
	else
		asset="cli-$cli-$target.zip"
	fi
	url="$repo/releases/download/$tag/$asset"

	tmp="$(mktemp -d "${TMPDIR:-/tmp}/$name.XXXXXX")"
	trap 'rm -rf "$tmp"' EXIT
	trap 'exit 130' INT
	trap 'exit 143' TERM
	echo "$name: downloading $url" >&2
	curl -fsSL --retry 3 -o "$tmp/cli.zip" "$url" || fail "cannot download $url"
	# CLI だけを取り出す（実行ビットは zip のものを保つ）。
	unzip -q "$tmp/cli.zip" "bin/$cli" -d "$tmp" || fail "no bin/$cli in $url"

	# 利用者の環境の install の指定（VW2026_INSTALL_CHECK・VW2026_REGISTRY。2025 は VW2025_）は渡さない（この
	# スクリプトは cli@<tag> を既定のレジストリから入れるだけで、--check もレジストリも受け付けない）。
	unset "VW${year}_INSTALL_CHECK" "VW${year}_REGISTRY"
	# 標準入力は渡さない（curl | sh では標準入力がこのスクリプトそのもの）。
	code=0
	"$tmp/bin/$cli" "$@" </dev/null || code=$?
	if [ "$code" -eq 6 ]; then
		echo "$name: Vectorworks is running. Quit Vectorworks and run this script again." >&2
	fi
	exit "$code"
}

main "$@"

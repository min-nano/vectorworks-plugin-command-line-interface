#!/bin/sh
# 初回スクリプト（mac）。取得したリリースの CLI を取り出して vw2026 install cli@<そのリリースのタグ>
# （又は uninstall cli）を実行するのみで、配置の手順・PATH・判断は install と cli のフックが担う
# （docs/plugin/install-and-update.md「初回スクリプト」）。インストール・アンインストールする
# タグは、スクリプトを取得したリリースのタグに定まっている（他のタグはそのタグのスクリプトを
# 取得する）。
#
# このファイルは雛形で、リリースがタグと版を書き込んで get-vw2026.sh と get-vw2025.sh を生成する
# （docs/plugin/editions.md）。2025 のものは vw2025 を取り出して Vectorworks 2025 の cli を
# インストール・アンインストールする。
#
#   curl -fsSL https://github.com/min-nano/vectorworks-plugin-command-line-interface/releases/latest/download/get-vw2026.sh | sh
#   curl -fsSL …/releases/download/<tag>/get-vw2026.sh | sh                  # そのタグを導入
#   curl -fsSL …/get-vw2026.sh | sh -s -- --plugins-dir <Plug-Ins>           # 既定以外の場所へ
#   curl -fsSL …/releases/download/<tag>/get-vw2026.sh | sh -s -- uninstall  # そのタグの CLI で除去
#
# 本体は関数に格納し、最後の行で呼び出す。パイプで受け取る途中で切断された場合に、途中までを
# 実行しないため。
set -eu

repo="https://github.com/min-nano/vectorworks-plugin-command-line-interface"

# name はこのスクリプトの名前（get-vw2026）。版が書き込まれる前は雛形の名前。
name=get-vw

fail() {
	echo "$name: $*" >&2
	exit 1
}

# タグに使用できる文字のみか（docs/registry.md「名前・タグ・ターゲット」）。リリースが置換していない
# スクリプト（リポジトリのものをそのまま実行した場合）を拒否し、URL に埋め込めることを確認する。
valid_tag() {
	case "$1" in
	"" | [!A-Za-z0-9]* | *[!A-Za-z0-9._-]*) return 1 ;;
	*) return 0 ;;
	esac
}

main() {
	# タグは、リリースが公開する際にそのリリースのタグに置換する（docs/plugin/build-and-release.md
	# 「リリース」）。いずれのリリースから取得したスクリプトも、そのリリースの cli をインストール・
	# アンインストールする。版も同様に、リリースが書き込む（2026 か 2025）。コマンド・環境変数・
	# 資産の名前が版により定まる。
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

	# 位置引数は uninstall（uninstall cli）のみ、指定は --plugins-dir（--plugins-dir=<dir> も）
	# のみを受け付ける。--registry は持たない（install は既定のレジストリを先に参照し、cli は既定の
	# レジストリに存在するため、他のレジストリは使用されない。uninstall はレジストリを
	# 読み取らない）。指定は、記述した位置に依らず CLI のコマンドの後ろへそのまま渡す（vw2026 は
	# コマンドの前の指定を受け付けない）。
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
				# uninstall cli とも記述できる（vw2026 uninstall cli と同じ形式）。
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

	# ターゲットは計算機により決定する（Rosetta の下の端末でも、Vectorworks は arm64 で動作する。
	# docs/registry.md「名前・タグ・ターゲット」）。
	[ "$(uname -s)" = Darwin ] || fail "this script is for macOS; on Windows, use $name.ps1"
	# リリースが配布するのは macos-arm64 のみ（docs/plugin/identifiers.md）。他は取得する前に
	# 停止する（存在しない zip を取得しに行き、分かりにくい「取得できない」で終了しないように）。
	if [ "$(sysctl -n hw.optional.arm64 2>/dev/null || true)" = 1 ]; then
		target=macos-arm64
	else
		fail "cli is not built for this Mac (macos-x64, an Intel Mac); it runs on an Apple silicon Mac"
	fi
	# 既定のレジストリの cli の規則と同じ形式の場所（マニフェストは読み取らない。JSON の読み取りを
	# 不要にするため）。2026 の資産は cli-<ターゲット>.zip、2025 は cli-vw2025-<ターゲット>.zip
	# （docs/plugin/editions.md）。
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
	# CLI のみを取り出す（実行ビットは zip のものを保持する）。
	unzip -q "$tmp/cli.zip" "bin/$cli" -d "$tmp" || fail "no bin/$cli in $url"

	# 利用者の環境の install の指定（VW2026_INSTALL_CHECK・VW2026_REGISTRY。2025 は VW2025_）は
	# 渡さない（このスクリプトは cli@<tag> を既定のレジストリからインストールするのみで、--check も
	# レジストリも受け付けない）。
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

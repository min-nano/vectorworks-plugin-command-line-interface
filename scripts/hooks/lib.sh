# shellcheck shell=sh
# cli の install・uninstall のフック（install.sh・uninstall.sh）が読み込む関数
# （docs/plugin/install-and-update.md「`cli` のフック」）。リンクの判定と PATH の項目の比べ方を
# 1 か所に置き、install と uninstall が同じ判定を使うようにする。
#
# POSIX の sh で書く（local を使わないので、関数の中の変数は名前に関数の名前を付けて分ける）。

# hook_fail は理由を標準エラーへ書いて 1 で終わる。何も変える前の確かめにだけ使う。
hook_fail() {
	printf 'cli hook: %s\n' "$1" >&2
	exit 1
}

# hook_year は、渡されたフックの変数の名前から版（2026 か 2025）を決めて書く。CLI は自分の版の名前（2025 の
# CLI は VW2025_HOOK_*）だけを渡すので、2025 と 2026 の zip に同じフックを入れられる（docs/plugin/editions.md）。
# どちらも無いとき・両方あるときは、理由を書いて 1 で終わる（呼ぶ側は $(...) の終了コードを見て、何も変える前に
# 終わる）。
hook_year() {
	if [ -n "${VW2025_HOOK_INSTALL_DIR+x}" ] && [ -n "${VW2026_HOOK_INSTALL_DIR+x}" ]; then
		hook_fail "both VW2025_HOOK_INSTALL_DIR and VW2026_HOOK_INSTALL_DIR are set"
	elif [ -n "${VW2025_HOOK_INSTALL_DIR+x}" ]; then
		printf 2025
	elif [ -n "${VW2026_HOOK_INSTALL_DIR+x}" ]; then
		printf 2026
	else
		hook_fail "VW2026_HOOK_INSTALL_DIR (or VW2025_HOOK_INSTALL_DIR) is not set"
	fi
}

# hook_install_dir は、版 $1 の VW<版>_HOOK_INSTALL_DIR を absolute_dir で確かめて書く（絶対パスでなければ
# 理由を書いて 1 で終わる）。
hook_install_dir() {
	case $1 in
	2025) absolute_dir VW2025_HOOK_INSTALL_DIR "${VW2025_HOOK_INSTALL_DIR-}" ;;
	*) absolute_dir VW2026_HOOK_INSTALL_DIR "${VW2026_HOOK_INSTALL_DIR-}" ;;
	esac
}

# hook_init は、フックの冒頭で決める 4 つの変数を置く。year（hook_year）・install_dir（hook_install_dir。
# <Plug-Ins>/cli）・command（vw<版>）・home（絶対パスにした HOME）。決められなければ、理由は呼んだ関数が
# 書いているので、そのまま 1 で終わる（何も変える前に終わる）。$(...) の中では呼ばない（変数を置けないため）。
# 変数は install.sh・uninstall.sh が使う（このファイルだけを検査すると使われていないように見える）。
# shellcheck disable=SC2034
hook_init() {
	year="$(hook_year)" || exit 1
	install_dir="$(hook_install_dir "$year")" || exit 1
	command="vw$year"
	home="$(absolute_dir HOME "${HOME-}")" || exit 1
}

# strip_slashes は末尾の / を除いて書く（根の / は残す）。
strip_slashes() {
	strip_slashes_s="$1"
	while :; do
		case $strip_slashes_s in
		?*/) strip_slashes_s="${strip_slashes_s%/}" ;;
		*) break ;;
		esac
	done
	printf '%s' "$strip_slashes_s"
}

# absolute_dir は、環境変数 $1 の値 $2 が絶対パスなら末尾の / を除いて書き、そうでなければ
# 理由を書いて 1 で終わる（呼ぶ側は $(...) の終了コードを見る）。
absolute_dir() {
	case $2 in
	/*) strip_slashes "$2" ;;
	'') hook_fail "$1 is not set" ;;
	*) hook_fail "$1 is not an absolute path: $2" ;;
	esac
}

# json_string は $1 を JSON の文字列にして書く。" と \ と制御文字を逃がし、他のバイト（UTF-8 の
# 日本語を含む）はそのまま書く。末尾の改行も保つように、x を足して渡し、awk の中で落とす。
json_string() {
	printf '%sx' "$1" | LC_ALL=C awk '
		BEGIN {
			for (i = 1; i < 32; i++) {
				ctrl = ctrl sprintf("%c", i)
			}
		}
		{
			text = (NR == 1) ? $0 : text "\n" $0
		}
		END {
			text = substr(text, 1, length(text) - 1)
			out = "\""
			for (i = 1; i <= length(text); i++) {
				c = substr(text, i, 1)
				code = index(ctrl, c)
				if (code > 0) {
					out = out sprintf("\\u%04x", code)
				} else if (c == "\"" || c == "\\") {
					out = out "\\" c
				} else {
					out = out c
				}
			}
			printf "%s\"", out
		}'
}

# dir_in_path は、ディレクトリ $1 が PATH の項目にあるか。同じ場所かは same_place で決める（リンクの確かめと
# 揃える。同じディレクトリを別の書き方で指す項目も、あるとみなす）。相対の項目（空を含む）はあるとみなさない。
dir_in_path() {
	dir_in_path_rest="${PATH-}:"
	while [ -n "$dir_in_path_rest" ]; do
		dir_in_path_entry="${dir_in_path_rest%%:*}"
		dir_in_path_rest="${dir_in_path_rest#*:}"
		if same_place "$dir_in_path_entry" "$1"; then
			return 0
		fi
	done
	return 1
}

# nearest_dir は、絶対パス $1（末尾の / を除いたもの）から、在るいちばん近いディレクトリまでさかのぼり、
# そのディレクトリを nearest_dir_dir、その下の在らない部分（/ で始まる。在れば空）を nearest_dir_rest に置く。
nearest_dir() {
	nearest_dir_dir="$1"
	nearest_dir_rest=""
	while [ ! -d "$nearest_dir_dir" ]; do
		nearest_dir_rest="/${nearest_dir_dir##*/}$nearest_dir_rest"
		nearest_dir_dir="${nearest_dir_dir%/*}"
		[ -n "$nearest_dir_dir" ] || nearest_dir_dir=/
	done
}

# same_place は、絶対パス $1 と $2 が同じ場所を指すか。それぞれの、在るいちばん近いディレクトリ（nearest_dir）
# どうしを同一性（-ef。i ノードで比べる）で比べ、その下の在らない部分は名前の文字列で比べる。同じディレクトリを
# 別の書き方（シンボリックリンク・大文字と小文字・--plugins-dir の書き方）で指しても同じとみなすため
# （CLI の within と同じ考え方）。uninstall のフックはフォルダを移した後で走り、<Plug-Ins>/cli/ はもう
# 無いので、在る親までさかのぼる。相対パスは同じとみなさない。
same_place() {
	case $1 in /*) ;; *) return 1 ;; esac
	case $2 in /*) ;; *) return 1 ;; esac
	nearest_dir "$(strip_slashes "$1")"
	same_place_a="$nearest_dir_dir"
	same_place_rest_a="$nearest_dir_rest"
	nearest_dir "$(strip_slashes "$2")"
	# -ef は POSIX に無いが、mac の /bin/sh（と bash・dash・zsh）は持つ。
	# shellcheck disable=SC3013
	[ "$same_place_rest_a" = "$nearest_dir_rest" ] && [ "$same_place_a" -ef "$nearest_dir_dir" ]
}

# link_points_to は、$1 がシンボリックリンクで、その中身（解かない）が $2 と同じ場所を指すか（same_place）。
# 相対の中身は、リンクのあるディレクトリから見た場所にする（OS がリンクを解くときと同じ）。
# install の「同じリンクが既にある」と uninstall の「このプラグインの CLI を指すリンク」は、同じこの関数で
# 決める（片方だけが一致とみなすと、install が exists と答えたリンクを uninstall が残すため）。
link_points_to() {
	[ -L "$1" ] || return 1
	link_points_to_target="$(readlink "$1")"
	case $link_points_to_target in
	/*) ;;
	*) link_points_to_target="$(dirname "$1")/$link_points_to_target" ;;
	esac
	same_place "$link_points_to_target" "$2"
}

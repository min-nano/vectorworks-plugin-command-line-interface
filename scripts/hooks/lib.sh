# shellcheck shell=sh
# cli の install・uninstall のフック（install.sh・uninstall.sh）が読み込む関数
# （docs/plugin/install-and-update.md「`cli` のフック」）。リンクの判定と PATH の項目の比較方法を
# 1 か所に配置し、install と uninstall が同一の判定を使用するようにする。
#
# POSIX の sh で記述する（local を使用しないため、関数内の変数は名前に関数の名前を付けて区別する）。

# hook_fail は理由を標準エラーへ書き込み、1 で終了する。何も変更する前の確認にのみ使用する。
hook_fail() {
	printf 'cli hook: %s\n' "$1" >&2
	exit 1
}

# hook_year は、渡されたフックの変数の名前から版（2026 か 2025）を決定して書き出す。CLI は自身の版の
# 名前（2025 の CLI は VW2025_HOOK_*）のみを渡すため、2025 と 2026 の zip に同一のフックを格納できる
# （docs/plugin/editions.md）。いずれも無い場合・両方ある場合は、理由を書き込んで 1 で終了する
# （呼び出し元は $(...) の終了コードを確認し、何も変更する前に終了する）。
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

# hook_install_dir は、版 $1 の VW<版>_HOOK_INSTALL_DIR を absolute_dir で検証して書き出す
# （絶対パスでなければ理由を書き込んで 1 で終了する）。
hook_install_dir() {
	case $1 in
	2025) absolute_dir VW2025_HOOK_INSTALL_DIR "${VW2025_HOOK_INSTALL_DIR-}" ;;
	*) absolute_dir VW2026_HOOK_INSTALL_DIR "${VW2026_HOOK_INSTALL_DIR-}" ;;
	esac
}

# hook_init は、フックの冒頭で決定する 4 つの変数を設定する。year（hook_year）・install_dir
# （hook_install_dir。<Plug-Ins>/cli）・command（vw<版>）・home（絶対パスにした HOME）。
# 決定できなければ、理由は呼び出した関数が書き込んでいるため、そのまま 1 で終了する（何も変更する
# 前に終了する）。$(...) の中では呼び出さない（変数を設定できないため）。
# 変数は install.sh・uninstall.sh が使用する（このファイルのみを検査すると使用されていないように
# 見える）。
# shellcheck disable=SC2034
hook_init() {
	year="$(hook_year)" || exit 1
	install_dir="$(hook_install_dir "$year")" || exit 1
	command="vw$year"
	home="$(absolute_dir HOME "${HOME-}")" || exit 1
}

# strip_slashes は末尾の / を除去して書き出す（根の / は保持する）。
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

# absolute_dir は、環境変数 $1 の値 $2 が絶対パスならば末尾の / を除去して書き出し、そうでなければ
# 理由を書き込んで 1 で終了する（呼び出し元は $(...) の終了コードを確認する）。
absolute_dir() {
	case $2 in
	/*) strip_slashes "$2" ;;
	'') hook_fail "$1 is not set" ;;
	*) hook_fail "$1 is not an absolute path: $2" ;;
	esac
}

# json_string は $1 を JSON の文字列にして書き出す。" と \ と制御文字をエスケープし、他のバイト
# （UTF-8 の日本語を含む）はそのまま書き出す。末尾の改行も保持するように、x を追加して渡し、awk の
# 中で除去する。
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

# dir_in_path は、ディレクトリ $1 が PATH の項目に存在するか。同一の場所か否かは same_place で
# 判定する（リンクの確認と統一する。同じディレクトリを別の表記で指す項目も、存在するとみなす）。
# 相対の項目（空を含む）は存在するとみなさない。
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

# nearest_dir は、絶対パス $1（末尾の / を除いたもの）から、存在する最も近いディレクトリまで遡り、
# そのディレクトリを nearest_dir_dir、その下の存在しない部分（/ で始まる。存在すれば空）を
# nearest_dir_rest に設定する。
nearest_dir() {
	nearest_dir_dir="$1"
	nearest_dir_rest=""
	while [ ! -d "$nearest_dir_dir" ]; do
		nearest_dir_rest="/${nearest_dir_dir##*/}$nearest_dir_rest"
		nearest_dir_dir="${nearest_dir_dir%/*}"
		[ -n "$nearest_dir_dir" ] || nearest_dir_dir=/
	done
}

# same_place は、絶対パス $1 と $2 が同じ場所を指すか。それぞれの、存在する最も近いディレクトリ
# （nearest_dir）同士を同一性（-ef。i ノードで比較する）で比較し、その下の存在しない部分は名前の
# 文字列で比較する。同じディレクトリを別の表記（シンボリックリンク・大文字と小文字・--plugins-dir の
# 表記）で指しても同じとみなすため（CLI の within と同じ考え方）。uninstall のフックはフォルダを
# 移動した後で実行され、<Plug-Ins>/cli/ は既に存在しないため、存在する親まで遡る。相対パスは
# 同じとみなさない。
same_place() {
	case $1 in /*) ;; *) return 1 ;; esac
	case $2 in /*) ;; *) return 1 ;; esac
	nearest_dir "$(strip_slashes "$1")"
	same_place_a="$nearest_dir_dir"
	same_place_rest_a="$nearest_dir_rest"
	nearest_dir "$(strip_slashes "$2")"
	# -ef は POSIX に無いが、mac の /bin/sh（と bash・dash・zsh）は備えている。
	# shellcheck disable=SC3013
	[ "$same_place_rest_a" = "$nearest_dir_rest" ] && [ "$same_place_a" -ef "$nearest_dir_dir" ]
}

# link_points_to は、$1 がシンボリックリンクで、その内容（解決しない）が $2 と同じ場所を指すか
# （same_place）。相対の内容は、リンクのあるディレクトリから見た場所とする（OS がリンクを解決する
# 場合と同様）。install の「同じリンクが既にある」と uninstall の
# 「このプラグインの CLI を指すリンク」は、同一のこの関数で判定する（片方のみが一致とみなすと、
# install が exists と応答したリンクを uninstall が残すため）。
link_points_to() {
	[ -L "$1" ] || return 1
	link_points_to_target="$(readlink "$1")"
	case $link_points_to_target in
	/*) ;;
	*) link_points_to_target="$(dirname "$1")/$link_points_to_target" ;;
	esac
	same_place "$link_points_to_target" "$2"
}

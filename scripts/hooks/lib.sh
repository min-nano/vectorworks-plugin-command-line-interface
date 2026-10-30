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

# json_string は $1 を JSON の文字列にして書く。" と \ と制御文字を逃がし、ほかのバイト（UTF-8 の
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

# dir_in_path は、ディレクトリ $1 が PATH の項目にあるか（末尾の / の違いは同じとみなす）。
dir_in_path() {
	dir_in_path_want="$(strip_slashes "$1")"
	dir_in_path_rest="${PATH-}:"
	while [ -n "$dir_in_path_rest" ]; do
		dir_in_path_entry="${dir_in_path_rest%%:*}"
		dir_in_path_rest="${dir_in_path_rest#*:}"
		if [ "$(strip_slashes "$dir_in_path_entry")" = "$dir_in_path_want" ]; then
			return 0
		fi
	done
	return 1
}

# link_points_to は、$1 がシンボリックリンクで、その中身（解かない）がちょうど $2 か。
link_points_to() {
	[ -L "$1" ] && [ "$(readlink "$1")" = "$2" ]
}

# link_points_into は、$1 がシンボリックリンクで、その中身（解かない）がディレクトリ $2 の中を
# 指すか。uninstall は、別の場所を指すリンクと利用者のファイルに触れないために、これで決める。
link_points_into() {
	[ -L "$1" ] || return 1
	case $(readlink "$1") in
	"$2"/*) return 0 ;;
	*) return 1 ;;
	esac
}

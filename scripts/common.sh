# shellcheck shell=sh
# scripts/format.sh と scripts/lint.sh が共に読む（. で読み込む）。対象と版の確かめを 1 か所に置き、
# 整形と検査で食い違わないようにする。読む前にリポジトリのルートへ cd しておく。

# shfmt_scripts は、引数に scripts/ と tests/hooks/ を足して shfmt を走らせる（shfmt が整形し、shellcheck が
# 検査するシェルスクリプトの置き場所）。shfmt は go.mod の無いリポジトリのルートでは動かないので、tools/ で
# 実行し、絶対パスを渡す。
shfmt_scripts() {
	go -C tools tool shfmt "$@" "$PWD/scripts" "$PWD/tests/hooks"
}

# require_clang は、PATH の clang-format・clang-tidy（$1）が tools/requirements.txt の版でなければ止める
# （版で出力と指摘が変わるため）。版は前方一致ではなく、全体で比べる（23.1.3 の固定で 23.1.30 を通さない）。
# tools/requirements.txt が 2 つを == で固定していないとき（どの版も通ってしまう）と、2 つの版が違うときも
# 止める（同じ版にそろえる。docs/lint.md）。2 つは PyPI の別のパッケージで、版の出る時期が違うので、
# Dependabot は片方だけを上げた PR を作ることがある。ここで止めて、もう片方の版が出るまでマージさせない。
# sh には local が無いので、呼ぶ側の変数と重ならないよう、名前を common_ で始める。
require_clang() {
	clang_pin clang-format
	common_format="$common_pin"
	clang_pin clang-tidy
	common_tidy="$common_pin"
	if [ "$common_format" != "$common_tidy" ]; then
		echo "tools/requirements.txt pins clang-format $common_format and" \
			"clang-tidy $common_tidy; pin both to the same version" >&2
		exit 1
	fi
	common_found="$("$1" --version 2>/dev/null |
		sed -n 's/.*version \([0-9][0-9.]*\).*/\1/p' | head -n 1)"
	if [ "$common_found" != "$common_format" ]; then
		echo "$1 $common_format is required (found ${common_found:-none}):" \
			"pip install -r tools/requirements.txt" >&2
		exit 1
	fi
}

# clang_pin は、tools/requirements.txt の $1（clang-format か clang-tidy）を固定した版を common_pin に入れる。行末の
# CR は除く（Windows で autocrlf の checkout）。== の行が無いときと、== の行から版（数字とドット）を読めないとき
# （接尾辞の付いた版・行末のコメント）は止め、読めなかったときはその行を示す（固定していないと誤って伝えない）。
clang_pin() {
	common_line="$(tr -d '\r' <tools/requirements.txt | grep "^$1==" || true)"
	common_pin="$(printf '%s\n' "$common_line" | sed -n "s/^$1==\([0-9][0-9.]*\)\$/\1/p")"
	if [ -n "$common_pin" ]; then
		return
	fi
	if [ -z "$common_line" ]; then
		echo "tools/requirements.txt does not pin $1 with ==" >&2
	else
		echo "tools/requirements.txt pins $1 as \"$common_line\";" \
			"write it as $1==<version> (digits and dots) alone on the line" >&2
	fi
	exit 1
}

# powershell_files は、git が追う・無視しない PowerShell のファイルを並べる（ビルドの出力や SDK の写しを
# 含めない）。
powershell_files() {
	git ls-files -co --exclude-standard -- '*.ps1' '*.psm1' '*.psd1'
}

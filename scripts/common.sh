# shellcheck shell=sh
# scripts/format.sh と scripts/lint.sh が共に読み込む（. で読み込む）。対象と版の確認を 1 か所に
# 配置し、整形と検査で食い違わないようにする。読み込む前にリポジトリのルートへ cd しておく。

# shfmt_scripts は、引数に scripts/ と tests/hooks/ を追加して shfmt を実行する（shfmt が整形し、
# shellcheck が検査するシェルスクリプトの配置場所）。shfmt は go.mod の無いリポジトリのルートでは
# 動作しないため、tools/ で実行し、絶対パスを渡す。
shfmt_scripts() {
	go -C tools tool shfmt "$@" "$PWD/scripts" "$PWD/tests/hooks"
}

# require_clang は、PATH の clang-format・clang-tidy（$1）が tools/requirements.txt の版でなければ
# 停止する（版により出力と指摘が変わるため）。版は前方一致ではなく、全体で比較する（23.1.3 の固定で
# 23.1.30 を通過させない）。tools/requirements.txt が 2 つを == で固定していない場合（いずれの版も
# 通過してしまう）と、2 つの版が異なる場合も停止する（同一の版に統一する。docs/lint.md）。2 つは
# PyPI の別のパッケージで、版の公開の時期が異なるため、Dependabot は片方のみを上げた PR を
# 作成することがある。ここで停止し、もう片方の版が公開されるまでマージさせない。
# sh には local が無いため、呼び出し元の変数と重複しないよう、名前を common_ で始める。
require_clang() {
	requirements_pin clang-format
	common_format="$common_pin"
	requirements_pin clang-tidy
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

# require_shellcheck は、PATH の shellcheck が tools/requirements.txt の shellcheck-py の
# 版でなければ停止する（版により指摘が変わるため。actionlint も PATH の shellcheck で run: の内容を
# 検査する）。shellcheck-py の版は <shellcheck の版>.<再パッケージの番号> であるため、最後の番号を
# 除いたものを、shellcheck --version の版と全体で比較する。Windows では shellcheck の出力の行末が
# CRLF となり、版の行を $ で固定して読み取るため、CR を除去してから読み取る。
require_shellcheck() {
	requirements_pin shellcheck-py
	common_want="$(printf '%s\n' "$common_pin" | sed -n 's/^\([0-9]*\.[0-9]*\.[0-9]*\)\.[0-9]*$/\1/p')"
	if [ -z "$common_want" ]; then
		echo "tools/requirements.txt pins shellcheck-py $common_pin;" \
			"write it as <shellcheck version>.<build> (four numbers)" >&2
		exit 1
	fi
	common_found="$(shellcheck --version 2>/dev/null | tr -d '\r' |
		sed -n 's/^version: \([0-9][0-9.]*\)$/\1/p' | head -n 1)"
	if [ "$common_found" != "$common_want" ]; then
		echo "shellcheck $common_want is required (found ${common_found:-none}):" \
			"pip install -r tools/requirements.txt" >&2
		exit 1
	fi
}

# requirements_pin は、tools/requirements.txt の $1（パッケージの名前）を固定した版を common_pin に
# 格納する。行末の CR は除去する（Windows で autocrlf の checkout）。== の行が無い場合と、== の
# 行から版（数字とドット）を読み取れない場合（接尾辞の付いた版・行末のコメント）は停止し、
# 読み取れなかった場合はその行を示す（固定していないと誤って伝えない）。
requirements_pin() {
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

# tracked_files は、git が追跡し、無視しないファイルの内、パターン（$@）に合致するものを列挙する
# （ビルドの出力や SDK の複製を含めない）。
tracked_files() {
	git ls-files -co --exclude-standard -- "$@"
}

# powershell_files は、整形と検査の対象の PowerShell のファイルを列挙する。
powershell_files() {
	tracked_files '*.ps1' '*.psm1' '*.psd1'
}

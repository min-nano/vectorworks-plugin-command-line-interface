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
# （版で出力と指摘が変わるため）。tools/requirements.txt の clang-format と clang-tidy の版が違っても止める
# （同じ版にそろえる。docs/lint.md）。2 つは PyPI の別のパッケージで、版の出る時期が違うので、Dependabot は
# 片方だけを上げた PR を作ることがある。ここで止めて、もう片方の版が出るまでマージさせない。
require_clang() {
	format="$(sed -n 's/^clang-format==//p' tools/requirements.txt)"
	tidy="$(sed -n 's/^clang-tidy==//p' tools/requirements.txt)"
	if [ "$format" != "$tidy" ]; then
		echo "tools/requirements.txt pins clang-format $format and clang-tidy $tidy; pin both to the same version" >&2
		exit 1
	fi
	want="$(sed -n "s/^$1==//p" tools/requirements.txt)"
	if ! "$1" --version 2>/dev/null | grep -qF "version $want"; then
		echo "$1 $want is required: pip install -r tools/requirements.txt" >&2
		exit 1
	fi
}

# powershell_files は、git が追う・無視しない PowerShell のファイルを並べる（ビルドの出力や SDK の写しを
# 含めない）。
powershell_files() {
	git ls-files -co --exclude-standard -- '*.ps1' '*.psm1' '*.psd1'
}

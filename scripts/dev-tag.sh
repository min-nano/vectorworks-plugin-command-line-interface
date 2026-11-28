#!/bin/sh
# PR のプレリリースのタグ（dev-<slug>-<PR 番号>-<短い sha>）を生成する
# （docs/plugin/identifiers.md）。タグは再利用しない（docs/registry.md「名前・タグ・ターゲット」）
# ため、push ごとに短い sha により変わる。slug は可読性のための装飾であり、/:@ と空白を - に置換し、
# 英数字・-・_・. 以外を除去し、連続する . を 1 つにまとめ（.. は git の ref 名に使用できない。
# 日本語を除去すると a.レ.b が a..b になる）、先頭から 40 文字で切り詰めて末尾の -・_・. を
# 除去したもの（タグを 128 文字の上限に収めるため）。別のブランチが同一の slug になりうる
# （claude/foo と claude-foo、日本語のみの名前）ため、一意にするのは PR 番号と sha。slug が空ならば
# dev-<PR 番号>-<短い sha>。タグを生成するのはこのスクリプトのみで、build.yml が使用する
# （cleanup-dev-release.yml と build.yml の release の下書きの整理は、この形式の末尾の
# -<PR 番号>-<短い sha> で PR のタグを識別する）。
#
#   scripts/dev-tag.sh <branch> <PR 番号> <短い sha>
set -eu

usage="usage: dev-tag.sh <branch> <pr-number> <short-sha>"
branch="${1?$usage}"
pr="${2:?$usage}"
sha="${3:?$usage}"
case "$pr" in
*[!0-9]*)
	echo "dev-tag.sh: the PR number $pr is not a number" >&2
	exit 1
	;;
esac
# 短い sha は小文字の 16 進 7 桁（cleanup-dev-release.yml と build.yml が同じ形式で識別する）。
case "$sha" in
[0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f]) ;;
*)
	echo "dev-tag.sh: the short sha $sha is not 7 lowercase hex digits" >&2
	exit 1
	;;
esac
slug="$(printf '%s' "$branch" | tr '/:@ ' '----' | tr -cd 'A-Za-z0-9._-' | tr -s . | cut -c1-40 | sed 's/[-_.]*$//')"
if [ -z "$slug" ]; then
	printf 'dev-%s-%s\n' "$pr" "$sha"
else
	printf 'dev-%s-%s-%s\n' "$slug" "$pr" "$sha"
fi

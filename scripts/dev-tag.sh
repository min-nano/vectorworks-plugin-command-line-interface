#!/bin/sh
# PR のプレリリースのタグ（dev-<slug>-<PR 番号>-<短い sha>）を作る（docs/plugin/identifiers.md）。
# タグは使い回さない（docs/registry.md「名前・タグ・ターゲット」）ので、push ごとに短い sha で変わる。
# slug は読むための飾りで、/:@ と空白を - にし、英数字・-・_・. のほかを落とし、続く . を 1 つに
# まとめ（.. は git の ref 名に使えない。日本語を落とすと a.レ.b が a..b になる）、先頭から 40 文字で
# 切って末尾の -・_・. を落としたもの（タグを 128 文字の上限に収めるため）。別のブランチが同じ slug に
# なりうる（claude/foo と claude-foo、日本語だけの名前）ので、一意にするのは PR 番号と sha。
# slug が空なら dev-<PR 番号>-<短い sha>。タグを作るのはこのスクリプトだけで、build.yml が使う
# （cleanup-dev-release.yml はこの形の末尾の -<PR 番号>-<短い sha> で PR のタグを見分ける）。
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
# 短い sha は小文字の 16 進 7 桁（cleanup-dev-release.yml が同じ形で見分ける）。
case "$sha" in
[0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f]) ;;
*)
	echo "dev-tag.sh: the short sha $sha is not 7 lowercase hex digits" >&2
	exit 1
	;;
esac
slug="$(printf '%s' "$branch" | tr '/:@ ' '----' | tr -cd 'A-Za-z0-9._-' | sed 's/\.\.*/./g' | cut -c1-40 | sed 's/[-_.]*$//')"
if [ -z "$slug" ]; then
	printf 'dev-%s-%s\n' "$pr" "$sha"
else
	printf 'dev-%s-%s-%s\n' "$slug" "$pr" "$sha"
fi

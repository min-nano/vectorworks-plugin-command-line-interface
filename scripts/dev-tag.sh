#!/bin/sh
# PR のプレリリースのタグ（dev-<slug>-<PR 番号>）を作る（docs/plugin/identifiers.md）。
# slug は読むための飾りで、/:@ と空白を - にし、英数字・-・_・. のほかを落としたもの。別のブランチが
# 同じ slug になりうる（claude/foo と claude-foo、日本語だけの名前）ので、一意にするのは PR 番号。
# slug が空なら dev-<PR 番号>。build.yml と cleanup-dev-release.yml が同じものを使う。
#
#   scripts/dev-tag.sh <branch> <PR 番号>
set -eu

branch="${1?usage: dev-tag.sh <branch> <pr-number>}"
pr="${2:?usage: dev-tag.sh <branch> <pr-number>}"
case "$pr" in
*[!0-9]*)
	echo "dev-tag.sh: the PR number $pr is not a number" >&2
	exit 1
	;;
esac
slug="$(printf '%s' "$branch" | tr '/:@ ' '----' | tr -cd 'A-Za-z0-9._-')"
if [ -z "$slug" ]; then
	printf 'dev-%s\n' "$pr"
else
	printf 'dev-%s-%s\n' "$slug" "$pr"
fi

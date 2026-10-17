#!/bin/sh
# PR のプレリリースのタグ（dev-<slug>）をブランチの名前から作る（docs/plugin/identifiers.md）。
# slug は /:@ と空白を - にし、英数字・-・_・. のほかを落としたもの。build.yml と
# cleanup-dev-release.yml が同じものを使う。
#
#   scripts/dev-tag.sh <branch>
set -eu

branch="${1:?usage: dev-tag.sh <branch>}"
slug="$(printf '%s' "$branch" | tr '/:@ ' '----' | tr -cd 'A-Za-z0-9._-')"
if [ -z "$slug" ]; then
	echo "dev-tag.sh: the branch name $branch has no letters for a tag" >&2
	exit 1
fi
printf 'dev-%s\n' "$slug"

#!/usr/bin/env bash
# 自動レビュー（docs/review.md）に使用する code-review プラグインを、固定した commit から取り出す。
#
# 取得元と commit は git の submodule（.gitmodules の SUBMODULE）で固定し、Dependabot
# （gitsubmodule）に更新させる。取得元には tag も release も無く、Actions でも npm のパッケージでも
# ないため、Dependabot が追跡できるのは submodule のみである。submodule 自体はチェックアウトしない
# （actions/checkout の既定）。記録された commit から PLUGIN_PATH のみを DEST に取り出す。
#
# 取り出した後、プラグインのファイルが存在するかを確認する（置き場所が変更された commit に
# Dependabot が更新した場合に、ここで停止する）。Dependabot の PR では review ジョブが実行されない
# ため、review.yml の plugin ジョブが毎回これを実行して確認する。
#
# 使用方法（リポジトリのルートで実行する。SHA は HEAD のツリーから読み取るため、浅いチェックアウトで
# よい）:
#
#   scripts/review/plugin.sh "$RUNNER_TEMP/plugins"
#
# 標準出力には、取り出したプラグインのディレクトリのみを出力する。
set -euo pipefail

SUBMODULE=.github/claude-plugins-official
PLUGIN_PATH=plugins/code-review
REQUIRED=(.claude-plugin/plugin.json commands/code-review.md)

dest="${1:?usage: scripts/review/plugin.sh <destination>}"

url="$(git config -f .gitmodules "submodule.${SUBMODULE}.url")"
sha="$(git rev-parse "HEAD:${SUBMODULE}")"

git init -q "$dest"
git -C "$dest" sparse-checkout set "$PLUGIN_PATH"
git -C "$dest" fetch -q --depth 1 --filter=blob:none "$url" "$sha"
git -C "$dest" checkout -q FETCH_HEAD
echo "plugin: ${url%.git}/tree/${sha}/${PLUGIN_PATH}" >&2

plugin="$dest/$PLUGIN_PATH"
for file in "${REQUIRED[@]}"; do
	if [ ! -f "$plugin/$file" ]; then
		echo "::error::${PLUGIN_PATH}/${file} of the code-review plugin is missing at ${sha} of ${url}." \
			"The plugin may have moved; fix the source or commit of ${SUBMODULE}, or PLUGIN_PATH of this script."
		exit 1
	fi
done

echo "$plugin"

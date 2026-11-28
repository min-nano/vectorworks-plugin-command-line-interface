#!/bin/sh
# SDK のキャッシュのキーを、配布されている SDK の版から決定する（docs/plugin/build-and-release.md
# 「CI」）。SDK の URL は latest で、SDK が差し替えられても同一であるため、HEAD の ETag（無ければ
# Last-Modified）をキーに含める。構成は SDK リファレンスの scripts/vw-sdk-cache-key.sh と同一。
#
#   VW_SDK_URL=… scripts/vw-sdk-cache-key.sh <キーの前半>
#
# GITHUB_OUTPUT があれば key と restore-keys を書き込む。版を確認できなければキーを
# <前半>-unknown-<実行の ID> とし、restore-keys に <前半>- を渡して直近のキャッシュを使用する（版が
# 判明している場合に渡すと、新しい版のキャッシュが無い初回に古い版を復元し、取得し直さなくなる）。
# キーを実行ごとに変更するのは、キャッシュが上書きできず、固定の <前半>-unknown では一度保存した
# SDK が以後も完全一致で復元され、版が判明しない間は古い SDK のままになるため。
set -eu

prefix="${1:?usage: vw-sdk-cache-key.sh <key-prefix>}"
url="${VW_SDK_URL:?VW_SDK_URL is not set}"

# 応答の見出しから 1 つ取り出す。リダイレクトの分も重複するため、最後のものを使用する。
header() {
	printf '%s\n' "$headers" | tr -d '\r' | awk -v k="$1" '
		tolower($0) ~ "^" k ":" { sub(/^[^:]*:[ \t]*/, ""); v = $0 } END { print v }'
}

version=""
if headers="$(curl -fsSIL --max-time 30 --retry 2 --retry-delay 3 "$url")"; then
	version="$(header etag)"
	[ -n "$version" ] || version="$(header last-modified)"
fi
# キーに使用できない文字を除去し、長さを抑える。
version="$(printf '%s' "$version" | tr -cs 'A-Za-z0-9._-' '-' | sed 's/^-//; s/-$//' | cut -c1-64)"
restore=""
if [ -z "$version" ]; then
	echo "::warning::could not read the SDK version from $url; using the latest cache"
	version="unknown-${GITHUB_RUN_ID:-local}"
	restore="${prefix}-"
fi
key="${prefix}-${version}"
echo "cache key: $key"
if [ -n "${GITHUB_OUTPUT:-}" ]; then
	{
		echo "key=$key"
		echo "restore-keys=$restore"
	} >>"$GITHUB_OUTPUT"
fi

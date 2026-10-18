#!/bin/sh
# SDK のキャッシュのキーを、配布されている SDK の版から決める（docs/plugin/build-and-release.md「CI」）。
# SDK の URL は latest で、SDK が差し替わっても同じなので、HEAD の ETag（無ければ Last-Modified）を
# キーに混ぜる。形は SDK リファレンスの scripts/vw-sdk-cache-key.sh と同じ。
#
#   VW_SDK_URL=… scripts/vw-sdk-cache-key.sh <キーの前半>
#
# GITHUB_OUTPUT があれば key と restore-keys を書く。版を確かめられなければキーを
# <前半>-unknown-<実行の ID> にし、restore-keys に <前半>- を渡して直近のキャッシュを使う（版が分かる
# ときに渡すと、新しい版のキャッシュが無い初回に古い版を復元し、取り直さなくなる）。キーを実行ごとに
# 変えるのは、キャッシュが上書きできず、固定の <前半>-unknown では一度保存した SDK が以後も完全一致で
# 戻り、版が分からない間は古い SDK のままになるため。
set -eu

prefix="${1:?usage: vw-sdk-cache-key.sh <key-prefix>}"
url="${VW_SDK_URL:?VW_SDK_URL is not set}"

# 応答の見出しから 1 つ取り出す。リダイレクトの分も重なるので、最後のものを使う。
header() {
	printf '%s\n' "$headers" | tr -d '\r' | awk -v k="$1" '
		tolower($0) ~ "^" k ":" { sub(/^[^:]*:[ \t]*/, ""); v = $0 } END { print v }'
}

version=""
if headers="$(curl -fsSIL --max-time 30 --retry 2 --retry-delay 3 "$url")"; then
	version="$(header etag)"
	[ -n "$version" ] || version="$(header last-modified)"
fi
# キーに使えない文字を落とし、長さを抑える。
version="$(printf '%s' "$version" | tr -c 'A-Za-z0-9._-' '-' | sed 's/--*/-/g; s/^-//; s/-$//' | cut -c1-64)"
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

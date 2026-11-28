#!/usr/bin/env bash
# PR に判定のレビュー（Approve / Request changes）を出す。GitHub の API が 5xx を返した場合は、
# 間隔を空けて出し直す。
#
# review.yml の review ジョブの判定の段が使用する。5xx は GitHub 側の一時的な失敗であり、出し直せば
# 成功することが多い。4xx（権限が無い・内容が不正）は出し直しても変化しないため、直ちに失敗とする。
#
# 5xx であっても、GitHub 側ではレビューが作成されていることがある。そのまま出し直すと同じ判定が
# 2 つ付与されるため、出し直す前に、同じ判定（同じ commit・種類・本文で、SINCE 以降に REVIEWER
# 〈既定は github-actions[bot]〉が出したもの）が既に存在するかを確認し、存在すれば成功とみなす。
#
# 使用方法（環境変数 GH_TOKEN・PR・HEAD_SHA を渡す。SINCE・RETRIES・RETRY_DELAY は任意）:
#
#   scripts/review/submit.sh APPROVE "<本文>"
#
# 終了コード: 0 出せた、1 出せなかった（4xx など）、2 5xx が継続して出せなかった
set -euo pipefail

event="${1:?usage: scripts/review/submit.sh APPROVE|REQUEST_CHANGES <body>}"
body="${2:?usage: scripts/review/submit.sh APPROVE|REQUEST_CHANGES <body>}"
: "${PR:?PR (the pull request number) is required}"
: "${HEAD_SHA:?HEAD_SHA (the commit to attach the verdict to) is required}"
repo="${GITHUB_REPOSITORY:?}"
reviewer="${REVIEWER:-github-actions[bot]}"
since="${SINCE:-1970-01-01T00:00:00Z}"
retries="${RETRIES:-3}"
delay="${RETRY_DELAY:-10}"

case "$event" in
APPROVE) state=APPROVED ;;
REQUEST_CHANGES) state=CHANGES_REQUESTED ;;
*)
	echo "the verdict must be APPROVE or REQUEST_CHANGES: $event" >&2
	exit 1
	;;
esac

# 同じ判定が既に付与されているか（5xx であっても作成されていた場合）。
already_posted() {
	gh api --paginate "repos/$repo/pulls/$PR/reviews?per_page=100" |
		jq -s -e --arg user "$reviewer" --arg sha "$HEAD_SHA" --arg state "$state" \
			--arg body "$body" --arg since "$since" \
			'[add[] | select(.user.login == $user and .commit_id == $sha and
				.state == $state and .body == $body and .submitted_at >= $since)]
			| length > 0' >/dev/null
}

attempt=0
while true; do
	if err="$(gh api "repos/$repo/pulls/$PR/reviews" --silent \
		-f commit_id="$HEAD_SHA" -f event="$event" -f body="$body" 2>&1)"; then
		exit 0
	fi
	echo "$err" >&2
	# gh は、本文が JSON ならば「gh: <message> (HTTP 502)」、そうでなければ（ロードバランサの HTML
	# など）「gh: HTTP 502」と出力する。いずれも検出する。
	if ! grep -qE 'HTTP 5[0-9]{2}' <<<"$err"; then
		exit 1
	fi
	# 5xx であっても作成されていることがあるため、出し直す前（最後の 1 回の後も）に確認する。一覧への
	# 反映を待機するため、間隔を空けてから確認する。
	sleep "$delay"
	if already_posted; then
		echo "the API returned 5xx, but the verdict was posted" >&2
		exit 0
	fi
	if [ "$attempt" -ge "$retries" ]; then
		echo "the API kept returning 5xx after ${retries} retries" >&2
		exit 2
	fi
	attempt=$((attempt + 1))
	echo "the API returned 5xx; retrying (${attempt}/${retries})" >&2
done

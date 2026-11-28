#!/usr/bin/env bash
# PR の自動レビュー（docs/review.md）の状況を取得する。
#
# review.yml の review ジョブが、プラグインを呼び出す前と後に使用する。
#
#   前: 判定（Approve / Request changes）があれば、PR 全体ではなく、その判定の head（verdict_sha）
#       からの差分を再レビューする。head が変化していなければ何もしない（再オープンなど）
#   後: この実行でレビューが付与されたかを数え、判定の材料とする。付与されていなければ、判定の
#       段が Request changes（標識なし）を出してジョブを失敗させる（レビュー準備完了の PR を判定の
#       無いまま残さない）
#
# 「判定済み」の標識は、ワークフローが出す判定のレビュー（VERDICT_AUTHOR。既定は
# github-actions[bot]）の本文の MARKER である。プラグインのコメントの有無を標識とすると、途中で
# 停止した実行（コメントを一部のみ付与した時点で中断したもの）を判定済みと誤認する。MARKER を含む
# 判定は全ての処理が終了した後にのみ出されるため、こちらを使用する（レビューが終了しなかった場合の
# Request changes には MARKER を含めない）。
#
# この実行で付与されたレビューとして数えるのは、SINCE（ISO 8601。UTC）以降に REVIEWER（既定は
# github-actions[bot]。review.py が GITHUB_TOKEN で付与する）が付与した次のものである。
#
#   inline   インラインコメントの内、スレッドの起点のもの（返信は除く。利用者とのやり取りの返信を
#            指摘として数えないため）
#   summary  `## Code review` で始まる PR へのコメント（指摘が無かった場合の 1 件）
#   clean    summary の内、「No issues found」を含むもの
#
# 投稿の直後は API の一覧に反映されていないことがあるため、inline も summary も 0 件の場合に限り、
# RETRIES 回まで 5 秒の間隔で取得し直す。
#
# 環境変数: GH_TOKEN・PR（必須）、SINCE・RETRIES（任意）
#
# 結果は GITHUB_OUTPUT に state（open / draft / closed）・verdict（true / false）・verdict_sha
# （最後の判定を出した時点の PR の head。再レビューの起点）・inline・summary・clean（件数）・
# reviewed（inline か summary が 1 件以上ならば true）として渡す。
set -euo pipefail

: "${PR:?PR (the pull request number) is required}"
repo="${GITHUB_REPOSITORY:?}"
reviewer="${REVIEWER:-github-actions[bot]}"
verdict_author="${VERDICT_AUTHOR:-github-actions[bot]}"
marker="${MARKER:-<!-- claude-review-verdict -->}"
since="${SINCE:-1970-01-01T00:00:00Z}"
retries="${RETRIES:-0}"

# --paginate はページごとに --jq の結果を出力するため、件数を合計する。
count() {
	gh api --paginate "$1" --jq "$2" | awk '{ s += $1 } END { print s + 0 }'
}

measure() {
	inline="$(count "repos/$repo/pulls/$PR/comments?per_page=100" \
		"[.[] | select(.user.login == \"$reviewer\" and .in_reply_to_id == null and .created_at >= \"$since\")] | length")"
	summary="$(count "repos/$repo/issues/$PR/comments?per_page=100" \
		"[.[] | select(.user.login == \"$reviewer\" and .created_at >= \"$since\" and (.body | ltrimstr(\"\\n\") | startswith(\"## Code review\")))] | length")"
	clean="$(count "repos/$repo/issues/$PR/comments?per_page=100" \
		"[.[] | select(.user.login == \"$reviewer\" and .created_at >= \"$since\" and (.body | ltrimstr(\"\\n\") | startswith(\"## Code review\")) and (.body | contains(\"No issues found\")))] | length")"
}

measure
attempt=0
while [ "$inline" -eq 0 ] && [ "$summary" -eq 0 ] && [ "$attempt" -lt "$retries" ]; do
	attempt=$((attempt + 1))
	echo "no review comments are visible yet; retrying (${attempt}/${retries})"
	sleep 5
	measure
done

# 一覧は古い順に返されるため、最後の行が最新の判定である。
verdict_shas="$(gh api --paginate "repos/$repo/pulls/$PR/reviews?per_page=100" \
	--jq ".[] | select(.user.login == \"$verdict_author\" and ((.body // \"\") | contains(\"$marker\"))) | .commit_id")"
verdicts="$(printf '%s' "$verdict_shas" | grep -c . || true)"
verdict_sha="$(printf '%s\n' "$verdict_shas" | tail -n 1)"

state="$(gh api "repos/$repo/pulls/$PR" \
	--jq 'if .state != "open" then "closed" elif .draft then "draft" else "open" end')"

verdict=false
[ "$verdicts" -gt 0 ] && verdict=true
reviewed=false
{ [ "$inline" -gt 0 ] || [ "$summary" -gt 0 ]; } && reviewed=true

echo "PR #$PR: state=$state verdict=$verdict${verdict_sha:+ ($verdict_sha)} reviewed=$reviewed" \
	"(since $since: inline $inline, summary $summary, clean $clean)"
if [ -n "${GITHUB_OUTPUT:-}" ]; then
	{
		echo "state=$state"
		echo "verdict=$verdict"
		echo "verdict_sha=$verdict_sha"
		echo "reviewed=$reviewed"
		echo "inline=$inline"
		echo "summary=$summary"
		echo "clean=$clean"
	} >>"$GITHUB_OUTPUT"
fi

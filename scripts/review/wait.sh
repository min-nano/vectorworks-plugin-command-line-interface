#!/usr/bin/env bash
# 自動レビュー（docs/review.md）の前に、PR の head に対する他のワークフロー（テスト・ビルド・整形・
# 検査）の完了を待機する。
#
# テストが失敗する変更をレビューしても、修正した後に再度レビューすることになるため、全てのワーク
# フローが成功してからレビューする。テストは複数のワークフローに分かれており、ワークフローを跨ぐ
# needs は記述できないため、ここで Actions の API から実行の状況を取得する。
#
# 対象は、HEAD_SHA に対する pull_request のイベントの実行の内、このワークフロー（review.yml）以外の
# ものである。同じワークフローの実行が複数ある場合（再実行・再オープン）は、最も新しいものを用いる。
# ready_for_review では他のワークフローは起動しないため、直前の push の実行を参照する。
#
# 待機の間に PR の head が変化した場合は moved とする（新しい head の実行がレビューする）。実行が
# 1 つも見つからない場合は、作成を待機するため GRACE 秒までは待機を継続する。
#
# 環境変数: GH_TOKEN・PR・HEAD_SHA（必須）、INTERVAL（秒。既定は 30）・LIMIT（秒。既定は 5400）・
# GRACE（秒。既定は 120）
#
# 結果は GITHUB_OUTPUT に result として渡す。
#
#   passed   全て成功した（success・skipped・neutral）
#   failed   失敗したものがある（名前を注記に出力する）
#   moved    PR の head が変化した
#   timeout  LIMIT 秒を経過しても完了しなかった
set -euo pipefail

: "${PR:?PR (the pull request number) is required}"
: "${HEAD_SHA:?HEAD_SHA (the head of the pull request) is required}"
repo="${GITHUB_REPOSITORY:?}"
run_id="${GITHUB_RUN_ID:?}"
interval="${INTERVAL:-30}"
limit="${LIMIT:-5400}"
grace="${GRACE:-120}"

result() {
	echo "result=$1"
	if [ -n "${GITHUB_OUTPUT:-}" ]; then
		echo "result=$1" >>"$GITHUB_OUTPUT"
	fi
	exit 0
}

workflow_id="$(gh api "repos/$repo/actions/runs/$run_id" --jq .workflow_id)"
started="$(date +%s)"

while true; do
	current="$(gh api "repos/$repo/pulls/$PR" --jq .head.sha)"
	if [ "$current" != "$HEAD_SHA" ]; then
		echo "::notice::the head of PR #${PR} moved from ${HEAD_SHA} to ${current};" \
			"the run for the new head reviews it"
		result moved
	fi

	# ワークフローごとに最も新しい実行を 1 つずつ取り出し、名前・状態・結論を 1 行ずつ出力する。
	runs="$(gh api --paginate "repos/$repo/actions/runs?head_sha=$HEAD_SHA&event=pull_request&per_page=100" |
		jq -s -r --argjson self "$workflow_id" \
			'[.[].workflow_runs[] | select(.workflow_id != $self)]
			| group_by(.workflow_id) | map(max_by(.created_at))[]
			| "\(.name)\t\(.status)\t\(.conclusion // "")"')"
	pending="$(awk -F '\t' '$2 != "completed" { print "  " $1 " (" $2 ")" }' <<<"$runs")"
	elapsed=$(($(date +%s) - started))

	if [ -n "$runs" ] && [ -z "$pending" ]; then
		failed="$(awk -F '\t' '$3 != "success" && $3 != "skipped" && $3 != "neutral" { print $1 " (" $3 ")" }' <<<"$runs")"
		if [ -n "$failed" ]; then
			echo "::notice::workflows for ${HEAD_SHA} did not succeed:" \
				"$(paste -sd ',' <<<"$failed"); the review is skipped"
			result failed
		fi
		echo "all workflows for ${HEAD_SHA} succeeded:"
		cut -f 1 <<<"$runs"
		result passed
	fi
	if [ -z "$runs" ] && [ "$elapsed" -ge "$grace" ]; then
		echo "::warning::no other workflow runs for ${HEAD_SHA} after ${grace}s; reviewing anyway"
		result passed
	fi
	if [ "$elapsed" -ge "$limit" ]; then
		echo "::error::workflows for ${HEAD_SHA} did not complete within ${limit}s"
		result timeout
	fi
	echo "waiting for workflows (${elapsed}s):"
	printf '%s\n' "${pending:-  (none yet)}"
	sleep "$interval"
done

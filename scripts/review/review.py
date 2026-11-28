"""Claude Agent SDK で公式の code-review プラグインを実行し、PR をレビューする。

review.yml の review ジョブが使用する（docs/review.md）。

claude-code-action を使用しないのは、その実体が Claude Code であり、Max / Team プランに付属する API
クレジットの対象外であるためである（Claude Agent SDK は対象）。アクションが提供していたものの内、
プラグインが必要とするもののみをここで実装する。

- プラグインのコマンドの本文（commands/code-review.md）を渡す。追記するのは、手順の 1（レビューする
  か否かの判断）で停止してよい場合を「閉じている・下書き」に限定すること（と、再レビューで変更する
  点）のみである。他の手順と、指摘を絞り込む基準はプラグインの通りとする
- インラインコメントのツール（mcp__github_inline_comment__create_inline_comment）。アクションの同名の
  ツールと同じ引数を受け付け、GitHub の API で付与する
- GitHub の OIDC トークンの取得。起動時に 1 回のみ取得し、環境変数 ANTHROPIC_IDENTITY_TOKEN として
  CLI に渡す（ファイルには書き込まない）

環境変数は起動時に確定するため、トークンを取得し直すことはできない。Anthropic のアクセストークンの
寿命は「GitHub の OIDC トークンの残りの寿命の 2 倍」まで（OIDC トークンは約 5 分であるため、約 10 分）
であり、それを経過すると CLI は同じ OIDC トークンで交換し直そうとして拒否される。すなわち、レビューが
約 10 分を超えると途中で失敗する。

認証は環境変数の WIF（ANTHROPIC_FEDERATION_RULE_ID など）に委ねる。コメントは GITHUB_TOKEN
（github-actions[bot]）の名義で付与される。

環境変数: GITHUB_REPOSITORY・GH_TOKEN・PR_NUMBER・PR_URL・PLUGIN_DIR・HEAD_SHA・
ACTIONS_ID_TOKEN_REQUEST_URL / _TOKEN（Actions が渡す）。再レビューでは PREVIOUS_SHA・REVIEW_BASE。

手順の 1 を限定するのは、プラグインが「レビューが不要（自明な変更）」「Claude が既にコメントした PR」と
判断すると、何も書き込まずに終了するためである。その場合は判定（Approve / Request changes）の材料が
無く、レビュー準備完了の PR が判定の無いまま残る。レビューするか否かはワークフローが決定している
（判定がまだ無い head に対してのみ実行する）ため、ここで重ねて判断しない。

再レビュー（前回の判定から head が変化した場合）では、PREVIOUS_SHA（前回の判定を出した時点の head）と
HEAD_SHA を受け取り、再レビューで変更する点（前回からの変更のみを確認する・前回の指摘が残っていないか
確認する）も追記する。REVIEW_BASE は差分の起点であり、前回の head が現在の head の祖先でない（force
push した）場合は空となり、PR 全体を確認する。
"""

import asyncio
import json
import os
import re
import sys
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path
from typing import Any

from claude_agent_sdk import (
	AssistantMessage,
	ClaudeAgentOptions,
	ResultMessage,
	TextBlock,
	ToolUseBlock,
	create_sdk_mcp_server,
	query,
	tool,
)

# 版を固定せず、Opus の最新の版を使用する（別名）。
MODEL = "opus"
OIDC_AUDIENCE = "https://api.anthropic.com"
BOT = "github-actions[bot]"


def required(name: str) -> str:
	value = os.environ.get(name, "")
	if not value:
		sys.exit(f"environment variable {name} is not set")
	return value


REPO = required("GITHUB_REPOSITORY")
GITHUB_TOKEN = required("GH_TOKEN")
PR_NUMBER = int(required("PR_NUMBER"))
PR_URL = required("PR_URL")
PLUGIN_DIR = Path(required("PLUGIN_DIR"))
# レビューする commit。インラインコメントの既定の付与先にもする（付与する時点の head とすると、
# レビューの間に push された commit に付与されるため）。
HEAD_SHA = required("HEAD_SHA")
PREVIOUS_SHA = os.environ.get("PREVIOUS_SHA", "")
REREVIEW = PREVIOUS_SHA != ""
REVIEW_BASE = os.environ.get("REVIEW_BASE", "")

# --- HTTP ----------------------------------------------------------------------------------------


def request(url: str, token: str, method: str = "GET", body: Any = None) -> Any:
	data = None if body is None else json.dumps(body).encode()
	req = urllib.request.Request(url, data=data, method=method)
	req.add_header("Authorization", f"Bearer {token}")
	req.add_header("Accept", "application/vnd.github+json")
	req.add_header("X-GitHub-Api-Version", "2022-11-28")
	if data is not None:
		req.add_header("Content-Type", "application/json")
	try:
		with urllib.request.urlopen(req, timeout=60) as res:
			return json.load(res)
	except urllib.error.HTTPError as error:
		# 本文に GitHub の API の message があれば、それを含める（原因を読み取れるように）。
		try:
			message = json.load(error).get("message", "")
		except ValueError:
			message = ""
		raise RuntimeError(f"{method} {url}: HTTP {error.code} {message}") from error


def github(method: str, path: str, body: Any = None) -> Any:
	return request(f"https://api.github.com/repos/{REPO}{path}", GITHUB_TOKEN, method, body)


# --- GitHub の OIDC トークン -----------------------------------------------------------------------


def fetch_identity_token() -> str:
	url = required("ACTIONS_ID_TOKEN_REQUEST_URL")
	token = required("ACTIONS_ID_TOKEN_REQUEST_TOKEN")
	audience = urllib.parse.quote(OIDC_AUDIENCE, safe="")
	return request(f"{url}&audience={audience}", token)["value"]


# --- インラインコメントのツール ------------------------------------------------------------------

INLINE_COMMENT_SCHEMA = {
	"type": "object",
	"properties": {
		"path": {
			"type": "string",
			"description": "The file path to comment on (e.g., 'src/index.js')",
		},
		"body": {
			"type": "string",
			"description": (
				"The comment text (supports markdown and GitHub code suggestion blocks). "
				"For code suggestions, use: ```suggestion\\nreplacement code\\n```. "
				"The suggestion block will REPLACE the ENTIRE line range "
				"(single line or startLine to line)."
			),
		},
		"line": {
			"type": "number",
			"minimum": 0,
			"description": (
				"Line number for single-line comments, or the end line of a multi-line comment"
			),
		},
		"startLine": {
			"type": "number",
			"minimum": 0,
			"description": "Start line for multi-line comments (use with line for the end line)",
		},
		"side": {
			"type": "string",
			"enum": ["LEFT", "RIGHT"],
			"description": "Side of the diff: LEFT (old code) or RIGHT (new code, default)",
		},
		"commit_id": {
			"type": "string",
			"description": "Commit SHA to comment on (defaults to the reviewed head)",
		},
		"confirmed": {
			"type": "boolean",
			"description": (
				"Set false to skip posting. Final review comments are posted when true or omitted."
			),
		},
	},
	"required": ["path", "body"],
}


def text_result(text: str, is_error: bool = False) -> dict[str, Any]:
	result: dict[str, Any] = {"content": [{"type": "text", "text": text}]}
	if is_error:
		result["is_error"] = True
	return result


@tool(
	"create_inline_comment",
	"Create an inline comment on a specific line or lines in a PR file",
	INLINE_COMMENT_SCHEMA,
)
async def create_inline_comment(args: dict[str, Any]) -> dict[str, Any]:
	if args.get("confirmed") is False:
		return text_result("Not posted (confirmed=false).")
	try:
		line = args.get("line")
		if not line:
			raise ValueError("'line' is required (the end line for multi-line comments)")
		side = args.get("side", "RIGHT")
		params: dict[str, Any] = {
			"body": args["body"],
			"path": args["path"],
			"side": side,
			"commit_id": args.get("commit_id") or HEAD_SHA,
			"line": int(line),
		}
		start = args.get("startLine")
		if start and start != line:
			params.update(start_line=int(start), start_side=side)
		# urllib は同期的に待機するため、別のスレッドで実行する（SDK のイベントループを止めない）。
		result = await asyncio.to_thread(github, "POST", f"/pulls/{PR_NUMBER}/comments", params)
		span = f"{int(start)}-" if start else ""
		print(f"posted an inline comment: {args['path']}:{span}{int(line)}", flush=True)
		return text_result(json.dumps({"success": True, "html_url": result.get("html_url")}))
	except (RuntimeError, ValueError, KeyError) as error:
		return text_result(f"Error creating inline comment: {error}", is_error=True)


# --- レビューの指示 ------------------------------------------------------------------------------


def procedure() -> str:
	"""プラグインのコマンドの本文。先頭の front matter（allowed-tools など）は除去する。"""
	command = (PLUGIN_DIR / "commands" / "code-review.md").read_text(encoding="utf-8")
	return re.sub(r"\A---\n.*?\n---\n", "", command, flags=re.DOTALL).strip()


# 手順の 1 で停止してよい場合。最初のレビューと再レビューで共通。
STEP_1 = (
	"In step 1, stop only if the pull request is closed or a draft. Do not stop because the change "
	"looks trivial or obviously correct (documentation-only changes included), or because Claude "
	"has already commented on this pull request: this workflow only runs when a verdict is due, "
	"and it turns the review you post into the verdict (Approve or Request changes), so it must "
	"always get one."
)

# 指摘が無かった場合のまとめのコメント（ワークフローはこれを確認して Approve する）。
SUMMARY = (
	"Use `--comment` behaviour: if no issues are found, post the summary comment in exactly the "
	'format the procedure gives (starting with "## Code review" and containing "No issues found").'
)


def review_prompt() -> str:
	return f"""Review pull request {PR_URL} (head {HEAD_SHA}) by following the code review procedure
below, with the `--comment` argument provided. Apply these changes, and pass them on to every
subagent that they affect (the first one to the step 1 agent):

1. {STEP_1}
2. {SUMMARY}

--- Code review procedure ---

{procedure()}"""


def previous_findings() -> list[dict[str, Any]]:
	"""前回までに付与した指摘（インラインコメントの内、スレッドの起点のもの。返信は除く）。

	起動時に取得するため、この実行で付与するものは含まない。
	"""
	comments: list[dict[str, Any]] = []
	page = 1
	while True:
		batch = github("GET", f"/pulls/{PR_NUMBER}/comments?per_page=100&page={page}")
		comments.extend(batch)
		if len(batch) < 100:
			break
		page += 1
	return [c for c in comments if c["user"]["login"] == BOT and c.get("in_reply_to_id") is None]


def rereview_prompt() -> str:
	findings = previous_findings()
	if findings:
		listed = "\n".join(
			f"- {c['html_url']} ({c['path']}{':' + str(c['line']) if c.get('line') else ''}, "
			f"commit {c['original_commit_id']})\n"
			+ re.sub(r"^", "  > ", c["body"], flags=re.MULTILINE)
			for c in findings
		)
	else:
		listed = "(none)"
	if REVIEW_BASE:
		scope = (
			f"Review only the changes made since the previous review: "
			f"`git diff {REVIEW_BASE} {HEAD_SHA}`. Only flag issues on lines that are part of that "
			"diff and also part of the pull request diff (`gh pr diff`), so that changes merged in "
			"from the base branch are not reviewed."
		)
	else:
		scope = (
			f"The previous head ({PREVIOUS_SHA}) is no longer an ancestor of the current head (the "
			"branch was rewritten), so review the whole pull request diff as the procedure says."
		)
	return f"""This is a RE-REVIEW of pull request {PR_URL} (current head {HEAD_SHA}).
Claude already reviewed this pull request at commit {PREVIOUS_SHA}. Follow the code review procedure
below with these changes, and pass them on to every subagent that they affect:

1. {STEP_1} (Claude's earlier comments are expected for a re-review.)
2. {scope}
3. These are the findings Claude posted in earlier reviews. For each one, check whether it still
   applies at the current head. A finding that still applies is an issue of this review: validate
   it like the others (step 5) and, if it is still valid, post a new inline comment on the current
   code that says it is still unresolved and links the original comment. Do not post anything for
   findings that have been fixed.
{listed}
4. {SUMMARY}

Read-only git commands (`git diff`, `git log`, `git show`) are available. The working directory is
the pull request merged into its base branch, with full history (the current head is in it).

--- Code review procedure ---

{procedure()}"""


# --- 実行 ----------------------------------------------------------------------------------------


async def main() -> int:
	# 指示は OIDC トークンより先に組み立てる（トークンの寿命をレビューに充てる）。
	prompt = rereview_prompt() if REREVIEW else review_prompt()

	identity_token = fetch_identity_token()
	# OIDC トークンを取得するための値を、このプロセスの環境から除去する（Claude が別のトークンを
	# 取得できないように）。Python の SDK は ClaudeAgentOptions.env を、このプロセスの環境に重ねて
	# CLI に渡すため、env から除くだけでは除去されない。
	for name in (
		"ACTIONS_ID_TOKEN_REQUEST_URL",
		"ACTIONS_ID_TOKEN_REQUEST_TOKEN",
		"ANTHROPIC_IDENTITY_TOKEN_FILE",
	):
		os.environ.pop(name, None)

	# プラグインのコマンドが宣言しているもの（gh と、インラインコメント）に、サブエージェントの起動と
	# 読み取りを加えたもの。他のものは確認せずに拒否する。
	allowed_tools = [
		"Agent",
		"Task",
		"TodoWrite",
		"Read",
		"Glob",
		"Grep",
		"Bash(gh pr view:*)",
		"Bash(gh pr diff:*)",
		"Bash(gh pr comment:*)",
		"Bash(gh pr list:*)",
		"Bash(gh issue view:*)",
		"Bash(gh issue list:*)",
		"Bash(gh search:*)",
		"mcp__github_inline_comment__create_inline_comment",
	]
	if REREVIEW:
		# 再レビューで前回からの差分を確認するため。
		allowed_tools += ["Bash(git diff:*)", "Bash(git log:*)", "Bash(git show:*)"]

	options = ClaudeAgentOptions(
		model=MODEL,
		env={"ANTHROPIC_IDENTITY_TOKEN": identity_token},
		mcp_servers={
			"github_inline_comment": create_sdk_mcp_server(
				name="github_inline_comment",
				version="1.0.0",
				tools=[create_inline_comment],
			),
		},
		allowed_tools=allowed_tools,
		permission_mode="dontAsk",
		# PR のチェックアウトにある .claude/ の設定（フックなど）は読み込まない。PR の側で書き換え
		# られるため。CLAUDE.md はプラグインがファイルとして読み込む。
		setting_sources=[],
	)

	ok = False
	async for message in query(prompt=prompt, options=options):
		if isinstance(message, AssistantMessage):
			for block in message.content:
				if isinstance(block, TextBlock) and block.text.strip():
					print(block.text, flush=True)
				elif isinstance(block, ToolUseBlock):
					print(f"-> {block.name}", flush=True)
		elif isinstance(message, ResultMessage):
			summary = {
				"subtype": message.subtype,
				"is_error": message.is_error,
				"num_turns": message.num_turns,
				"total_cost_usd": message.total_cost_usd,
				"result": message.result,
			}
			print(json.dumps(summary, ensure_ascii=False, indent=2), flush=True)
			ok = not message.is_error
	return 0 if ok else 1


if __name__ == "__main__":
	sys.exit(asyncio.run(main()))

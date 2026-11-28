// Claude Agent SDK で公式の code-review プラグインを実行し、PR をレビューする（review.yml の review
// ジョブが使用する。docs/review.md）。
//
// claude-code-action を使用しないのは、その実体が Claude Code であり、Max / Team プランに付属する API
// クレジットの対象外であるためである（Claude Agent SDK は対象）。アクションが提供していたものの内、
// プラグインが必要とするもののみをここで実装する。
//
//   - プラグインのコマンドの本文（commands/code-review.md）を渡す。追記するのは、手順の 1（レビュー
//     するか否かの判断）で停止してよい場合を「閉じている・下書き」に限定すること（と、再レビューで
//     変更する点）のみである。他の手順と、指摘を絞り込む基準はプラグインの通りとする
//   - インラインコメントのツール（mcp__github_inline_comment__create_inline_comment）。アクションの
//     同名のツールと同じ引数を受け付け、GitHub の API で付与する
//   - GitHub の OIDC トークンの取得。起動時に 1 回のみ取得し、環境変数 ANTHROPIC_IDENTITY_TOKEN として
//     CLI に渡す（ファイルには書き込まない）
//
// 環境変数は起動時に確定するため、トークンを取得し直すことはできない。Anthropic のアクセストークンの
// 寿命は「GitHub の OIDC トークンの残りの寿命の 2 倍」まで（OIDC トークンは約 5 分であるため、約
// 10 分）であり、それを経過すると CLI は同じ OIDC トークンで交換し直そうとして拒否される。すなわち、
// レビューが約 10 分を超えると途中で失敗する。
//
// 認証は環境変数の WIF（ANTHROPIC_FEDERATION_RULE_ID など）に委ねる。コメントは GITHUB_TOKEN
// （github-actions[bot]）の名義で付与される。
//
// 環境変数: GITHUB_REPOSITORY・GH_TOKEN・PR_NUMBER・PR_URL・PLUGIN_DIR・HEAD_SHA・
// ACTIONS_ID_TOKEN_REQUEST_URL / _TOKEN（Actions が渡す）。再レビューでは PREVIOUS_SHA・REVIEW_BASE。
//
// 手順の 1 を限定するのは、プラグインが「レビューが不要（自明な変更）」「Claude が既にコメントした
// PR」と判断すると、何も書き込まずに終了するためである。その場合は判定（Approve / Request changes）の
// 材料が無く、レビュー準備完了の PR が判定の無いまま残る。レビューするか否かはワークフローが決定して
// いる（判定がまだ無い head に対してのみ実行する）ため、ここで重ねて判断しない。
//
// 再レビュー（前回の判定から head が変化した場合）では、PREVIOUS_SHA（前回の判定を出した時点の head）と
// HEAD_SHA を受け取り、再レビューで変更する点（前回からの変更のみを確認する・前回の指摘が残っていないか
// 確認する）も追記する。REVIEW_BASE は差分の起点であり、前回の head が現在の head の祖先でない（force
// push した）場合は空となり、PR 全体を確認する。
import { readFile } from 'node:fs/promises';
import { createSdkMcpServer, query, tool } from '@anthropic-ai/claude-agent-sdk';
import { z } from 'zod';

// 版を固定せず、Opus の最新の版を使用する（別名）。
const MODEL = 'opus';
const OIDC_AUDIENCE = 'https://api.anthropic.com';
const BOT = 'github-actions[bot]';

function required(name) {
	const value = process.env[name];
	if (!value) throw new Error(`environment variable ${name} is not set`);
	return value;
}

const repo = required('GITHUB_REPOSITORY');
const githubToken = required('GH_TOKEN');
const prNumber = Number(required('PR_NUMBER'));
const prUrl = required('PR_URL');
const pluginDir = required('PLUGIN_DIR');
const oidcUrl = required('ACTIONS_ID_TOKEN_REQUEST_URL');
const oidcToken = required('ACTIONS_ID_TOKEN_REQUEST_TOKEN');
// レビューする commit。インラインコメントの既定の付与先にもする（付与する時点の head とすると、
// レビューの間に push された commit に付与されるため）。
const headSha = required('HEAD_SHA');
const previousSha = process.env.PREVIOUS_SHA ?? '';
const rereview = previousSha !== '';
const reviewBase = process.env.REVIEW_BASE ?? '';

// --- GitHub の OIDC トークン ---------------------------------------------------------------------

async function fetchIdentityToken() {
	const url = `${oidcUrl}&audience=${encodeURIComponent(OIDC_AUDIENCE)}`;
	const res = await fetch(url, { headers: { Authorization: `Bearer ${oidcToken}` } });
	if (!res.ok) throw new Error(`failed to get the OIDC token (HTTP ${res.status})`);
	const { value } = await res.json();
	return value;
}

// --- インラインコメントのツール ------------------------------------------------------------------

async function github(method, path, body) {
	const res = await fetch(`https://api.github.com/repos/${repo}${path}`, {
		method,
		headers: {
			Accept: 'application/vnd.github+json',
			Authorization: `Bearer ${githubToken}`,
			'X-GitHub-Api-Version': '2022-11-28',
		},
		body: body === undefined ? undefined : JSON.stringify(body),
	});
	const data = await res.json().catch(() => ({}));
	if (!res.ok) {
		throw new Error(`GitHub API ${method} ${path}: HTTP ${res.status} ${data.message ?? ''}`);
	}
	return data;
}

const createInlineComment = tool(
	'create_inline_comment',
	'Create an inline comment on a specific line or lines in a PR file',
	{
		path: z.string().describe("The file path to comment on (e.g., 'src/index.js')"),
		body: z
			.string()
			.describe(
				'The comment text (supports markdown and GitHub code suggestion blocks). ' +
					'For code suggestions, use: ```suggestion\\nreplacement code\\n```. ' +
					'The suggestion block will REPLACE the ENTIRE line range (single line or startLine to line).',
			),
		line: z
			.number()
			.nonnegative()
			.optional()
			.describe('Line number for single-line comments, or the end line of a multi-line comment'),
		startLine: z
			.number()
			.nonnegative()
			.optional()
			.describe('Start line for multi-line comments (use with line for the end line)'),
		side: z
			.enum(['LEFT', 'RIGHT'])
			.optional()
			.describe('Side of the diff: LEFT (old code) or RIGHT (new code, default)'),
		commit_id: z
			.string()
			.optional()
			.describe('Commit SHA to comment on (defaults to the reviewed head)'),
		confirmed: z
			.boolean()
			.optional()
			.describe('Set false to skip posting. Final review comments are posted when true or omitted.'),
	},
	async ({ path, body, line, startLine, side = 'RIGHT', commit_id, confirmed }) => {
		if (confirmed === false) {
			return { content: [{ type: 'text', text: 'Not posted (confirmed=false).' }] };
		}
		try {
			if (!line) throw new Error("'line' is required (the end line for multi-line comments)");
			const params = { body, path, side, commit_id: commit_id ?? headSha, line };
			if (startLine && startLine !== line) {
				Object.assign(params, { start_line: startLine, start_side: side });
			}
			const result = await github('POST', `/pulls/${prNumber}/comments`, params);
			console.log(`posted an inline comment: ${path}:${startLine ? `${startLine}-` : ''}${line}`);
			return {
				content: [{ type: 'text', text: JSON.stringify({ success: true, html_url: result.html_url }) }],
			};
		} catch (error) {
			return {
				content: [{ type: 'text', text: `Error creating inline comment: ${error.message}` }],
				isError: true,
			};
		}
	},
);

// --- レビューの指示 ------------------------------------------------------------------------------

// プラグインのコマンドの本文。先頭の front matter（allowed-tools など）は除去する。
async function procedure() {
	const command = await readFile(`${pluginDir}/commands/code-review.md`, 'utf8');
	return command.replace(/^---\n[\s\S]*?\n---\n/, '').trim();
}

// 手順の 1 で停止してよい場合。最初のレビューと再レビューで共通。
const STEP_1 =
	'In step 1, stop only if the pull request is closed or a draft. Do not stop because the change looks trivial or ' +
	'obviously correct (documentation-only changes included), or because Claude has already commented on this pull ' +
	'request: this workflow only runs when a verdict is due, and it turns the review you post into the verdict ' +
	'(Approve or Request changes), so it must always get one.';

// 指摘が無かった場合のまとめのコメント（ワークフローはこれを確認して Approve する）。
const SUMMARY =
	'Use `--comment` behaviour: if no issues are found, post the summary comment in exactly the format the ' +
	'procedure gives (starting with "## Code review" and containing "No issues found").';

async function reviewPrompt() {
	return `Review pull request ${prUrl} (head ${headSha}) by following the code review procedure
below, with the \`--comment\` argument provided. Apply these changes, and pass them on to every subagent that they
affect (the first one to the step 1 agent):

1. ${STEP_1}
2. ${SUMMARY}

--- Code review procedure ---

${await procedure()}`;
}

// 前回までに付与した指摘（インラインコメントの内、スレッドの起点のもの。返信は除く）。起動時に取得する
// ため、この実行で付与するものは含まない。
async function previousFindings() {
	const comments = [];
	for (let page = 1; ; page++) {
		const batch = await github('GET', `/pulls/${prNumber}/comments?per_page=100&page=${page}`);
		comments.push(...batch);
		if (batch.length < 100) break;
	}
	return comments.filter((c) => c.user.login === BOT && c.in_reply_to_id == null);
}

async function rereviewPrompt() {
	const findings = await previousFindings();
	const listed = findings.length
		? findings
				.map(
					(c) =>
						`- ${c.html_url} (${c.path}${c.line ? `:${c.line}` : ''}, commit ${c.original_commit_id})\n` +
						c.body.replace(/^/gm, '  > '),
				)
				.join('\n')
		: '(none)';
	const scope = reviewBase
		? `Review only the changes made since the previous review: \`git diff ${reviewBase} ${headSha}\`. ` +
			'Only flag issues on lines that are part of that diff and also part of the pull request diff ' +
			'(`gh pr diff`), so that changes merged in from the base branch are not reviewed.'
		: `The previous head (${previousSha}) is no longer an ancestor of the current head (the branch was ` +
			'rewritten), so review the whole pull request diff as the procedure says.';
	return `This is a RE-REVIEW of pull request ${prUrl} (current head ${headSha}).
Claude already reviewed this pull request at commit ${previousSha}. Follow the code review procedure below with
these changes, and pass them on to every subagent that they affect:

1. ${STEP_1} (Claude's earlier comments are expected for a re-review.)
2. ${scope}
3. These are the findings Claude posted in earlier reviews. For each one, check whether it still applies at the
   current head. A finding that still applies is an issue of this review: validate it like the others (step 5) and,
   if it is still valid, post a new inline comment on the current code that says it is still unresolved and links
   the original comment. Do not post anything for findings that have been fixed.
${listed}
4. ${SUMMARY}

Read-only git commands (\`git diff\`, \`git log\`, \`git show\`) are available. The working directory is the
pull request merged into its base branch, with full history (the current head is in it).

--- Code review procedure ---

${await procedure()}`;
}

// --- 実行 ----------------------------------------------------------------------------------------

// 指示は OIDC トークンより先に組み立てる（トークンの寿命をレビューに充てる）。
const prompt = rereview ? await rereviewPrompt() : await reviewPrompt();

// Claude に渡す環境から、OIDC トークンを取得するための値を除去し（Claude が別のトークンを取得
// できないように）、取得したトークンのみを渡す。
const env = { ...process.env, ANTHROPIC_IDENTITY_TOKEN: await fetchIdentityToken() };
delete env.ACTIONS_ID_TOKEN_REQUEST_URL;
delete env.ACTIONS_ID_TOKEN_REQUEST_TOKEN;
delete env.ANTHROPIC_IDENTITY_TOKEN_FILE;

let ok = false;
for await (const message of query({
	prompt,
	options: {
		model: MODEL,
		env,
		mcpServers: {
			github_inline_comment: createSdkMcpServer({
				name: 'github_inline_comment',
				version: '1.0.0',
				tools: [createInlineComment],
			}),
		},
		// プラグインのコマンドが宣言しているもの（gh と、インラインコメント）に、サブエージェントの
		// 起動と読み取りを加えたもの。他のものは確認せずに拒否する。
		allowedTools: [
			'Agent',
			'Task',
			'TodoWrite',
			'Read',
			'Glob',
			'Grep',
			'Bash(gh pr view:*)',
			'Bash(gh pr diff:*)',
			'Bash(gh pr comment:*)',
			'Bash(gh pr list:*)',
			'Bash(gh issue view:*)',
			'Bash(gh issue list:*)',
			'Bash(gh search:*)',
			'mcp__github_inline_comment__create_inline_comment',
			// 再レビューで前回からの差分を確認するため。
			...(rereview ? ['Bash(git diff:*)', 'Bash(git log:*)', 'Bash(git show:*)'] : []),
		],
		permissionMode: 'dontAsk',
		// PR のチェックアウトにある .claude/ の設定（フックなど）は読み込まない。PR の側で書き換え
		// られるため。CLAUDE.md はプラグインがファイルとして読み込む。
		settingSources: [],
	},
})) {
	if (message.type === 'assistant') {
		for (const block of message.message.content) {
			if (block.type === 'text' && block.text.trim()) console.log(block.text);
			if (block.type === 'tool_use') console.log(`-> ${block.name}`);
		}
	}
	if (message.type === 'result') {
		const { subtype, is_error, result, total_cost_usd, num_turns } = message;
		console.log(JSON.stringify({ subtype, is_error, num_turns, total_cost_usd, result }, null, 2));
		ok = !is_error;
	}
}
process.exit(ok ? 0 : 1);

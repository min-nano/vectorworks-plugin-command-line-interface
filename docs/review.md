# 自動レビュー

レビュー準備完了の PR を Claude がレビューし、判定（Approve / Request changes）を付与します。
`.github/workflows/review.yml` の `review` ジョブが実行します。方式は portal リポジトリのものを踏襲し、
本リポジトリの CI の構成（テストが複数のワークフローに分かれていること）に合わせて、他のワークフローの完了を
待機する手順を追加しています。

レビューの内容は Anthropic の公式の [code-review プラグイン](https://github.com/anthropics/claude-plugins-official/tree/main/plugins/code-review)
（`/code-review:code-review --comment`）の手順に委ねます。

- 並列のエージェントが **CLAUDE.md への準拠**と**変更箇所のバグ**を確認し、別のエージェントが検証して、確信度の
  低い指摘を除外する。書式や好みの指摘は出さない。
- 指摘は該当する行へのインラインコメントとして付与される。指摘が無ければ「No issues found」のコメントが 1 件
  付与される。
- その結果を **PR のレビューとして出す**。指摘があれば Request changes、無ければ Approve とする。判定は
  ワークフローが機械的に決定する（下記「判定」）。
- コメントも判定も `GITHUB_TOKEN` で付与するため、名義は `github-actions[bot]` となる。
- リポジトリ固有の観点は **`CLAUDE.md` に記述する**。プラグインはそれに照らしてレビューする。

`CLAUDE.md`「作業の進行」の通り、下書きの PR をレビュー準備完了の状態に変更する前には `/code-review` で
確認します。自動レビューはその代替ではなく、レビュー準備完了の状態に変更した後の確認です。

## 構成

| ファイル | 内容 |
| --- | --- |
| `.github/workflows/review.yml` | `plugin` ジョブ（プラグインを取り出せるかの確認）と `review` ジョブ（レビューと判定） |
| `scripts/review/wait.sh` | PR の head に対する他のワークフローの完了を待機する |
| `scripts/review/status.sh` | 前回の判定の有無と、この実行で付与されたレビューの件数・PR の状態を取得する |
| `scripts/review/review.mjs` | Claude Agent SDK でプラグインを実行する。インラインコメントのツールを提供する |
| `scripts/review/submit.sh` | 判定のレビューを出す（GitHub の API の 5xx は出し直す） |
| `scripts/review/plugin.sh` | 固定した commit からプラグインを取り出す |
| `.github/claude-plugins-official` | プラグインの固定（submodule。チェックアウトは不要） |

各段の詳細は、それぞれのファイルの注釈にあります。

- **Claude Agent SDK で実行します。** `anthropics/claude-code-action` は、その実体が Claude Code であり、
  [Max / Team プランに付属する API クレジット](https://platform.claude.com/docs/ja/about-claude/api-credits-for-subscribers)の
  対象外であるため使用しません。アクションが提供していたものの内、プラグインが必要とするインラインコメントの
  ツールは `review.mjs` が提供します。
- **プラグインと SDK の版を固定します。** プラグインの固定は git の submodule（`.gitmodules`）で行います。取得元
  には tag も release も無く、Actions でも npm のパッケージでもないため、Dependabot（`gitsubmodule`）が追跡
  できるのは submodule のみであるためです。Dependabot は週 1 回、`main` の最新の commit へ更新する PR を作成します
  （code-review に関係しない変更でも更新されます。プラグイン自体の変更は、PR の本文の commit の一覧の内、
  `plugins/code-review` に触れるものを確認します）。
- Dependabot の PR では `review` ジョブが実行されないため、更新した commit にプラグインが存在するかは `plugin`
  ジョブが毎回確認します。
- **モデルは Opus の別名（`opus`）を指定します**（`review.mjs` の `MODEL`）。版を固定せず、最新の版を使用します。
- JavaScript は `review.mjs` のみであるため、整形と検査の対象としません（[整形](format.md)・[検査](lint.md)）。
  シェルスクリプトは `scripts/` の配下であるため、他のものと同様に shfmt と shellcheck の対象です。

## 実行の条件

下書きの間はレビューしません。レビューは 1 回ごとに API の利用料が発生し、作業の途中の push のたびに実行しても
同じ指摘を繰り返すことになるためです。

1. PR は**下書きで作成**する（`CLAUDE.md`「作業の進行」）。
2. 内容が確定したら **レビュー準備完了の状態に変更する**。ここで PR 全体のレビューが 1 回実行される。
3. 指摘に対応して push すると、その変更のみが再レビューされる（下記「再レビュー」）。

実行するのは、レビュー準備完了の PR に次のイベントが発生した場合です。

| イベント | 場面 |
| --- | --- |
| `ready_for_review` | 下書きからレビュー準備完了へ変更した |
| `opened` | 最初からレビュー準備完了の状態で作成した |
| `reopened` | レビュー準備完了のまま再オープンした（閉じる前に判定が付かなかった PR を補う） |
| `synchronize` | push した |

何を行うかは前回の判定で決定します。

| 前回の判定 | 動作 |
| --- | --- |
| 無い | PR 全体をレビューする |
| あり、head が変化した | 前回の判定の head からの差分を再レビューする |
| あり、head が同一 | 何もしない（再オープン・下書きとの往復など） |

- 判定済みか否かは、判定のレビューの本文の標識（`<!-- claude-review-verdict -->`）で判断します（`status.sh`）。
  プラグインのコメントの有無で判断しないのは、途中で停止した実行を判定済みと誤認しないためです。
- 同じ PR の実行は 1 つずつ実行し、実行中のものは停止しません（途中で停止すると、コメントを一部のみ付与して判定の
  無い状態が残るため）。続けて push した場合は、待機している実行が新しいものに置き換わるため、1 回に集約され
  やすくなっています。
- bot（Dependabot など）の PR と、fork からの PR は対象外です（fork からの実行には OIDC トークンが発行されず、
  WIF を使用できないため）。
- PR のイベントでは PR の側のワークフローが実行されるため、`review` ジョブを変更する PR は、変更した後の内容で
  レビューされます。
- コメントによる起動（`issue_comment`）は設けません。`issue_comment` の実行は既定のブランチのワークフローで
  実行されるため、WIF のルールを広げる必要があり、PR のコードを特権のある実行で取り出すことにもなるためです。

### 他のワークフローの待機

**他のワークフロー（`go.yml`・`cpp.yml`・`build.yml`・`format.yml`・`lint.yml`・`hooks.yml`）が全て成功してから
レビューします。** テストが失敗する変更をレビューしても、修正した後に再度レビューすることになるためです。
ワークフローを跨ぐ `needs` は記述できないため、`wait.sh` が Actions の API で PR の head に対する実行の状況を
取得し、完了を待機します（30 秒間隔。最長 90 分）。

- 失敗したもの（`success`・`skipped`・`neutral` 以外）があれば、レビューせずに終了します。この場合 `review`
  ジョブは成功となり、判定は付与されません。失敗したワークフローを再実行して成功した場合は、**`review`
  ジョブも手動で再実行します**（自動では再実行されません）。修正を push した場合は、その push の実行がレビュー
  します。
- 待機の間に push された場合（head が進んだ場合）は、レビューせずに終了します（新しい head の実行がレビュー
  します）。
- 90 分を経過しても完了しない場合は、`review` ジョブを失敗させます。
- `ready_for_review` では他のワークフローは起動しないため、直前の push の実行の結果を参照します。

## 判定

**判定（Approve / Request changes）は必ず出します。** レビュー準備完了の PR を判定の無いまま残さないためです
（実行の間に下書きへ戻した・閉じた PR は対象外）。

| レビューの結果 | 判定 | ジョブ |
| --- | --- | --- |
| インラインコメント（指摘）がある | Request changes | 成功 |
| 「No issues found」のまとめのみ | Approve | 成功 |
| まとめはあるが「No issues found」が無い | Request changes（まとめを確認して判断する） | 成功 |
| 最後まで終了しなかった（レビューの段が失敗した・何も付与されなかった） | Request changes（**標識なし**） | 失敗 |

- プラグインの手順は、そのままでは最初に「レビューが不要（自明な変更）」「Claude が既にコメントした PR」と判断
  すると、何も書き込まずに終了します。その場合は判定の材料が無いため、その判断で停止してよい場合を「閉じている・
  下書き」に限定し、手順の本文（`commands/code-review.md`）を渡します（`review.mjs`）。レビューするか否かは
  ワークフローが決定している（判定がまだ無い head に対してのみ実行する）ため、重ねて判断させません。
- 最後まで終了しなかった場合は、確認していない変更を Approve しないよう、承認しない側に倒します。この Request
  changes には標識を付けないため、ジョブを再実行するか次に push すれば、再度（PR 全体を）レビューして判定を出し
  直します。
- 判定に数えるのは、**この実行で** `github-actions[bot]` が付与したインラインコメント（スレッドの起点のもののみ）と、
  `## Code review` で始まるまとめのコメントです。投稿の直後は API に反映されていないことがあるため、見つからなければ
  数回取得し直します。
- 判定はレビューした commit に付与します。出した時点の head に付与すると、レビューの間に push された commit が
  判定済みとなり、次の実行がその変更を省略するためです。
- 判定を出す際に GitHub の API が 5xx を返した場合は、10 秒待機し、同じ判定が付与されていなければ 3 回まで出し
  直します（`submit.sh`）。4xx は出し直しても変化しないため、直ちに失敗とします。
- **1 回のレビューは約 10 分までです。** GitHub の OIDC トークンは起動時に 1 回のみ取得し、環境変数で渡します。
  Anthropic のアクセストークンの寿命は「OIDC トークンの残りの寿命の 2 倍」まで（約 10 分）であり、環境変数は途中で
  差し替えられないため、それを経過すると取得し直しが拒否されて失敗します。

## 再レビュー

最初のレビューの後の変更（指摘への対応など）は、同じ `review` ジョブが**前回の判定より後の変更**をレビューし、
判定を出し直します。新しい判定は前の判定を置き換えます（GitHub は同じレビュアーの最新の判定を有効とする）。

- 確認するのは、前回の判定を出した時点の head から現在の head までの差分の内、PR の差分にも含まれる行です
  （ベースブランチを取り込んだ分は確認しない）。force push で前回の head が祖先でなくなっていた場合は、PR 全体を
  確認します。
- 前回までの指摘が修正されていなければ、現在のコードに改めてインラインコメントを付与します（指摘として数えるため
  Request changes となる）。修正されていれば何も付与しません。
- push のたびに API の利用料が発生します。細かな修正は、まとめてから push します。

## 初回の設定

### 1. Claude Console で WIF を設定する

Anthropic の組織の admin / owner の権限が必要です。
[Workload identity](https://platform.claude.com/settings/workload-identity-federation) → **Connect workload** →
**GitHub Actions** を選択すると、ウィザードが次の 3 つを作成します。portal リポジトリと同じ組織を使用する場合、
Federation issuer と Service account は既存のものを使用でき、Federation rule のみを本リポジトリ用に追加します。

| リソース | 設定する値 |
| --- | --- |
| Federation issuer（`fdis_...`） | Issuer URL `https://token.actions.githubusercontent.com`、JWKS は `discovery` |
| Service account（`svac_...`） | 利用料を集約するワークスペースのメンバーとする |
| Federation rule（`fdrl_...`） | 下記 |

Federation rule の match は**本リポジトリのみ**に限定します。

| 項目 | 値 | 理由 |
| --- | --- | --- |
| `subject_prefix` | `repo:min-nano@299874061/vectorworks-plugin-command-line-interface@1409984109:pull_request` | `pull_request` のイベントの `sub`（所有者とリポジトリの名前の後に数値の ID が付く形式）。レビューは PR のイベントでのみ実行するため、ここまで限定できる |
| `audience` | `https://api.anthropic.com` | `review.mjs` が OIDC トークンを要求する際の audience |
| `claims` | `repository_owner_id: 299874061`、`repository_id: 1409984109` | `sub` の解釈に依存しない確認。数値の ID は名前を変更しても変化しない |
| `oauth_scope` | `workspace:developer`（既定） | |
| `token_lifetime_seconds` | `600` 以上 | 実際の寿命は「OIDC トークンの残りの寿命の 2 倍」（約 10 分）が上限となるため、それより長くしても延長されない |

- ID は `gh api repos/min-nano/vectorworks-plugin-command-line-interface --jq '.owner.id, .id'` で確認できます。
  `sub` の実際の形式は、交換に失敗した場合の [authentication history](https://platform.claude.com/settings/workload-identity-federation?tab=history)
  にも表示されます（`match_subject_prefix` で失敗した場合は、ここと比較します）。
- `ref`（ブランチ）では限定しません。PR のイベントの `ref` は `refs/pull/<番号>/merge` となるためです。

### 2. リポジトリ変数を設定する

Settings → Secrets and variables → Actions → **Variables** に設定します。いずれも ID であって秘密ではありません
（WIF の安全性はルールの match が担います）。

| 変数 | 値 | 必須 |
| --- | --- | --- |
| `ANTHROPIC_FEDERATION_RULE_ID` | `fdrl_...` | 必須 |
| `ANTHROPIC_ORGANIZATION_ID` | 組織の UUID（Console の [Organization](https://platform.claude.com/settings/organization)） | 必須 |
| `ANTHROPIC_SERVICE_ACCOUNT_ID` | `svac_...` | 必須 |
| `ANTHROPIC_WORKSPACE_ID` | `wrkspc_...`。ルールが複数のワークスペースを対象とする場合のみ | 任意 |

```sh
gh variable set ANTHROPIC_FEDERATION_RULE_ID -R min-nano/vectorworks-plugin-command-line-interface --body fdrl_...
gh variable set ANTHROPIC_ORGANIZATION_ID -R min-nano/vectorworks-plugin-command-line-interface --body <組織の UUID>
gh variable set ANTHROPIC_SERVICE_ACCOUNT_ID -R min-nano/vectorworks-plugin-command-line-interface --body svac_...
```

- **`ANTHROPIC_API_KEY` を Secrets に配置しません。** 静的な鍵が渡されると WIF より優先されるためです。
- 変数が欠けている場合、`review` ジョブは最初の段で変数の名前を示して失敗します（WIF の交換は失敗の理由を返さない
  ため）。

### 3. GitHub Actions に PR の Approve を許可する

Settings → Actions → General → Workflow permissions の **「Allow GitHub Actions to create and approve pull
requests」** を有効にします。無効の場合は Approve の段でジョブが失敗します（Request changes は出せます）。

### 4. 利用の上限を設定する（推奨）

Service account を配置したワークスペースに、Console のワークスペースの設定で利用の上限（spend limit）を設定します。
想定外の呼び出しが継続した場合の費用を限定するためです。

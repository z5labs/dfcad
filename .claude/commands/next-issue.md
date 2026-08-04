---
description: Take the next eligible story issue through the full dev cycle — worktree, implement, test, PR, checks, Copilot review, merge.
allowed-tools: Bash, Read, Write, Edit, Glob, Grep, EnterWorktree, ExitWorktree, Monitor, TaskCreate, TaskUpdate
---

Run **exactly one** issue end-to-end, then stop. Do not start a second issue in the same
invocation — the loop re-invokes this command for the next one.

Repo: `z5labs/dfcad`. Default branch: `main`.

## 1. Pick the issue

```
gh issue list --state open --label story --limit 200 --json number,title --jq 'sort_by(.number)[]'
```

Walk the list in ascending number order. For each candidate, read its body
(`gh issue view <n>`) and look at the **Related Issues** section for `Depends on #N`
lines. An issue is *eligible* only when every issue it depends on is CLOSED
(`gh issue view <N> --json state`). Take the lowest-numbered eligible issue.

- If no open story issues remain, print `BACKLOG EMPTY` and stop. Do nothing else.
- If open issues remain but none are eligible, print `BLOCKED` plus which dependency is
  holding things up, and stop.

Read the whole issue body. The **Acceptance Criteria** checklist is the spec — every box
must be genuinely satisfied before you open the PR.

## 2. Worktree

```
EnterWorktree(name: "issue-<n>")
```

This branches fresh from `origin/main`, so previously merged work is present. Confirm with
`git rev-parse --abbrev-ref HEAD` and `git log --oneline -3`.

If the repo root has a `CLAUDE.md`, read it now — it holds the implementation conventions
later stories must follow.

## 3. Implement

Work through the acceptance criteria in order. Follow the conventions already established
in the repository — the package layout, naming and test style of code that has already
landed — rather than inventing new ones. Where the issue names a structural model, mirror
that.

Write tests alongside the implementation, in the table-driven style the issues call for.

## 4. Verify locally

All three must pass before you go further:

```
go build ./...
go vet ./...
go test -race ./...
```

Also run `gofmt -l .` and fix anything it lists. If a test fails, fix the code — never
weaken the test to make it pass. If the acceptance criteria and a passing test genuinely
conflict, stop and report rather than guessing.

## 5. Commit and open the PR

```
git add -A
git commit -m "<type>(<scope>): <summary>"   # match the issue title's prefix
git push -u origin HEAD
gh pr create --title "<issue title>" --body "<body>"
```

The PR body must include `Closes #<n>` so merging closes the issue, a short summary of the
change, and how it was verified. Keep the standard Claude Code attribution.

## 6. Wait for checks

```
gh pr checks <pr> --watch --fail-fast
```

- Exit 0 → green, continue.
- "no checks reported" → CI does not exist yet (true until issue #4 lands). Treat as pass.
- Failure → read the logs (`gh run view <id> --log-failed`), fix, push, and re-watch.
  After **three** failed attempts on the same root cause, stop and report instead of
  looping.

## 7. Request Copilot review

Copilot is a **Bot**, not a User. The REST endpoint
(`POST /pulls/<pr>/requested_reviewers` with `reviewers[]=Copilot`) returns 200 but
silently does nothing — `requested_reviewers` stays empty. Use the GraphQL `botIds` field:

```
PR_ID=$(gh pr view <pr> --json id --jq .id)
BOT_ID=$(gh api '/users/copilot-pull-request-reviewer[bot]' --jq .node_id)
gh api graphql -f query='
mutation($pr:ID!, $bot:ID!) {
  requestReviews(input: {pullRequestId: $pr, botIds: [$bot], union: true}) {
    pullRequest { reviewRequests(first:10) { nodes {
      requestedReviewer { __typename ... on Bot { login } } } } }
  }
}' -f pr="$PR_ID" -f bot="$BOT_ID"
```

The bot ID is looked up by login rather than hard-coded. Confirm the response lists
`copilot-pull-request-reviewer` under `reviewRequests` — an empty list means the request
did not take.

Then wait for the review to land. Pass this as the `command` of a `Monitor` call — not to
Bash with `run_in_background`, which has been observed exiting immediately without ever
polling:

```
for i in $(seq 1 40); do
  n=$(gh api --paginate repos/z5labs/dfcad/issues/<pr>/timeline \
        --jq '.[]|select(.event=="reviewed")
              |select(.user.login|test("copilot";"i"))|.id' 2>/dev/null | wc -l)
  if [ "$n" -gt 0 ]; then echo "copilot review landed"; exit 0; fi
  sleep 15
done
echo "copilot review timed out"; exit 1
```

Two filters, both load bearing.

`--paginate`, because the timeline returns thirty events per page by default, and a pull
request that saw several pushes, check runs and comments will push the `reviewed` event off
the first page — where an unpaginated read reports zero and the loop times out on a reviewed
pull request, which is the exact failure this step exists to avoid. Counting `.id` lines
rather than taking a `length` per page is what makes the count work across pages.

The `copilot` login filter, because a `reviewed` event from *anyone* would otherwise end the
wait. The repository owner reviewing a pull request while Copilot was still working would
satisfy this loop, and step 9 would then be deciding on a review that never arrived. The
gate is specifically that Copilot completed; the wait has to be specific in the same way.
Matching is case-insensitive on purpose — the timeline reports this reviewer as `Copilot`
while the `pulls/` endpoints report `copilot-pull-request-reviewer[bot]`.

The wait polls the **timeline**, not `pulls/<pr>/reviews`. The reviews endpoint — and the
equivalent GraphQL query — have been seen returning an empty array for as long as forty
minutes after Copilot had in fact submitted, while the timeline showed it immediately.
Polling the reviews endpoint therefore produces a timeout on a pull request that was
reviewed, and that reads as a missing review rather than as the API lagging.

**Never report `BLOCKED` for a missing review without checking the timeline first:**

```
gh api --paginate repos/z5labs/dfcad/issues/<pr>/timeline \
  --jq '.[]|select(.event=="reviewed")|select(.user.login|test("copilot";"i"))' \
  | jq -s 'sort_by(.submitted_at)|last|{user:.user.login,state,body}'
```

Same two filters, for the same two reasons — without the login filter this returns whichever
review is newest, which after a human comment is not Copilot's. And `jq -s` because it
slurps the per-page objects back into one array before sorting: sorting inside `--jq` would
sort each page separately and give you the last review *of the last page*, not the last
review.

A non-empty `reviews` array does **not** mean the PR was reviewed. Copilot posts a review
whose body declines the work — most often `"Copilot wasn't able to review this pull request
because it exceeds the maximum number of files (300)"` — and that decline satisfies the
`length > 0` test above. Check the body before treating the review as real.

Read the **most recent** Copilot review only. Reruns and pushed fixes leave older reviews
in the array, so an earlier decline sitting beside a later completed review — or the
reverse — is easy to misread:

```
gh api repos/z5labs/dfcad/pulls/<pr>/reviews \
  --jq '[.[] | select(.user.login | test("copilot";"i"))]
        | sort_by(.submitted_at) | last | .body // "no copilot review"'
```

A body matching `wasn't able to review` is a **declined** review, not a completed one.

If the review is declined, times out, or the request itself errors (Copilot review not
enabled for the org), the cycle does **not** merge — see step 9.

## 8. Address review comments

Pull both the summary review and the inline comments — a review with `"generated no
comments"` in its body still counts as having reviewed:

If the reviews endpoint is still lagging (step 7), read the body from the timeline instead —
the `reviewed` events carry the same `state` and `body`. An empty result here is a stale
endpoint, not an absent review.

```
gh api repos/z5labs/dfcad/pulls/<pr>/reviews --jq '.[] | "\(.user.login) [\(.state)]\n\(.body)"'
gh api repos/z5labs/dfcad/pulls/<pr>/comments --jq '.[] | "[\(.id)] \(.path):\(.line)\n\(.body)"'
```

Use judgment. Fix what is a real defect or a genuine improvement. Where a comment is wrong
or does not apply, reply on the thread explaining why rather than making the change — do
not silently ignore it and do not change correct code just to clear a comment.

If you push fixes, go back to step 6 and let checks re-run before merging.

## 9. Merge

Merge only when **both** hold:

1. Checks are green, and
2. A Copilot review actually **completed** — it either left comments (every one now
   addressed or answered) or reported that it generated none.

A review that was declined, never arrived, or was never requested because the request
errored is **not** a completed review. In that case do **not** label the pull request.
Leave it open, leave the worktree in place, and stop the cycle with a report beginning
`BLOCKED` that names the PR and why the review is missing. Sending unreviewed work to
`main` is the one step of this cycle that is not yours to take unilaterally — the user
resumes it once they have looked.

If the PR is unreviewable because it exceeds the 300-file limit, say so in the `BLOCKED`
report and suggest how the work could be split; do not label it anyway.

Once both conditions hold, hand the merge to CI by labelling the pull request:

```
gh pr edit <pr> --add-label auto-merge
```

Do **not** run `gh pr merge` yourself. `.github/workflows/auto-merge.yaml` picks the label
up and enables GitHub's native auto-merge, which squashes the pull request once the
required `build` check passes. This is not a formality to route around the check above: the
label is the assertion that you verified both conditions, and adding it without having done
so is the same failure as merging unreviewed work by hand.

Keeping the merge in a workflow puts the policy somewhere it can be read and changed — the
label gate plus the branch protection rule on `main` — rather than in a decision made
mid-cycle and visible only in a transcript. It is also what lets the loop run unattended:
an agent merging to `main` on its own is blocked by permission checks, and labelling is not.

Confirm the label was acted on. There are **two** successful outcomes, and checking only for
an armed auto-merge request will report a false failure on the more common one:

```
gh pr view <pr> --json state,autoMergeRequest -q '"\(.state) \(.autoMergeRequest.enabledAt // "not-armed")"'
```

- `MERGED …` — already merged. `gh pr merge --auto` merges immediately when the required
  checks have already passed, and since this cycle labels only after they pass, this is the
  usual result. `not-armed` beside `MERGED` is correct, not a fault.
- `OPEN <timestamp>` — auto-merge is armed and waiting on a check still running.
- `OPEN not-armed` — **usually just means the workflow has not started yet.** The run is
  queued for a few seconds after the label lands, and reading the pull request immediately
  will show this every time. It is not evidence of a failure on its own.

Do not conclude anything from `OPEN not-armed` until the workflow run has actually finished.
The run list is what disambiguates:

```
gh run list --repo z5labs/dfcad --workflow auto-merge.yaml --limit 1
```

Wait for it to leave `queued` and `in_progress`. A `success` conclusion with the pull
request still open means auto-merge is armed and waiting on a check. A `failure` conclusion
means the workflow itself is broken — report it, with the output of
`gh run view <id> --log-failed`, rather than falling back to a manual merge, which is what
this whole step exists to avoid.

## 10. Clean up

The merge is asynchronous: labelling queues it, and GitHub completes it when the checks
finish. Wait for it before touching the worktree. Pass the script below as the `command` of
a `Monitor` call — not to Bash with `run_in_background`, which has been observed exiting
immediately without ever polling:

```
for i in $(seq 1 40); do
  s=$(gh pr view <pr> --json state -q .state 2>/dev/null || echo "")
  case "$s" in
    MERGED) echo "PR <pr> MERGED"; exit 0;;
    CLOSED) echo "PR <pr> CLOSED without merging"; exit 1;;
  esac
  sleep 15
done
echo "PR <pr> still OPEN after 10m"; exit 1
```

Both failure paths exit non-zero so an unmerged close or a timeout cannot be mistaken for
success by anything that reads the exit code rather than the emitted line.

**Never delete the remote branch.** The `delete-merged-branch` job in
`.github/workflows/auto-merge.yaml` owns remote cleanup; leave it to do its job. `git push
--delete` is denied by user settings, and a subagent must not work around that rule. Clean
up only what you created locally: your own worktree and your own local branch (below).

Note that `deleteBranchOnMerge` on the repository does **not** cover this. It fires for a
merge a person performs, not for one the auto-merge workflow performs with its
`GITHUB_TOKEN`, which is why that job exists at all. If a branch outlives its merge, the
fix belongs in the workflow, not in a `git push` here.

If it merged, one thing the repository normally does on your behalf will not have happened:
a merge performed by the workflow's `GITHUB_TOKEN` does not close the issue the way a merge
performed by a person does. It is not a formality — do it.

**Close the issue.** A `Closes #<n>` line in the pull request body has been observed not
closing it. An issue left open is one the next invocation of this command selects again, so
the loop re-implements work it has already merged:

```
gh issue view <n> --json state -q .state
```

If that says `OPEN`, close it explicitly and say in the comment which pull request did the
work, so the trail is not just a bare state change:

```
gh issue close <n> --repo z5labs/dfcad --comment "Implemented in #<pr>, merged as <sha>."
```

Re-read the state afterwards and confirm it is `CLOSED` before moving on.

```
ExitWorktree(action: "remove")
```

`EnterWorktree`/`ExitWorktree` are unavailable when this command runs inside a subagent with
a working-directory override. In that case use the git equivalents —
`git worktree add -b issue-<n> <path> origin/main` at step 2 and
`git worktree remove <path>` here.

Then `git -C <repo root> checkout main && git pull` so the next iteration branches from the
merged state, and delete your own local branch:

```
git -C <repo root> branch -D issue-<n>
```

A stale *local* branch is what breaks a retry — `git worktree add -b issue-<n>` fails against
an existing branch, and the retry then reports `BLOCKED` on a name collision rather than on
anything real. The remote side needs nothing from you.

If the pull request did not merge, leave the worktree in place and report `BLOCKED` with the
PR number and the last state you saw.

## Report

Finish with a short status line: issue number and title, PR number and URL, check result,
whether Copilot reviewed and what it flagged, and merge confirmation. If you stopped early,
say exactly where and why.

## Stop conditions

Stop and report — do not push through — if any of these happen:

- The same CI failure survives three fix attempts.
- Acceptance criteria are ambiguous enough that two readings produce materially different
  APIs.
- Merging would require a force-push, a branch-protection override, or discarding someone
  else's commits.
- `git status` in the main checkout is dirty with changes you did not make.
- The Copilot review declined, timed out, or was never requested successfully (step 9).

When you stop for one of these, begin the report with `BLOCKED` so the caller can tell a
halted cycle from a finished one at a glance.

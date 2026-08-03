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

Then wait for the review to land. Run this with Bash `run_in_background` so you get one
notification when it finishes — do not foreground `sleep`:

```
for i in $(seq 1 40); do
  n=$(gh api repos/z5labs/dfcad/pulls/<pr>/reviews --jq 'length' 2>/dev/null || echo 0)
  if [ "$n" -gt 0 ]; then echo "copilot review landed"; exit 0; fi
  sleep 15
done
echo "copilot review timed out"; exit 1
```

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

Confirm the queue took the request:

```
gh pr view <pr> --json autoMergeRequest -q '.autoMergeRequest.enabledAt // "auto-merge NOT enabled"'
```

A timestamp means auto-merge is armed. The literal string `auto-merge NOT enabled` means the
workflow did not fire, the repository has auto-merge disabled, or `main` has no required
check for auto-merge to wait on. Report that rather than falling back to a manual merge.

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

If it merged, verify the issue closed, then clean up:

```
gh issue view <n> --json state -q .state
```

```
ExitWorktree(action: "remove")
```

`EnterWorktree`/`ExitWorktree` are unavailable when this command runs inside a subagent with
a working-directory override. In that case use the git equivalents —
`git worktree add -b issue-<n> <path> origin/main` at step 2 and
`git worktree remove <path>` here.

Then `git -C <repo root> checkout main && git pull` so the next iteration branches from the
merged state.

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

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

If the request itself errors (Copilot review not enabled for the org), note it in your
report and continue without it — do not stall the cycle.

## 8. Address review comments

Pull both the summary review and the inline comments:

```
gh api repos/z5labs/dfcad/pulls/<pr>/reviews --jq '.[] | select(.user.login=="Copilot") | .body'
gh api repos/z5labs/dfcad/pulls/<pr>/comments --jq '.[] | "\(.path):\(.line) \(.body)"'
```

Use judgment. Fix what is a real defect or a genuine improvement. Where a comment is wrong
or does not apply, reply on the thread explaining why rather than making the change — do
not silently ignore it and do not change correct code just to clear a comment.

If you push fixes, go back to step 6 and let checks re-run before merging.

## 9. Merge

Only once checks are green and every Copilot comment is either addressed or answered:

```
gh pr merge <pr> --squash --delete-branch
```

Verify the issue closed: `gh issue view <n> --json state`.

## 10. Clean up

```
ExitWorktree(action: "remove")
```

Then `git -C <repo root> checkout main && git pull` so the next iteration branches from the
merged state.

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

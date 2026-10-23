# Example CI: Architecture diff on pull requests

`diff` is non-blocking: nothing here fails the build on a violation. The
workflow below builds a model of the base and of the head, posts one sticky
comment (text plus a folded Mermaid version of the changed subgraph), and
uploads the diff-mode Explorer as an artifact.

CI does not draw the image. A PR comment can only show an image from a URL, and
`gh pr comment --attach` cannot upload with `GITHUB_TOKEN` (it fails with
`unsupported authentication type`; it works with a user token). So the image is
added from a machine where `gh` runs as a person, see
[Adding the image locally](#adding-the-image-locally).

## Workflow

```yaml
name: architecture-diff
on:
  pull_request:

permissions:
  contents: read
  pull-requests: write   # comment

jobs:
  diff:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with: { path: head, fetch-depth: 0 }
      - name: Check out the base in a worktree
        run: git -C head worktree add ../base "${{ github.event.pull_request.base.sha }}"
      - uses: actions/setup-go@v5
        with: { go-version-file: head/backend/go.mod }

      - name: Build both models
        run: |
          set -e
          (cd base/backend && go tool archmodel build --root .. --out "$GITHUB_WORKSPACE/base.json")
          (cd head/backend && go tool archmodel build --root .. --out "$GITHUB_WORKSPACE/head.json")

      - name: Diff
        run: |
          cd head/backend
          go tool archmodel diff --base "$GITHUB_WORKSPACE/base.json" --head "$GITHUB_WORKSPACE/head.json" \
            --markdown "$GITHUB_WORKSPACE/comment.md" --json "$GITHUB_WORKSPACE/diff.json"
          go tool archmodel explore --diff --base "$GITHUB_WORKSPACE/base.json" --head "$GITHUB_WORKSPACE/head.json" \
            --ref "${{ github.event.pull_request.head.sha }}" --out "$GITHUB_WORKSPACE/explorer-diff.html"

      - uses: marocchino/sticky-pull-request-comment@v2
        with:
          header: archmodel-diff
          path: comment.md

      - uses: actions/upload-artifact@v4
        with:
          name: architecture-explorer-diff
          path: |
            explorer-diff.html
            diff.json
```

Notes:

- Forks: `GITHUB_TOKEN` is read-only on fork PRs, so commenting fails. Guard the job with
  `if: github.event.pull_request.head.repo.full_name == github.repository`, or
  accept no comment on forks.
- For a gh-only comment instead of the action, find the comment containing
  `<!-- archmodel-diff -->` with `gh api repos/{owner}/{repo}/issues/{n}/comments`
  and PATCH it, else `gh pr comment --body-file comment.md`.
- Add `--coverprofile` to both builds if you want CRAP changes in the diff.

## Adding the image locally

An agent or developer with `gh` 2.99+ logged in as themselves builds the same two
models, draws the image, and attaches it. `gh` uploads the PNG as a native
attachment and rewrites the `![...](diff.png)` reference, which `archmodel diff`
writes by default (the PNG's path relative to the Markdown):

```sh
archmodel build --root <base-worktree> --out base.json
archmodel build --root .               --out head.json
archmodel diff --base base.json --head head.json --markdown diff.md --image diff
[ -f diff.png ] && gh pr comment --body-file diff.md --attach diff.png
```

This posts a second comment next to the CI one. To keep a single comment, delete
earlier ones that start with `<!-- archmodel-diff -->` first (`gh api
repos/{owner}/{repo}/issues/{n}/comments`), and have the CI job skip its own
comment, or use a different `header` for it. Do not use `--edit-last`: it edits
the last comment by the current user, which may not be the archmodel one.

Put the commands in the agent instructions (for example "after changing module
dependencies, post the architecture diff image to the PR") so it runs without
anyone remembering. GitHub serves attachments from an unguessable URL; on a
private repo an anonymous request got a 404 in our test.

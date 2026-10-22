# Example CI: Architecture diff on pull requests

`diff` is non-blocking: nothing here fails the build on a violation. The
workflow below builds a model of the base and of the head, posts one sticky
comment with the changed-subgraph image, and uploads the diff-mode Explorer as
an artifact.

## Getting the image into the comment

A PR comment can only show an image from a URL, and Actions artifacts have no
public URL. Two options:

1. **Commit the PNG to an orphan `archmodel-assets` branch** and point the
   comment at its raw URL (used below). Works for private repos too, since
   GitHub renders images in comments with the viewer's access. Create the branch
   once: `git switch --orphan archmodel-assets && git commit --allow-empty -m init && git push origin archmodel-assets`.
2. **Upload as an artifact and link it** in the comment. Nothing to maintain,
   but the image is not inline: use `--image-url` with the run URL and accept a
   link. Use this if you do not want a branch of binaries.

`archmodel diff` writes the literal `ARCHMODEL_IMAGE_URL` into the Markdown; the
workflow replaces it with `sed` once it knows the URL.

## Workflow

```yaml
name: architecture-diff
on:
  pull_request:

permissions:
  contents: write        # push the PNG to archmodel-assets
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
            --markdown "$GITHUB_WORKSPACE/comment.md" --json "$GITHUB_WORKSPACE/diff.json" \
            --image "$GITHUB_WORKSPACE/diff"
          go tool archmodel explore --diff --base "$GITHUB_WORKSPACE/base.json" --head "$GITHUB_WORKSPACE/head.json" \
            --ref "${{ github.event.pull_request.head.sha }}" --out "$GITHUB_WORKSPACE/explorer-diff.html"

      - name: Publish the image
        run: |
          [ -f diff.png ] || exit 0     # nothing changed: no image was written
          name="pr-${{ github.event.pull_request.number }}-${{ github.event.pull_request.head.sha }}.png"
          git clone --depth 1 --branch archmodel-assets \
            "https://x-access-token:${{ github.token }}@github.com/${{ github.repository }}" assets
          cp diff.png "assets/$name"
          git -C assets add "$name"
          git -C assets -c user.name=archmodel -c user.email=archmodel@users.noreply.github.com \
            commit -m "archmodel diff for #${{ github.event.pull_request.number }}"
          git -C assets push
          url="https://github.com/${{ github.repository }}/raw/archmodel-assets/$name"
          sed -i "s|ARCHMODEL_IMAGE_URL|$url|g" comment.md

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
            diff.svg
```

Notes:

- Forks: `GITHUB_TOKEN` is read-only on fork PRs, so pushing the image and
  commenting both fail. Guard the job with
  `if: github.event.pull_request.head.repo.full_name == github.repository`, or
  accept no comment on forks.
- For a gh-only comment instead of the action, find the comment containing
  `<!-- archmodel-diff -->` with `gh api repos/{owner}/{repo}/issues/{n}/comments`
  and PATCH it, else `gh pr comment --body-file comment.md`.
- Add `--coverprofile` to both builds if you want CRAP changes in the diff.
- Old PNGs accumulate on `archmodel-assets`; prune it occasionally.

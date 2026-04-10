# Create a new Terraform Provider Release

## Context

- Latest tag: !`git describe --tags --abbrev=0 2>/dev/null || echo "none"`
- Commits since last tag: !`git log $(git describe --tags --abbrev=0 2>/dev/null || echo "")..HEAD --oneline 2>/dev/null || git log --oneline`
- Full diff since last tag: !`git diff $(git describe --tags --abbrev=0 2>/dev/null)..HEAD -- '*.go' 2>/dev/null | head -500`
- Current branch: !`git branch --show-current`
- All tags: !`git tag --sort=-v:refname | head -10`

## Semver rules for Terraform providers

Analyse the commits and diff above to classify the change:

| Change type | Version bump | Examples |
|---|---|---|
| **MAJOR** (`vX.0.0`) | Breaking changes | Removed resource/data source, renamed required attribute, changed attribute type incompatibly, removed provider argument |
| **MINOR** (`vX.Y.0`) | New backwards-compatible features | New resource, new data source, new optional attribute, new provider argument |
| **PATCH** (`vX.Y.Z`) | Bug fixes & non-breaking improvements | Bug fix, documentation, refactor with same behaviour, dependency bump |

Breaking-change signals to look for:
- Deleted or renamed `schema.Resource` entries in `provider.go`, `resource_*.go`, or `data_source_*.go`
- Attribute `Required: true` added to an existing attribute
- Attribute type changed (e.g. `TypeString` → `TypeList`)
- Removed `Computed: true` from a previously computed attribute
- Changes to the Go module path or provider address

## Your task

1. **Identify** all breaking changes, new features, and bug fixes from the commits and diff.
2. **Determine** the correct semver bump (major / minor / patch) and compute the new version from the latest tag.
3. **Show** a summary of your analysis:
   - Breaking changes (if any)
   - New features (if any)
   - Bug fixes / other changes
   - Proposed new version (e.g. `v1.2.0`)
4. **Ask for confirmation** before proceeding: "Ready to tag and push `<version>`. Proceed? (yes/no)"
5. On confirmation, run **in a single step**:
   ```
   git tag <version> && git push origin <version>
   ```
   This pushes the tag to GitHub, which triggers the `release.yml` workflow (GoReleaser → Terraform Registry).
6. **Report** the GitHub Actions URL to monitor the release: `https://github.com/LaurentLesle/terraform-provider-rest/actions`

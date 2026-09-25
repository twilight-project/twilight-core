# History rewrite — 2026-09-25

On 2026-09-25, before the repository moved to the `twilight-project` organization and before any public
announcement, its git history was rewritten once with `git filter-repo` to remove stray development
artifacts that had been committed and later deleted:

- an editor swap file (`.README.md.swp`),
- a locally built binary (`dashboard`, ~79 MB, most of the repository's download size),
- an archived internal draft (`docs/specs/reward-protocol-v3.md`),
- a local home-directory path in 16 generated CLI help captures, replaced with `~/.twilightd` exactly as the
  following commit had already done.

Nothing else changed. **No source code, release content, author, date or commit message changed**, except
that a commit message quoting an old commit ID now quotes the new one. The clone shrank from ~64 MB to ~4 MB.

## Every branch and tag has an identical source tree

A commit ID covers the commit's parents, so every commit after the first removed file received a new ID.
The **tree** (the exact files of a commit) is unchanged for every branch and tag. Anyone can check a tag:

```
git rev-parse <tag>^{tree}
```

| Tag | Commit before | Commit after | Tree | Tree before vs after |
|---|---|---|---|---|
| `v0.1.0` | `b8ed78ed29f1` | `3d56910beee2` | `510306e6eb5b` | identical |
| `v0.2.0` | `85a3c157eaf8` | `056ccda2e887` | `be4c5db24d94` | identical |
| `v0.3.0-rc1` | `320e3bfc3b8a` | `8efb6a9ed30d` | `97b4ee08f361` | identical |
| `v0.3.0-rc2` | `ea83908442f3` | `87816d0a1d2d` | `c2ef0449c39e` | identical |
| `v0.3.0-rc3` | `b8f5948cbd79` | `499e7a2a5f93` | `11f1066f5cb7` | identical |
| `v0.3.0-rc4` | `aa44cb91b351` | `4373c8bc26a2` | `75765e204e38` | identical |
| `mining-v2-devnet2-baseline` | `3f0cb8bb4c31` | `f6ca463aa380` | `8e96e3a52fb9` | identical |
| `coreslot-v1-devnet-ready` | `c5dbc03c0a95` | `c5dbc03c0a95` | `484ecdbbf336` | identical |

Release binaries and `SHA256SUMS` published before this date were built from these trees and remain valid
unchanged. **A binary built before the rewrite reports its pre-rewrite commit** in
`twilightd version --long`: for example `v0.3.0-rc4` reports `aa44cb9`, which is now `4373c8b`. Use the map
below to resolve any older commit ID found in release notes, issues, pull requests or logs.

## Full commit map

[`history-rewrite-2026-09-commit-map.txt`](history-rewrite-2026-09-commit-map.txt) lists every rewritten
commit as `old new` (full SHA-1s).

# Third-Party Notices

## Product dependencies

The `rtk-go` module uses **only the Go standard library**. `go.mod` has no
`require`, `replace`, or `exclude` entries. There is no vendor directory, no
code generation step, and no auxiliary binary or script required to build or
run the product.

```sh
go list -m all
# github.com/ShawnMa123/token-trim/go
```

No Claude/Anthropic SDK, CLI, plugin, or transitive module is used, directly or
indirectly, in product, tests, fixtures, or build tooling.

## Reference material

The design documents under `../openspec/` reference the upstream Rust RTK
project (`rtk/`) for observable behavior and contract comparison. The Go
implementation is written independently:

- no upstream source files, templates, test fixtures, or generated assets are
  copied into this module;
- test fixtures are self-authored synthetic output;
- the upstream tree is not part of the Go module, build, test, or delivery
  artifact.

The upstream repository has mixed licensing (including BUSL-1.1 for the
engine); this notice does not grant or replace any company compliance review.
Any future code/fixture import from upstream must be registered here with
source commit, path, license, modification notes, reviewer, and date before
release.

## Licenses

Source in this module is distributed under the license of the parent
`token-trim` repository. Standard library code is covered by the Go project
license and is not redistributed as a separate dependency.

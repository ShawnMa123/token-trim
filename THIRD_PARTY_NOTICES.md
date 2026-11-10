# Third-Party Notices

## Product dependencies

The `rtk-go` module uses **only the Go standard library**. `go.mod` has no
`require`, `replace`, or `exclude` entries. There is no vendor directory, code
generation step, or auxiliary runtime service.

```sh
go list -m all
# github.com/ShawnMa123/token-trim
```

## Reference material

`rtk-go` is an independent Go implementation inspired by the public behavior
and design of the upstream Rust **RTK** project. No upstream source files,
templates, test fixtures, generated assets, or build artifacts are copied into
this repository or shipped with the module.

The upstream project has mixed licensing (including BUSL-1.1 for parts of its
engine). This notice is not a legal opinion and does not replace a compliance
review. Any future import of upstream code or fixtures must be recorded here
with source commit, path, license, modifications, reviewer, and date before
release.

## Test tooling

The end-to-end harness uses Docker, the Go toolchain, Codex CLI, and the local
magpie model gateway only in a disposable test environment. They are not
runtime dependencies of `rtk-go` and are not required to build or run the
product.

## Licenses

Source in this repository is distributed under the repository's license. Go
standard-library code is covered by the Go project license and is not
redistributed as a separate dependency.

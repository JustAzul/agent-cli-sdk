# Working on agentcli

## Versions

`VERSION` holds a [Semantic Versioning 2.0.0](https://semver.org) version, and
the release step refuses anything else.

Every push to `main` publishes a new build of the plugin on the `dist` branch,
which is what `claude plugin update` installs. The GitHub release and the
`v<version>` tag are created only for the first build of a version, so a change
pushed without a version bump reaches users under the previous version number
and appears in no release.

Every change to the plugin or the binary therefore bumps `VERSION` in the same
push, as Semantic Versioning defines it, with the public API being the
documented contract (commands, flags, exit codes, JSON outputs, persisted
formats, the mod's tools):

- an incompatible change to that contract bumps the major: `0.4.1` → `1.0.0`;
- a new feature that keeps it compatible bumps the minor: `0.4.1` → `0.5.0`;
- a compatible fix bumps the patch: `0.4.0` → `0.4.1`.

A version with a pre-release part, such as `0.5.0-rc.1`, is published as a
GitHub pre-release.

The bump is its own commit, `chore: release <version>`, which changes only
`VERSION` and is the last commit of the push. The release notes leave that
commit out and list the others since the previous version.

Anything under `plugin/`, `cmd/` or `internal/`, `go.mod`, `go.sum` or
`scripts/build-dist.sh` changes the build and needs a bump. A change limited to
`README.md`, `docs/`, tests, the CI workflows or the release scripts does not.

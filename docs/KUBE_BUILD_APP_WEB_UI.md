# Standalone build UI

The Go `kube-build-app` executable includes its own manifest build interface.
It uses `internal/buildapp`, the same renderer and prepared resource summary
as the CLI. It is separate from `kube-edit-app` and its model editors.

```sh
just build
./dist/kube-build-app serve \
  --root ./environments \
  --releases-root ./image-releases \
  --work-dir ./.tmp/build-web \
  --resource-policy-root ./resources \
  --image-policy strict
```

The resource policy option is optional. The default listen address is
`127.0.0.1:8787`; `--listen` changes it. Build flags such as variable sources,
replica profiles, namespace overrides and sync metadata are server inputs
and apply to builds started from this UI. Their safe context is visible in
**Server inputs**. HTTP header values and fetched variable values are not
included in that context.

`--releases-root` points to an image-release directory produced by `kube-release`
(Git is optional):

```text
image-releases/
  releases/SWD-42.yaml
  releases/SWD-43.yaml
  distributions/registry-b/SWD-42.yaml
```

Both `.yaml` and `.yml` are supported. Full native ImageRelease validation is
owned by the renderer. Invalid entries are reported and cannot be selected.
Release ID comes from the document, not the filename. A content checksum binds
the selected release; if it changed since discovery, refresh the selection.

The WebUI also supports [one-off HTTPS release loading](KUBE_BUILD_APP_URL_INPUT.cs.md).
Choose **Read from URL**, optionally supply one-off bearer/basic credentials and
an expected SHA256, then review the downloaded YAML before building. The exact
document and safe URL/checksum evidence survive alongside the build; credentials
are not retained. `--release-ca-file` adds a corporate CA without disabling TLS
verification. A [configured HTTPS source](KUBE_BUILD_APP_CONFIGURED_SOURCE.cs.md)
provides server-owned URL/path conventions and a token file, configured via YAML
or `serve` flags. Configured Git sources and direct CLI URL loading remain future stages.

## Workflow

1. Select an environment and find a release by ID or registry/path.
2. Click **Build manifests**. The spinner and log report the real running state
   and render events; no estimated completion percentage is displayed.
3. Review CPU/memory totals and the per-container table. Totals include replicas
   and use the same prepared apps as the actual render, without another variable fetch.
4. Browse and filter the generated file tree. Select a file to preview or copy it.
5. Download the complete generated output as a ZIP, or review and publish it to
   a configured deployment Git checkout. Cluster deployment remains separate.

Closing the browser does not stop the build. Reopening the page reconnects to
a running build. Completed history, results and files survive a server restart.
A build interrupted by process termination is marked **interrupted**, never
silently completed or resumed; starting a new build retries it.

Only one build runs at a time. Every run gets its own directory under `--work-dir`,
outside the environment and image-release repositories. Use a separate work
directory for each environment repository; another repository cannot reuse its
build history:

```text
<build-id>/
  release.yaml     # exact selected manifest snapshot
  build.json       # selection, checksum, builder info, safe context and result
  inputs/
    evidence.json # source/output checksums and Git provenance
    files/…       # private source and variable snapshots, named by SHA-256
  output/          # generated files, identical to the equivalent CLI build
```

The work directory is private local state. A failed build can leave partial
output there; preview and download are exposed only for succeeded builds.
Builds do not write input repositories or deployment directories. Git publication
requires a separate explicit confirmation, described below.
`serve` does not accept `--output-dir`, `--evidence-dir`, `--release-manifest` or `--release-id`:
the browser owns the selection, and outputs belong to individual runs.

## Current scope

This is a local build UI intended primarily for desktop use, with locally embedded CSS/JS. Authentication and trusted
proxy integration are not implemented yet. The renderer captures files as it
reads them; it does not snapshot entire repositories or create an atomic checkout
across repositories. This is input provenance, not a signed build attestation or
an automatic replay command. The ZIP contains rendered output; private snapshots
remain alongside it in the work directory.

Multiple manifest overlays and manually typed image overrides remain CLI features.
Argo/KDS operations, remote Git push and model editing are not implemented here.
No autopilot is required.

## Reviewed local Git publication

Optionally start `serve` with `--deployments-root /path/to/deployments`.
Use a **dedicated clean checkout on a named branch**, separate from the input
repositories and work directory. Configure its Git user name/email as usual.
Use one builder server per work directory and deployment checkout; concurrent
external writers are not supported. All existing tags must be fetched beforehand
if this checkout shares a revision sequence with other publishers.

After a successful build, **Review Git changes** shows the actual Git diff,
branch, base commit and next tag, for example `test/SWD-42.r2`.
Review freezes a Git tree without changing the checkout, its index, branch or
tags. It writes Git objects and a private review record under the build directory.
**Commit and tag locally** publishes precisely that reviewed tree, even if the
private generated files subsequently change. Start another review to include
new changes. A dirty checkout or changed branch/base is rejected.

Publication replaces the complete generated subtree for the selected environment,
including removal of obsolete files. Keep hand-maintained files outside it:

```text
deploy/test/…                       # generated manifests only
deploy/prod/…                       # untouched by publishing test
revisions/test/SWD-42.r2.json        # build evidence, outside the apply tree
```

The evidence contains the selected release snapshot/checksum, safe build context,
builder version, resource summary and the input provenance described below.
The commit's Git tree identifies the exact generated bytes. Modifying generated
files after a build invalidates a new publication review: rebuild from changed
source inputs instead. A review already created still publishes its frozen tree.

The local publication uses Git plumbing to commit the frozen tree, creates an
annotated tag without force, and fast-forwards the checkout. Commit hooks and
clean filters do not rewrite that tree; put required checks before publication
or in CI. Git hooks are disabled for the final checkout fast-forward as well.
Signing settings and Git failures can prevent publication. No credentials or
remote push are handled by this UI.

If the tag was created but the final fast-forward failed, the error reports this.
Retry the **same review** after resolving the checkout issue: the existing tag
must match the exact tree and parent. A completed retry returns the original
commit, without another revision or tag rewrite. Other tag collisions require
a fresh review. Review records survive server restart; reviews whose underlying
Git objects have been pruned must be recreated.

## Input provenance

Every new web build records **Input provenance**. Expand it to see repository
commits, file checksums and how each captured file compares with the recorded
commit: matches, modified, untracked, deleted or absent. Missing optional inputs
are recorded as absent; a repository without a commit is explicitly unpinned.
Files outside Git still have snapshots and checksums.

Capture happens at the renderer's read boundary, including defaults, app models,
resource policies, replica profiles, local assets, rollout checksum inputs and
release manifests. Repeated reads use the first captured bytes or absence.
The staged release snapshot keeps its original release repository/path as its
origin; its separate read path identifies the private copy actually used.

Git HEAD is sampled when a repository is first encountered and checked again
at the end. A changed HEAD is shown explicitly. Checksums identify the actual
consumed bytes even when files are edited during rendering; a recorded commit
alone does not claim that a mixed working tree was a pristine checkout.

The evidence also records effective rendering options, SHA-256 of each generated
file, builder version and the executable file's SHA-256 taken before rendering.
Publication checks the complete generated file inventory against these hashes.

Variable responses are fetched once and retained privately, together with the
resolved variable map actually supplied to rendering. With the default `env`
source, that map includes process environment variables. Ciphertext and an
optional sibling encjson schema are captured before decryption; the subprocess
reads private copies. Decryption keys are not copied into evidence.

Snapshot directories have mode `0700` and files `0600`. Their contents are not
served by the UI or included in its YAML ZIP or Git publication. Public metadata
contains checksums and snapshot identifiers; it does not contain variable values,
HTTP credentials or the variable endpoint URL/query. Keep the private build
directory if later investigation needs local modifications or dynamic values.
Published metadata alone cannot recover those private snapshots.

The same recorder is available for CLI builds:

```sh
./dist/kube-build-app build --root ./environments --environment test \
  --release-manifest ./image-releases/releases/SWD-42.yaml \
  --output-dir ./rendered/test --evidence-dir ./private-evidence/build-42
```

The evidence directory must be new and separate from source and output trees.
Without `--evidence-dir`, CLI behavior is unchanged. Failed builds retain captured
input metadata but do not claim successful output hashes. Older web builds keep
their existing history; they do not retroactively gain input provenance.

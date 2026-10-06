# kube-build-app

`kube-build-app` builds Kubernetes manifests from an environment repository.

The tool is intentionally small at runtime: it reads declarative environment metadata, app model files and assets, then writes Kubernetes YAML into a target directory. The Ruby implementation is the historical reference; the Go implementation is the productized CLI with a single binary, Cobra-based commands and shell completion.

This repository currently contains three related binaries, but only two are active product surfaces:

```text
kube-build-app = build/render Kubernetes manifests
kube-edit-app  = web editor for environment repositories
kube-ops-app   = sandbox prototype, not active product direction
```

`kube-edit-app` is the Go rewrite target for the Rust `kube-environments-ui` web application. See `docs/KUBE_EDIT_APP_PLAN.md`.
`kube-ops-app` is now kept as a sandbox prototype only. Useful read-only concepts should be migrated into `kube-edit-app`; sync/reconcile remains ArgoCD responsibility. See `docs/KUBE_OPS_TO_EDIT_APP_MIGRATION_PLAN.md`.

`kube-ops-app` commands use local `root_path` by default. Pass `--from-git --work-dir .tmp/kube-ops-work` to checkout `target_revision`, render from that checkout and record the resolved commit in applied state.

Start the sandbox read-only operations UI:

```bash
just ops-fixture
just ops-fixture-cluster
kube-ops-app --config .tmp/ops.yml --state .tmp/kube-ops-state/state.json --work-dir .tmp/kube-ops-work --from-git server
```

The sandbox operations UI can also read the current Kubernetes context through `kubectl` for namespace-level deployment, pod and service status. Use `--kubeconfig` and `--context` when the default context is not the desired test cluster.

## kube-edit-app Workflow

Start the web editor in safe read-only mode:

```bash
kube-edit-app serve --root ./environments
```

Enable structured writes explicitly:

```bash
kube-edit-app serve --root ./environments --allow-write
```

Recommended editing flow:

1. Select an environment.
2. Open `Apps` for app model edits, `Defaults` for `_defaults.yml` and shared definitions, or `Assets` for environment JSON and files.
3. Make a structured edit in write mode. Current editors cover local vars, defaults vars/container envs, container profiles (one tabbed form with a shared Save/Cancel for image, startup, envs, CPU/memory, HTTP probes and ports/services), shared sidecar definitions, references, replicas, autoscaling, resources, Java runtime, probes, ports/services/ingress, container envs and special env JSON entries. The `_defaults.yml` entry under Assets is a read-only source summary linking back to Defaults.
4. Use inline validation feedback to fix invalid values before saving.
5. Open `Build`, run validation and optionally render the build preview file tree.
6. Open `Changed files`, expand inline diffs and select the files to accept.
7. Use `Accept selected` to commit only the selected dirty files. The UI shows the resulting commit hash and committed paths.

Read-only mode still renders structured previews, build checks, generated file preview and diffs, but mutating controls are hidden or disabled and mutating API endpoints return `403`. This is the preferred mode for review, L2 inspection and dashboards. Use `--allow-write` only for intentional repository edits.

For repositories where each environment needs different build inputs, use a
server-side build context map:

```bash
kube-edit-app serve \
  --root ./environments \
  --build-config ./kube-edit-build.yml
```

```yaml
environments:
  dev:
    env_url: https://config.example.test/dev/render
    env_url_headers:
      - 'Authorization: Bearer token'
    release_manifest: releases/dev.yml
    resource_policy_root: policies
    image_policy: strict
  test:
    env_file: inputs/test.env
```

The map must contain exactly every environment found under `--root`. Unknown
keys and invalid contexts fail before the HTTP listener starts. Relative file
paths are resolved from the build config directory. `--build-config` cannot be
combined with per-build CLI flags; server, authentication, EncJson and
kubeconfig flags remain global. See
[`docs/KUBE_EDIT_APP_BUILD_CONTEXT.cs.md`](docs/KUBE_EDIT_APP_BUILD_CONTEXT.cs.md)
for the complete schema and security rules.

Behind a path-based reverse proxy, `--base-path /editor` serves the complete UI,
static assets and API under `/editor/`.

### Trusted Proxy Authentication

`kube-edit-app` can be protected by a trusted reverse proxy that authenticates
the browser and forwards identity through `X-Auth-*` headers:

```bash
kube-edit-app serve \
  --root ./environments \
  --trusted-proxy-auth
```

Environment variables are also supported:

```bash
KUBE_EDIT_TRUSTED_PROXY_AUTH=true
KUBE_EDIT_AUTH_HEADER_USER=X-Auth-User
KUBE_EDIT_AUTH_HEADER_EMAIL=X-Auth-Email
KUBE_EDIT_AUTH_HEADER_GROUPS=X-Auth-Groups
KUBE_EDIT_AUTH_GROUP_PREFIX=kube-edit-app
```

Supported groups:

- `kube-edit-app:role:admin`
- `kube-edit-app:env:*:reader`
- `kube-edit-app:env:*:writer`
- `kube-edit-app:env:<env>:reader`
- `kube-edit-app:env:<env>:writer`

`reader` can inspect the allowed environments. `writer` can modify the allowed
environments when the server is also started with `--allow-write`. Global Git
commit/restore operations require `kube-edit-app:role:admin`.

`env.secured.json` can be edited through the EncJson flow when `kube-edit-app` is started with configured EncJson paths. Existing encrypted values are preserved; newly added plaintext values are left for the EncJson tool to encrypt.

## Metamodel Goal

`kube-build-app` is not Helm. The app model intentionally follows an 80/20 rule:

```text
80 % of common deployment needs = clear first-class metamodel fields
20 % of special Kubernetes cases = explicit raw escape hatches
```

Prefer dedicated model fields when they exist, because they can be validated, summarized and edited by `kube-edit-app`. Use `raw:` only for Kubernetes fields that are too rare or too specific for the common model.

The long-term metamodel plan is tracked in `docs/KUBE_BUILD_APP_METAMODEL_PLAN.md`.

## Quick Start

Minimal repository layout:

```text
environments/
  test/
    env.unsecured.json
    apps/
      api.yml
    assets/
```

`environments/test/env.unsecured.json`:

```json
{
  "environment": {
    "NAMESPACE": "demo-test",
    "TSM_REGISTRY_URL": "registry.example.com/demo",
    "TSM_RELEASE_ID": "2026.05.13.1"
  }
}
```

`environments/test/apps/api.yml`:

```yaml
name: api
replicas: 1
containers:
  - name: api
    image: "{{env:TSM_REGISTRY_URL}}/api:{{env:TSM_RELEASE_ID}}"
```

Build manifests:

```bash
kube-build-app build -e test -R environments -t deploy/test
```

Generated files:

```text
deploy/test/
  deployments/api-deployment.yml
  services/
  assets/
```

If `-t/--target` is omitted, the default output directory is:

```text
<root>/<environment>/target
```

## CLI

Preferred modern commands:

```bash
kube-build-app examples
kube-build-app build -e test -R environments -t deploy/test
kube-build-app validate -e test -R environments
kube-build-app summary -e test -R environments
kube-build-app summary --summary-format json -e test -R environments
kube-build-app inventory -e test -R environments
kube-build-app list -e test -R environments
kube-build-app scaffold app api -e test -R environments
kube-build-app import -f deployment.yml -o environments/test/apps/api.yml
kube-build-app completion zsh
```

### Examples Command

Run `kube-build-app examples` for a self-contained English guide with runnable
commands and clearly marked JSON/YAML input files. It covers a first build,
inspection, defaults and references, app ports and probes, variable sources,
external resource policies, release images, sync metadata, scaffolding, and
importing an existing Deployment.

Terminal output uses color when available; redirected output is plain text.
Use `--color always` or `--color never` to override this, for example:

```bash
kube-build-app examples | less
kube-build-app examples > examples.txt
kube-build-app examples --color never
```

The catalog uses placeholder paths and registry names for workflows requiring
your own environment. The included smoke fixture example can run directly from
the source checkout.

### External Resource Policy

Resource ownership can be separated from the environment repository by using
`-P/--resource-policy-root`:

```bash
kube-build-app build \
  -e test \
  -R tsm-environments \
  -P tsm-resources \
  -t deploy/test
```

The policy repository mirrors app file paths from the environment repository:

```text
tsm-resources/
  test/
    apps/
      tsm-gateway.yml
```

`tsm-resources/test/apps/tsm-gateway.yml` contains only resources keyed by the
final container name:

```yaml
containers:
  tsm-gateway:
    cpu:
      from: "100m"
      to: "500m"
    memory:
      from: "550Mi"
      to: "850Mi"

  tsm-ui-diag:
    cpu:
      from: "10m"
      to: "100m"
    memory:
      from: "20Mi"
      to: "100Mi"
```

Without `-P`, inline `resources` keep their existing behavior. With `-P`, the
external policy is strict and authoritative: a policy file is required for
every active app, every primary container and sidecar requires an entry, and
unknown container entries fail validation. Policy resources fully replace
inline resources; they are not merged. CPU and memory `from`/`to` values are
required. `ephemeral-storage` may be added with the same `from`/`to` shape.
Generated init containers, tools, and runtime-asset fetchers retain their own
resource configuration.

`build`, `validate`, `summary`, `inventory`, and `list` all enforce the same
policy. Inventory marks effective values with
`"resources_source": "external-policy"`.

The separate repository is a governance boundary only when protected CI/CD
jobs always invoke `kube-build-app` with the required `-P` argument and access
to the policy repository is restricted separately.

### Generator Commands

`scaffold` is a non-interactive generator for creating environment and app model
files. It follows the same principle as framework scaffolding: it generates
explicit, ordinary kube-build-app metamodel YAML that can be reviewed and
edited afterwards.

Create a starter environment:

```bash
kube-build-app scaffold env \
  --root environments \
  --env dev \
  --namespace app-dev \
  --registry-url registry.example.com/project \
  --release-id latest
```

This creates:

```text
environments/_scaffold.yml
environments/dev/env.unsecured.json
environments/dev/apps/_defaults.yml
environments/dev/apps/_scaffold.yml
```

The root `_scaffold.yml` is created only when it does not exist. Creating
another environment, including with `--force`, never overwrites the shared
repository scaffold configuration. The environment file is an initially empty
override template.

Create a minimal app model:

```bash
kube-build-app scaffold app api \
  --root environments \
  --environment dev \
  --cpu-from 100m \
  --cpu-to 500m \
  --memory-from 128Mi \
  --memory-to 512Mi
```

The generated model uses the canonical resource format:

```yaml
name: api
replicas: 1
containers:
  - name: api
    image: "{{REGISTRY_URL}}/api:{{RELEASE_ID}}"
    resources:
      cpu:
        from: 100m
        to: 500m
      memory:
        from: 128Mi
        to: 512Mi
```

The image, container name and replica count can be overridden:

```bash
kube-build-app scaffold app worker \
  --root environments \
  --environment dev \
  --image 'custom/worker:1.0.0' \
  --container worker \
  --replicas 2
```

Use `--dry-run` to print the generated YAML without writing a file. Generator
commands do not overwrite existing files unless `--force` is used.

The older commands remain available as compatibility aliases:

```bash
kube-build-app skeleton env ...
kube-build-app app add api ...
```

New automation should use `scaffold env` and `scaffold app`.

#### Repository Scaffold Profiles

Application startup conventions differ between repositories. A JIB image, a
standalone JAR, a C++ application and an nginx-based utility may all use
different wrapper scripts. Shared repository conventions belong in:

```text
<environments-root>/_scaffold.yml
```

An environment can optionally override them in:

```text
<environments-root>/<environment>/apps/_scaffold.yml
```

Both files are input only for `kube-build-app scaffold app`. They are not part
of the build metamodel, are not merged with `_defaults.yml`, and changing them
does not change already existing app models. The root file is outside the
`apps` directory and the environment file begins with `_`, so neither is
loaded as an app model.

Global example:

```yaml
version: 1

defaults:
  replicas: 1
  resources:
    cpu:
      from: 100m
      to: 500m
    memory:
      from: 128Mi
      to: 512Mi

profiles:
  java-jib:
    runtime: java-jib
    image: "{{REGISTRY_URL}}/${APP_NAME}:{{RELEASE_ID}}"
    wrapper:
      source: shared
      path: /app/start-java.sh
      arguments:
        - /app/jib-classpath-file
        - /app/jib-main-class-file
    assets:
      - file: spring_configs/${APP_NAME}.json.tpl
        to: /app/${APP_NAME}.json.tpl
        transform: true

  java-jar:
    runtime: java-jar
    wrapper:
      source: shared
      path: /app/start-java.sh

  cpp:
    runtime: cpp
    wrapper:
      source: shared
      path: /app/start-cpp.sh

  binary:
    runtime: binary
    wrapper:
      source: image
      path: /usr/local/bin/${APP_NAME}
```

Environment-specific files should contain only real differences:

```yaml
version: 1
profiles:
  java-jib:
    resources:
      memory:
        to: 2Gi
```

Use a profile:

```bash
kube-build-app scaffold app tsm-calendar \
  --root environments \
  --environment test \
  --scaffold-profile java-jib
```

Profile composition order is:

1. built-in CLI defaults
2. root `_scaffold.yml` `defaults`
3. environment `_scaffold.yml` `defaults`
4. selected root profile
5. selected environment profile
6. explicitly specified CLI flags

For a profile with the same name, scalar fields and individual resource values
from the environment override the global values. Assets are replaced by their
`to` path. Sidecars are recursively merged by `name`, allowing an environment
to replace only an image while inheriting its global startup and resources.
Assets and sidecars not matching an inherited key are appended.

Profiles may also contain ordinary metamodel fragments that are copied into
the generated model:

```yaml
profiles:
  java-http:
    app:
      registry:
        - secret_name: docker-registry
      labels:
        app.kubernetes.io/component: ${APP_NAME}
    container_defaults:
      envs:
        - name: LOG_LEVEL
          value: info
      ports:
        - name: http
          port: 8080
          expose_as:
            - hostname: ${APP_NAME}
              port: 80
      probes:
        preset: spring-actuator
        port: 8080
```

`app` is merged at the application level. `container_defaults` is merged into
the generated primary container. These sections use the existing build
metamodel directly; they do not define another abstraction. Core generator
fields are reserved: `app` cannot define `name`, `replicas`, `containers` or
`sidecars`, and `container_defaults` cannot define `name`, `image`, `startup`,
`assets` or `resources`.

Only `${APP_NAME}` and `${CONTAINER_NAME}` are scaffold tokens. They are
expanded while generating the file. Existing metamodel placeholders such as
`{{REGISTRY_URL}}`, `{{RELEASE_ID}}`, `{{env:VAR}}` and `{{var:VAR}}` remain
unchanged for their normal build or deployment phase.

`runtime` is a scaffold hint and is never written to the generated app model.
Supported values are `java-jib`, `java-jar`, `cpp`, `binary` and `custom`.
Java heap sizing is deliberately not inferred from Kubernetes memory limits;
configure `runtime.java` explicitly in the generated app model when required.
Scaffold-specific configuration fields are decoded strictly, so unknown fields
and common typos fail before any app file is written. Values inside `app` and
`container_defaults` are ordinary build metamodel fragments and should be
checked with `kube-build-app validate`.

#### Wrapper Sources

A profile or CLI invocation can select one of three wrapper sources:

- `shared`: the wrapper target must already be provided by
  `<environment>/shared.assets.yml`; no app asset is generated
- `asset`: `wrapper.file` is relative to `<environment>/assets`; the generator
  adds an app asset mapping to `wrapper.path`
- `image`: the executable or script already exists in the container image; no
  asset is generated

Repository profile example for an app-owned wrapper:

```yaml
profiles:
  nginx:
    runtime: custom
    wrapper:
      source: asset
      file: utils/start-nginx.sh
      path: /app/start-nginx.sh
```

Equivalent CLI invocation:

```bash
kube-build-app scaffold app edge-proxy \
  -e test -R environments \
  --runtime custom \
  --wrapper-source asset \
  --wrapper-file utils/start-nginx.sh \
  --wrapper-path /app/start-nginx.sh
```

By default, wrappers for `java-jib`, `java-jar`, `cpp` and `custom` are started
through `/bin/sh`. A `binary` or `image` wrapper is executed directly. Use the
repeatable `--wrapper-command` flag to specify another launcher and
`--wrapper-arg` for wrapper arguments. The wrapper path becomes the first
argument when a launcher is used.

#### Assets And Sidecars

Add app-owned assets with a repeatable argument:

```bash
kube-build-app scaffold app api \
  -e test -R environments \
  --asset config/api.yml=/app/config.yml \
  --asset ssl/truststore.p12=/app/ssl/truststore.p12
```

The source is always relative to `<environment>/assets`; the destination must
be an absolute container path. Absolute sources, `..` traversal, missing source
files, symlinks resolving outside the assets directory and duplicate
destinations are rejected.

Simple sidecars can be added from the CLI:

```bash
kube-build-app scaffold app api \
  -e test -R environments \
  --sidecar metrics=registry.example.com/metrics:1 \
  --sidecar audit=registry.example.com/audit:2
```

CLI sidecars receive resource defaults configurable through
`--sidecar-cpu-from`, `--sidecar-cpu-to`, `--sidecar-memory-from` and
`--sidecar-memory-to`. For a repository-standard sidecar with startup, env,
mounts or other settings, place the complete existing sidecar metamodel
fragment in the profile:

```yaml
profiles:
  java-jib:
    sidecars:
      - name: cgroup-runtime-exporter
        image: "{{REGISTRY_URL}}/cgroup-runtime-exporter:{{RELEASE_ID}}"
        startup:
          command:
            - /usr/local/bin/cgroup-runtime-exporter
        envs:
          - name: CGROUP_EXPORTER_TARGET_PID_REGEXP
            value: java
        resources:
          cpu:
            from: 5m
            to: 25m
          memory:
            from: 8Mi
            to: 32Mi
```

The generated sidecar remains an ordinary `sidecars` metamodel entry. Scaffold
profiles do not introduce a second runtime representation.

### Importing A Deployment

Create an initial app model from an existing `apps/v1` Deployment:

```bash
kube-build-app import \
  --file deployment.yml \
  --output environments/test/apps/api.yml
```

The input can also come from stdin:

```bash
kubectl -n demo get deployment api -o yaml \
  | kube-build-app import --file - --output environments/test/apps/api.yml
```

For an authenticated current Kubernetes context, `kube-build-app` can invoke
`kubectl` directly. In this mode it reads the Deployment and Services in the
source namespace:

```bash
kube-build-app import \
  --namespace demo \
  --deployment api \
  --output environments/test/apps/api.yml
```

An import report is generated only when explicitly requested:

```bash
kube-build-app import \
  --file deployment.yml \
  --output environments/test/apps/api.yml \
  --report api.import-report.json
```

Import behavior:

- all Kubernetes containers are imported under `containers`; sidecar roles are
  never guessed
- init containers, resources, environment references, probes, scheduling and
  common pod settings are converted into structured metamodel fields
- Kubernetes resource requests and limits are written in the canonical
  metamodel form `from` and `to`
- cluster import matches Service selectors against pod-template labels and
  converts unambiguous Service ports to named `ports` and
  `expose_as[].service_name`
- source Deployment selectors are preserved through `selector_labels`, so a
  rebuilt Deployment and its generated Services use the original selector
- Kubernetes API defaults are omitted when the metamodel has the same effective
  default, including the default ServiceAccount, default probe thresholds and
  common Deployment, pod and container defaults
- values are retained when omission would change behavior; for example,
  `image_pull_policy: IfNotPresent` is preserved because the metamodel defaults
  to `Always`, and `replicas: 1` is explicit because `0` means stopped
- unsupported but reusable Deployment, pod and container fields are preserved
  through `deployment_raw`, `pod_raw` and container `raw`
- offline `--file`/stdin import reads the Deployment only and therefore cannot
  infer Services
- Ingresses, Routes, HPAs and ConfigMaps are not imported yet
- Secret references are imported, but Secret values are never requested or read

The generated model is a reviewable migration starting point, not proof that
the original object can be reconstructed byte-for-byte.

Legacy-compatible root flags are still supported:

```bash
kube-build-app -e test -R environments -t deploy/test
kube-build-app -s -e test -R environments
kube-build-app -i -e test -R environments
kube-build-app -l -e test -R environments
```

Do not mix action subcommands with legacy action flags. For example, this is intentionally invalid:

```bash
kube-build-app build -e test -s
```

Use this instead:

```bash
kube-build-app summary -e test
```

Verbose build logging:

```bash
kube-build-app build -e test -R environments -t deploy/test --verbose
kube-build-app build -e test -R environments -t deploy/test --verbose --log-format json
kube-build-app build -e test -R environments -t deploy/test --verbose --color always
```

Build logs are written to `stderr`. Normal command output stays on `stdout`.

Useful flags:

```text
-e, --environment        environment name
-R, --root               environments root directory, default: environments
-t, --target             target output directory
-p, --profile            replica profile name
    --profiles-file      replica profiles file path
-r, --release-manifest   release manifest YAML path; repeatable, later files win
    --release-id         deployment release ID; required with multiple manifests
    --image              override image as app/container=image; repeatable
    --image-policy       image override policy: fallback or strict
    --image-reference    release image reference: auto, digest, or tag
    --force-image-tag    force one tag for all release manifest images
    --force-image-prefix replace release image prefixes and keep basenames
-w, --down               scale selected app replicas to 0
-E, --env-file           explicit .env file path
    --legacy-apply-env   resolve legacy {{VAR}} in deployment and external service manifests
    --env-url            HTTP(S) URL returning .env content
    --env-url-header     HTTP header for --env-url as 'Name: value'; repeatable
    --env-url-insecure   skip TLS certificate verification for --env-url
    --vars-source        env, json, dot-env; repeatable or comma-separated
-d, --decrypt-secured    enable env.secured.json variables
    --helm-escape-assets escape remaining {{VAR}} placeholders in text assets
    --verbose            print build render events to stderr
    --log-format         verbose build log format: text or json
    --color              verbose text color: auto, always or never
```

## Image Resolution


Both `oci-toolbox/v1` (`image`, `source.image`, `registry_base`) and
`oci-toolbox/v2` (`repository`, `source.repository`, `repository_prefix`) are
accepted, as are historical unversioned v1 files. Field names must match the
version. Duplicate selectors/IDs are rejected within each file; different
files may override the same selector or reuse logical entry IDs.

Repeat `--release-manifest` to apply files in command-line order. A later file
replaces earlier resolutions only for containers it matches, including
wildcard and app-level fallback matching. Exact selectors beat wildcard and
app-level fallback within each file. CLI `--image` entries apply after all
files, even when the selector is absent from every release manifest.
They target existing containers/sidecars; they do not create containers.

```bash
kube-build-app build -e prod -R environments -t deploy/prod \
  --release-manifest releases/core.yml \
  --release-manifest releases/addons.yml \
  --release-id TSM-Core_RE2026.01.02 \
  --image 'monitoring/nginx=docker.io/library/nginx:1.27'
```

Multiple files require an explicit `--release-id` for deployment labels,
release-context ConfigMap and build variable `RELEASE_ID`. A single file still
automatically supplies its `release_id`. Explicit `--release-id` overrides
`RELEASE_ID` from variable sources; without that flag, existing conflict checks
remain in place. Public images can also use `@sha256:...` references. No
registry lookup is performed by the builder.

`kube-edit-app` uses the same resolution. Its build configuration additionally
accepts ordered `release_manifests: [core.yml, addons.yml]` and `release_id`;
the legacy single `release_manifest` key remains supported. Paths are relative
to the build configuration file. If both keys are supplied, the singular path
is applied first, followed by the list.


`kube-build-app` resolves container images per `<app>/<container>`.

Resolution precedence:

1. `--image app/container=image`
2. `--release-manifest release.yml`
3. `image:` from `<env>/apps/<app>.yml`

The direct CLI override is useful for one-off CI/CD jobs:

```bash
kube-build-app build -e test \
  --image 'tsm-dms/tsm-dms=registry.example.com/tsm-dms:2.0.0' \
  --image 'tsm-ui/tsm-ui=registry.example.com/tsm-ui@sha256:abcdef'
```

Use `=` instead of `:` between selector and image because container image references already use `:` for tags and `@sha256:...` for digests.

Release manifests are intended for controlled release pipelines. `kube-build-app` accepts the immutable manifest generated by `oci-toolbox bundle publish` or `oci-toolbox release reconstruct`:

```yaml
apiVersion: oci-toolbox/v2
kind: ImageRelease
release_id: RE_2026.07.28.01
created_at: 2026-07-28T18:00:00Z
bundle:
  name: stable
  revision: abc123
repository_prefix: registry.example.com/project
platform: linux/amd64
images:
  - id: api
    app_name: api
    container_name: api
    source:
      repository: registry-source.example.com/team/api
      tag: build-1
      digest: sha256:source
    repository: registry.example.com/project/api
    tag: RE_2026.07.28.01
    digest: sha256:target
    extra_tags: [stable]
    platform: linux/amd64
  - app_name: "*"
    container_name: cgroup-runtime-exporter
    repository: registry.example.com/project/cgroup-runtime-exporter
    tag: RE_2026.07.28.01
extra_tags: [stable]
```

The parser is strict and recognizes the audit metadata emitted by `oci-toolbox`: `created_at`, `bundle`, `platform`, image `id`, `source`, and `extra_tags`. Rendering uses only `app_name`, `container_name`, target `repository`, `digest`, and `tag`. Duplicate selectors and IDs are rejected, as is a per-image platform that conflicts with the top-level platform.

Use `app_name: "*"` for a container or sidecar shared by multiple apps. An
exact `<app_name>/<container_name>` entry takes precedence over the wildcard,
so a single app can use a different image. An app-level entry without
`container_name` remains a fallback for the app's primary containers and does
not override sidecars.

When `digest` is present, the generated deployment uses an immutable digest reference:

```text
registry.example.com/project/tsm-dms@sha256:abcdef
```

When only `tag` is present, the generated deployment uses:

```text
registry.example.com/project/tsm-dms:2026.06.25.01
```

When the release manifest contains `release_id`, `kube-build-app` also exposes
it as the build-time variable `RELEASE_ID`. This avoids duplicating release
metadata in a `.env` file:

```yaml
labels:
  app.kubernetes.io/version: "{{env:RELEASE_ID}}"
```

If an external variable source also defines `RELEASE_ID`, its value must match
the release manifest. A mismatch fails the build instead of producing
inconsistent image and metadata versions. Without `--release-manifest`,
`RELEASE_ID` continues to come only from the configured external variable
sources.

`--release-manifest` reads the image `release_id` from the manifest content.
`--customer-release-id TSM-Core_RE61_SP_04.01` supplies the independent
customer release ID. The generated Deployment metadata carries
`app.kubernetes.io/release-id` and, when supplied,
`app.kubernetes.io/customer-release-id` labels. Both values must fit Kubernetes
label syntax (at most 63 characters). Neither label is added to the Deployment
selector or Pod template.

With a release manifest, the build also creates one stable ConfigMap named
`release-context` by default, with `RELEASE_ID` and optional
`CUSTOMER_RELEASE_ID` data. Every regular container, including sidecars, gets
explicit `configMapKeyRef` environment entries. The customer entry is optional
and remains in the Pod template even when no customer ID is assigned. Init
containers are not injected. Updating the ConfigMap does not change existing
container environments or trigger a Deployment rollout; a newly created Pod
reads the current values. The ConfigMap has sync order 100, before Deployments
at order 200. Remove old manual Downward API entries for these two ENV names
from app models; conflicting entries fail the build.

Set `release_context_name: release-context-dev` at the top level of
`<environment>/apps/_defaults.yml` to give a build a stable name when multiple
environment repositories target the same namespace. `--release-context-name`
overrides this setting. The other defaults can be adjusted independently:

```bash
kube-build-app build -e test --release-manifest release.yml \
  --release-context-name tsm-release-context \
  --release-id-env-name RELEASE_ID \
  --customer-release-id-env-name CUSTOMER_RELEASE_ID
```

`--no-release-context-configmap` uses an externally managed ConfigMap while
keeping ENV references. `--no-release-context-env` keeps ConfigMap generation
but skips injection. Both flags together disable the runtime release context.
When renaming ENV variables, configure `simple-idm-token-proxy` with
`SIMPLE_IDM_TOKEN_PROXY_RELEASE_ID_ENV_NAME` and
`SIMPLE_IDM_TOKEN_PROXY_CUSTOMER_RELEASE_ID_ENV_NAME` accordingly. Existing
Pods retain the context with which they started; a one-time rollout occurs when
switching from the old Pod-label/Downward API scheme to stable references.

Image policy:

```bash
kube-build-app build -e test --release-manifest release.yml --image-policy fallback
kube-build-app build -e test --release-manifest release.yml --image-policy strict
kube-build-app build -e test --release-manifest release.yml --image-reference tag
kube-build-app build -e test --release-manifest release.yml \
  --force-image-prefix artifactory.example.com/docker-release \
  --force-image-tag emergency-1
```

`fallback` keeps the app YAML image when no override exists. `strict` requires every primary app `containers[]` image to be covered by `--image` or `--release-manifest`; this is recommended for release pipelines. Sidecar images may still be overridden from the release manifest, but `strict` does not require sidecars to be listed because they are often deterministic platform/helper images from the app model.

`--image-reference auto` is the default and prefers an immutable digest, then a tag, then the bare image name. `digest` and `tag` explicitly require that reference type in every matched release image and fail when it is missing.

The force flags provide an explicit recovery mode. `--force-image-prefix` replaces the complete repository prefix while preserving only the final image basename. `--force-image-tag` then replaces every manifest digest or tag with one shared tag. They affect only images selected from `--release-manifest`; per-container `--image` overrides still have the highest priority. Prefixes use OCI reference syntax without `https://` or `docker://`. `--force-image-tag` cannot be combined with `--image-reference digest`.

Repositories with the same basename map to the same destination when a forced prefix is used. For example, both `team-a/api` and `team-b/api` become `<forced-prefix>/api`; this accepted limitation should be checked in the test environment before production rollout.

## Placeholder Contract

There are three placeholder forms. They have different scopes and different timing.

| Placeholder | Resolved by | Timing | Meaning |
| --- | --- | --- | --- |
| `{{VAR}}` | `apply-env`, CI/CD, deploy phase, Helm-safe post-processing | later | Runtime/deploy placeholder. `kube-build-app` leaves it unresolved in app YAML. |
| `{{env:VAR}}` | `kube-build-app` | build time | Build-time environment variable loaded from env JSON, `.env` or process env according to configured sources. |
| `{{var:VAR}}` | `kube-build-app` | build time | App-local variable from the `vars:` block in the current `<app>.yml`, after `_defaults.yml` merge. |

Use this rule of thumb:

```text
prefixed placeholder = resolve during kube-build-app
plain placeholder    = keep for later deployment/rendering phase
```

Example:

```yaml
vars:
  - name: APP_NAME
    value: api

name: "{{var:APP_NAME}}"
containers:
  - name: "{{var:APP_NAME}}"
    image: "{{env:TSM_REGISTRY_URL}}/{{var:APP_NAME}}:{{env:TSM_RELEASE_ID}}"
    envs:
      - name: RUNTIME_VALUE
        value: "{{RUNTIME_VALUE}}"
```

Resulting deployment keeps only the plain runtime placeholder:

```yaml
containers:
  - name: api
    image: registry.example.com/demo/api:2026.05.13.1
    env:
      - name: RUNTIME_VALUE
        value: "{{RUNTIME_VALUE}}"
```

### Container Environment Variables

Use `containers[].envs` for container environment variables. The historical
`containers[].env_vars` spelling remains supported indefinitely for existing
environment repositories, but emits a deprecation warning.

If both keys are present in one container, values are merged by variable name:
`env_vars` is applied first and `envs` wins on a duplicate name.

Use this in CI to prevent new uses of the historical spelling:

```bash
kube-build-app validate -e test -R environments --fail-on-deprecated
```

## Variable Sources

Default behavior is backward-compatible:

```text
env.unsecured.json + env.secured.json when -d is enabled, then process environment
```

Explicit `.env` file:

```bash
kube-build-app build -e test -E /path/to/release.env
```

This is the recommended production workflow for secured variables. The decrypting tool can be anything; `kube-build-app` receives already resolved key/value pairs.

In this mode the explicit `.env` is the only variable source. It cannot be combined with `-d` or `--vars-source`.

Older environment repositories can opt into compatible post-processing after loading
the explicit `.env` file:

```bash
kube-build-app build -e test \
  -E /path/to/release.env \
  --legacy-apply-env
```

`--legacy-apply-env` resolves bare `{{VAR}}` placeholders only in generated files
under `deployments/` and `services/external/`, matching the historical post-build
`apply-env` script scope. An unknown variable or invalid resulting YAML fails the
build. Without this explicit flag, the standard placeholder contract is unchanged and
bare placeholders remain available for a later deployment stage.

Remote `.env` source:

```bash
kube-build-app build -e test \
  --env-url "https://server/public/v1/tenants/acme/environments/test/export-profiles/default/render" \
  --env-url-header "Authorization: Bearer $TOKEN"
```

In this mode the downloaded `.env` is the only variable source. It cannot be combined with `-E`, `-d` or `--vars-source`.
For internal self-signed HTTPS endpoints, add `--env-url-insecure` to skip TLS certificate verification.

Target namespace override:

```bash
kube-build-app build -e test --namespace customer-test
```

`--namespace` has the highest priority and overrides `NAMESPACE` from every
variable source, including `--env-file` and `--env-url`. When the flag is not
specified, the existing `NAMESPACE` variable behavior is unchanged. The
environment selected by `-e/--environment` and the generated Kubernetes
namespace are independent values.

Backward-compatible secured JSON decrypt:

```bash
kube-build-app build -e test -d
```

When `-d/--decrypt-secured` is used, `env.secured.json` is decrypted by executing an external EncJson binary:

```text
encjson decrypt -k <keydir> -f env.secured.json
encjson-rs decrypt -k <keydir> -f env.secured.json
```

Binary selection:

```text
EncJson[@api=1.0  -> ENCJSON_LEGACY_PATH, ENCJSON_LEGACY_BIN, ENCJSON_BIN, fallback encjson
EncJson[@api=2.0  -> ENCJSON_PATH, ENCJSON_RS_BIN, fallback encjson-rs
unknown marker    -> ENCJSON_BIN or legacy fallback
```

Key directory:

```text
if ENCJSON_KEYDIR is set: decrypt -k "$ENCJSON_KEYDIR" -f env.secured.json
otherwise:                 decrypt -f env.secured.json
```

When `ENCJSON_KEYDIR` is not set, key directory selection is delegated to the EncJson utility itself.

Explicit source selection:

```bash
kube-build-app build -e test --vars-source json
kube-build-app build -e test --vars-source env
kube-build-app build -e test --vars-source json --vars-source env
kube-build-app build -e test --vars-source json,env
```

Supported sources:

```text
json     env.unsecured.json and env.secured.json when -d is enabled
env      process environment
dot-env  <environment_dir>/.env unless -E/--env-file is used
```

## App Model by Composition

An app model is one YAML file in:

```text
<environment>/apps/<app>.yml
```

Files starting with `_` are special files, not apps. For example:

```text
<environment>/apps/_defaults.yml
```

### 1. Minimal App

```yaml
name: api
containers:
  - name: api
    image: nginx:stable
```

Generated deployment contains one container:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api
spec:
  template:
    spec:
      containers:
        - name: api
          image: nginx:stable
```

### 2. Replicas

Add:

```yaml
replicas: 3
```

Generated deployment contains:

```yaml
spec:
  replicas: 3
```

### 3. Resources

Add container resources with Kubernetes-aligned `requests` / `limits`:

```yaml
containers:
  - name: api
    image: nginx:stable
    resources:
      cpu:
        requests: "100m"
        limits: "500m"
      memory:
        requests: "128Mi"
        limits: "512Mi"
```

Legacy `from` / `to` remains supported:

```yaml
containers:
  - name: api
    image: nginx:stable
    resources:
      cpu:
        from: "100m"
        to: "500m"
      memory:
        from: "128Mi"
        to: "512Mi"
```

Generated deployment contains:

```yaml
resources:
  requests:
    cpu: 100m
    memory: 128Mi
  limits:
    cpu: 500m
    memory: 512Mi
```

The summary command uses these values and multiplies totals by replicas:

```bash
kube-build-app summary -e test -R environments
```

#### Autoscaling / HPA

Use app-level `autoscaling` when runtime replica count should be controlled by Kubernetes HPA:

```yaml
replicas: 2
autoscaling:
  enabled: true
  min_replicas: 2
  max_replicas: 6
  cpu:
    average_utilization: 75
  memory:
    average_utilization: 80
containers:
  - name: api
    image: nginx:stable
    resources:
      cpu:
        requests: "100m"
        limits: "500m"
      memory:
        requests: "128Mi"
        limits: "512Mi"
```

Generated manifests contain an additional `deployments/<app>-hpa.yml`:

```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
spec:
  minReplicas: 2
  maxReplicas: 6
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: api
```

If `autoscaling.enabled=true`, the runtime replica count is controlled by HPA. The `replicas` value remains in the generated Deployment/StatefulSet as the initial desired state, but HPA can change it after the object is running.

CPU and memory utilization metrics require container `resources.requests`. Use `autoscaling.raw` for advanced HPA fields such as `spec.behavior`:

```yaml
autoscaling:
  enabled: true
  min_replicas: 2
  max_replicas: 6
  cpu:
    average_utilization: 75
  raw:
    spec:
      behavior:
        scaleDown:
          stabilizationWindowSeconds: 300
```

### 4. Container Environment Variables

Add:

```yaml
containers:
  - name: api
    image: nginx:stable
    envs:
      - name: JAVA_OPTS
        value: "-Xms256m -Xmx512m"
      - name: POD_NAME
        field_path: metadata.name
```

Generated deployment contains:

```yaml
env:
  - name: JAVA_OPTS
    value: "-Xms256m -Xmx512m"
  - name: POD_NAME
    valueFrom:
      fieldRef:
        fieldPath: metadata.name
```

#### Java Runtime Options

For Java containers, prefer `runtime.java` for well-known JVM memory options instead of hand-writing `JAVA_OPTS`:

```yaml
containers:
  - name: api
    image: nginx:stable
    runtime:
      java:
        xms: "512m"
        xmx: "2048m"
        opts:
          - "-XX:+UseG1GC"
        export:
          env_name: JAVA_OPTS
    resources:
      cpu:
        requests: "100m"
        limits: "1000m"
      memory:
        requests: "1024Mi"
        limits: "2560Mi"
```

Generated deployment contains:

```yaml
env:
  - name: JAVA_OPTS
    value: "-Xms512m -Xmx2048m -XX:+UseG1GC"
```

`export.env_name` defaults to `JAVA_OPTS`. If the same variable is also specified manually in `vars`, validation fails. This keeps JVM heap sizing visible in `kube-build-app summary` and avoids hidden conflicts with startup scripts.

### 5. Ports and Services

Add a container port:

```yaml
containers:
  - name: api
    image: nginx:stable
    ports:
      - name: http
        port: 8080
```

Generated deployment contains `containerPort: 8080`.

To create a service, add `expose_as`:

```yaml
ports:
  - name: http
    port: 8080
    expose_as:
      - service_name: api
        port: 80
```

Generated service:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: api
spec:
  ports:
    - name: http-80
      port: 80
      targetPort: 8080
```

`service_name` is the Kubernetes Service name and the short DNS name inside the namespace. Legacy `expose_as[].hostname` is still accepted as an alias for backward compatibility.

External exposure can be attached under `expose_as[].external` and generates Ingress or OpenShift Route according to the model.

### 6. Probes

Preferred modern form:

```yaml
containers:
  - name: api
    image: api:latest
    probes:
      preset: spring-actuator
      port: 8080
```

This generates:

```text
livenessProbe  -> /actuator/health/liveness
readinessProbe -> /actuator/health/readiness
startupProbe   -> /actuator/health
```

Generic HTTP form:

```yaml
probes:
  preset: http
  port: 8080
  path: /healthz
```

Override form:

```yaml
probes:
  http:
    path: /actuator/health
    port: 8080
  live:
    period: 10
    timeout: 2
    failure: 5
  ready:
    period: 2
    timeout: 2
    success: 2
    failure: 2
  start:
    period: 10
    timeout: 2
    failure: 30
```

Exec probes:

```yaml
probes:
  live:
    command: ["/bin/sh", "/app/liveness.sh"]
  ready:
    command: ["/bin/sh", "/app/liveness.sh"]
  start:
    command: ["/bin/sh", "/app/liveness.sh"]
    failure: 30
```

Legacy `health` and `probe` blocks remain supported for backward compatibility:

```yaml
containers:
  - name: api
    image: nginx:stable
    health:
      http:
        path:
          live: /health/live
          ready: /health/ready
        port: 8080
```

`health` generates liveness/readiness only. `probe` can generate liveness/readiness/startup but is more verbose. New files should prefer `probes`.

### 7. Assets

Place a file under:

```text
<environment>/assets/nginx.conf
```

Reference it from the app:

```yaml
containers:
  - name: api
    image: nginx:stable
    assets:
      - file: assets/nginx.conf
        to: /etc/nginx/conf.d/default.conf
```

Generated output includes:

```text
assets/api-asset-<crc>.yml
```

and deployment gets a volume mount:

```yaml
volumeMounts:
  - mountPath: /etc/nginx/conf.d/default.conf
    name: api-asset-<crc>
    readOnly: true
    subPath: nginx.conf
volumes:
  - name: api-asset-<crc>
    configMap:
      name: api-asset-<crc>
```

Asset name digest is based on the real asset input and mount target. This keeps ConfigMap names stable and changes them only when the mounted asset content changes.

Text assets can be transformed with build-time variables:

```yaml
assets:
  - file: assets/application.yml.tpl
    to: /app/config/application.yml
    transform: true
```

Legacy `assets` also supports volume-only forms such as `temp`, `pvc`, `nfs-server` and `host-path`. They remain supported for backward compatibility, but new files should prefer `mounts` for volume declarations.

### 8. Mounts

Use `mounts` for non-ConfigMap volumes and for a clearer modern form of file mounts:

```yaml
containers:
  - name: api
    image: api:latest
    mounts:
      - type: config
        file: assets/app.conf
        mount_path: /app/app.conf

      - type: empty_dir
        name: cache
        mount_path: /app/cache

      - type: pvc
        name: data
        claim_name: data-claim
        mount_path: /data

      - type: nfs
        name: data-nfs
        server: nfs.local
        path: /export/data
        mount_path: /nfs

      - type: host_path
        name: host-data
        path: /var/lib/host-data
        mount_path: /host
```

`host_path` is a high-risk escape hatch because it exposes node filesystem paths to the container.

For rare Kubernetes volume types, use raw mount passthrough:

```yaml
mounts:
  - type: raw
    volume:
      name: special
      projected:
        sources: []
    mount:
      name: special
      mountPath: /app/special
      readOnly: true
```

### 9. Shared Assets

Use `<environment>/shared.assets.yml` for assets mounted into multiple apps.
Assign `name` when another metamodel block needs to reference the asset:

```yaml
assets:
  - name: internal-ca
    file: assets/ssl/internal-ca.pem
    to: /var/run/certs/internal-ca.pem
```

Shared asset names are optional, but defined names must be unique Kubernetes
DNS labels. Fields ending in `_ref_name` resolve another declaration by its
`name`; fields ending in `_ref_names` contain a list of such references.

The reference contract intentionally removed ambiguous keys:

| Removed key | Replacement |
|---|---|
| `envs[].workload_identity_token` | `workload_identity_token_ref_name` |
| `runtime_assets` | `runtime_asset_definitions` + `runtime_asset_ref_names` |
| `runtime_assets[].source.token` | `workload_identity_token_ref_name` |
| `runtime_assets[].apps` | app-level `runtime_asset_ref_names` |
| `runtime_assets[].containers` | `containers[].runtime_asset_ref_names` |
| `_defaults.yml sidecars` | `sidecar_definitions` + app-level `sidecar_ref_names` |
| `container_envs[].name` | `container_ref_name` |
| `replica-profiles.yml defaults.profile` | `replica_profile_ref_name` |

Using a removed key fails validation with a migration message.

An app can opt out:

```yaml
disable_shared_assets: true
```

### 10. Tools

Static utility binaries can be copied by initContainers and mounted at an exact path in every app container:

```yaml
tools:
  - name: util-apply-env
    image: registry.example.com/tools/apply-env:latest
    expose_bin: /usr/bin/apply-env
    mount_path: /usr/local/bin/apply-env
    image_pull_policy: Always
    resources:
      cpu: { requests: "10m", limits: "100m" }
      memory: { requests: "16Mi", limits: "128Mi" }
```

Behavior:

- every tool becomes an initContainer
- the initContainer copies `expose_bin` into a shared `emptyDir`
- app containers mount the copied file read-only at `mount_path` via `subPath`
- `mount_path` is required and must be an absolute file path
- `image_pull_policy` defaults to `Always`; accepted values are `Always`, `IfNotPresent` and `Never`
- `as` is removed and rejected; it must be replaced with `mount_path`
- every tool gets default resources (`10m`/`100m` CPU and `16Mi`/`128Mi` memory), so it is valid in namespaces with a `ResourceQuota`; `resources` can override individual values

`image_pull_policy` uses the same values and default for `containers`,
`sidecars`, `init_containers` and `tools`.

### 11. Scheduling and Pod Metadata

Common app-level fields:

```yaml
labels:
  team: tsm
annotations:
  app.example.com/owner: l2
pod_annotations:
  prometheus.io/scrape: "true"
arch: amd64
node_selector:
  node-role.kubernetes.io/worker: ""
tolerations:
  - key: dedicated
    operator: Equal
    value: tsm
    effect: NoSchedule
```

These are rendered into deployment metadata and pod template fields.

Preferred modern scheduling form:

```yaml
scheduling:
  arch: amd64
  node_selector:
    node-role.kubernetes.io/worker: ""
  tolerations:
    - key: dedicated
      operator: Equal
      value: tsm
      effect: NoSchedule
  spread:
    by: hostname
    max_skew: 1
    when_unsatisfiable: ScheduleAnyway
  anti_affinity:
    self: preferred
    topology: kubernetes.io/hostname
```

For rare Kubernetes scheduling cases, use raw affinity under `scheduling.affinity`:

```yaml
scheduling:
  affinity:
    nodeAffinity:
      preferredDuringSchedulingIgnoredDuringExecution:
        - weight: 10
          preference:
            matchExpressions:
              - key: disk
                operator: In
                values: [ssd]
```

Legacy `arch`, `node_selector` and `tolerations` remain supported. New files should prefer `scheduling`.

### 12. Security Context

Use `security_context` for common pod and container security settings.

App-level `security_context` is rendered to pod spec `securityContext`:

```yaml
security_context:
  runAsNonRoot: true
  fsGroup: 2000
```

Container-level `security_context` is rendered to container `securityContext`:

```yaml
containers:
  - name: api
    image: nginx:stable
    security_context:
      allowPrivilegeEscalation: false
      runAsUser: 1000
      capabilities:
        drop: ["ALL"]
```

Prefer this dedicated field over `raw.securityContext`. If both are used, `raw` remains a compatibility escape hatch and can override the generated field.

### 13. Lifecycle and Termination Grace

Use app-level `termination_grace_period` to render pod spec `terminationGracePeriodSeconds`:

```yaml
termination_grace_period: 45
```

Use container-level `lifecycle.pre_stop` for graceful shutdown hooks:

```yaml
containers:
  - name: api
    image: nginx:stable
    lifecycle:
      pre_stop:
        command: ["/bin/sh", "-c", "sleep 10"]
```

This generates Kubernetes `lifecycle.preStop.exec.command`.

For rare lifecycle handler forms, use explicit raw passthrough:

```yaml
containers:
  - name: api
    lifecycle:
      pre_stop:
        raw:
          httpGet:
            path: /shutdown
            port: 8080
```

### 14. Service Account and Env From

Use app-level `service_account` to render pod spec `serviceAccountName`:

```yaml
service_account: tsm-api
```

Use container-level `env_from` for common ConfigMap/Secret environment imports:

```yaml
containers:
  - name: api
    image: api:latest
    env_from:
      - config_map: api-config
      - secret: api-secret
        prefix: SECRET_
```

For less common Kubernetes options, use the native reference shape:

```yaml
env_from:
  - configMapRef:
      name: api-config
      optional: true
  - secretRef:
      name: api-secret
      optional: true
```

This renders to Kubernetes `envFrom`.

### 15. Workload Identity and Downward API

Use `workload_identity` when an app needs a projected Kubernetes/OpenShift ServiceAccount token for service-to-service authentication:

```yaml
workload_identity:
  service_account:
    create: true
    name: order-api      # default: app name
    automount: false     # default: false when tokens are configured

  tokens:
    - name: simple-config
      audience: simple-config-server
      mount_path: /var/run/secrets/workload-identity/simple-config # default
      path: token                                                  # default
      expiration_seconds: 3600                                     # default
```

The generated deployment contains `serviceAccountName`, `automountServiceAccountToken: false`, a projected `serviceAccountToken` volume, and a read-only mount. When `service_account.create=true`, `kube-build-app` also generates a `ServiceAccount` manifest.

Reference a declared token from a primary container, sidecar or explicit init
container without repeating its effective file path:

```yaml
sidecars:
  - name: simple-idm-token-proxy
    image: registry.example.test/simple-idm-token-proxy:1.0.0
    envs:
      - name: SIMPLE_IDM_TOKEN_PROXY_TOKEN_FILE
        workload_identity_token_ref_name: simple-config
```

The example renders
`SIMPLE_IDM_TOKEN_PROXY_TOKEN_FILE=/var/run/secrets/workload-identity/simple-config/token`.
Custom `tokens[].mount_path` and `tokens[].path` values are respected.
`workload_identity_token_ref_name` is a reference to `tokens[].name`; an
unknown reference or a combination with another environment value source is a
validation error. The `_ref_name` suffix intentionally makes symbolic
metamodel references visible. The same reference can be inherited through
`apps/_defaults.yml` and `container_envs`.

Use `pod_info` for the common Downward API metadata mount:

```yaml
pod_info:
  enabled: true
  mount_path: /etc/podinfo
```

This creates files such as:

```text
/etc/podinfo/namespace
/etc/podinfo/pod_name
```

For custom Downward API mounts, use the explicit form:

```yaml
downward_api:
  mounts:
    - name: runtime-info
      mount_path: /etc/runtime-info
      items:
        - path: namespace
          field_path: metadata.namespace
        - path: pod_name
          field_path: metadata.name
```

`pod_info` and `downward_api.mounts` expose metadata as files. To expose pod
metadata as environment variables, use `envs[].field_path`. A reusable
opt-in profile keeps the mapping in one place. Define it in
`apps/_defaults.yml`:

```yaml
container_profiles:
  - name: kubernetes-runtime-metadata
    defaults:
      envs:
        - name: POD_NAME
          field_path: metadata.name
        - name: POD_NAMESPACE
          field_path: metadata.namespace
        - name: POD_IP
          field_path: status.podIP
        - name: NODE_NAME
          field_path: spec.nodeName
```

Enable the profile explicitly on each container that needs the variables:

```yaml
containers:
  - name: "{{var:APP_NAME}}"
    image: "..."
    profile_ref_names:
      - kubernetes-runtime-metadata
```

The generated container has `POD_NAME`, `POD_NAMESPACE`, `POD_IP` and
`NODE_NAME` populated through Kubernetes `valueFrom.fieldRef`. These variables
only expose the metadata to the process; application or collection-layer
configuration is still responsible for adding it to metrics or structured
logs.

Pod name is runtime/audit metadata only. Authorization should use the normalized workload identity `namespace/serviceAccount` from the projected token.

### 16. Runtime Assets

Use `runtime_asset_definitions` to define reusable authenticated binary or text
file groups. Apps and containers opt in with `runtime_asset_ref_names`. The
generated init container reads a projected token declared under
`workload_identity`, downloads the files, and stores them in a shared `emptyDir`
volume before application containers start.

Define the reusable asset in `apps/_defaults.yml`:

```yaml
workload_identity:
  service_account:
    create: true
    automount: false
  tokens:
    - name: simple-config
      audience: simple-config-server

runtime_asset_defaults:
  source:
    type: simple_config
    base_url: https://config.example.test/simple-config-server
    tenant: default
    environment: test
    label: release-2026.07 # optional Git label
    workload_identity_token_ref_name: simple-config
    ca_shared_asset_ref_name: internal-ca
    timeout_seconds: 30
  volume:
    name: runtime-config
    mount_path: /app/runtime-config
    medium: Memory
    size_limit: 16Mi
  fetcher:
    image: registry.example.test/simple-idm-token-proxy:1.0.0
    # command defaults to simple-idm-token-proxy
    # image_pull_policy defaults to Always

runtime_asset_definitions:
  - name: java-runtime-config
    files:
      - source: files/ssl/tsm-client-keystore.jks
        target: tsm-client-keystore.jks
        mode: "0440"
        sha256: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

      - source: files/ssl/tsm-client-truststore.jks
        target: tsm-client-truststore.jks
        mode: "0440"

  - name: connector-runtime-config
    files:
      - source: files/infrastructure/mtls-connector-worker.yaml
        target: mtls-connector-worker.yaml
        mode: "0440"
```

`runtime_asset_defaults` supplies the complete `source`, `volume`, and
`fetcher` blocks when a definition omits them. A definition may replace any
one of these blocks by declaring that complete block itself. Fields inside a
block are not merged, which keeps the resulting configuration unambiguous.

Select it from an app for all primary `containers[]`:

```yaml
runtime_asset_ref_names:
  - java-runtime-config
  - connector-runtime-config
```

Or select it only for one runtime container:

```yaml
containers:
  - name: api
    runtime_asset_ref_names:
      - java-runtime-config
```

`runtime_asset_ref_names` on sidecars is also supported for the rare case where
a sidecar needs the runtime volume. App-level refs never apply to sidecars.

```yaml
sidecars:
  - name: mtls-gateway
    runtime_asset_ref_names:
      - connector-runtime-config
```

Selected definitions may share a volume when `name`, `mount_path`, `medium`,
and `size_limit` are identical. The Pod receives one volume and each container
receives at most one matching mount. Fetch init containers remain separate and
run sequentially. Two definitions may not write the same `target` into a
shared volume. A differently configured volume with the same name is also a
validation error.

`source.workload_identity_token_ref_name` references
`workload_identity.tokens[].name`; it does not define another token or
audience. An unknown token reference is a validation error.

Give reusable entries in `<environment>/shared.assets.yml` a stable `name`.
`source.ca_shared_asset_ref_name` resolves that name to the asset's canonical
`to` path. The generated fetch init container mounts the matching asset and
passes the resolved path through `--ca-file`. The fetcher adds the PEM bundle
to its normal trust roots; TLS verification remains enabled.

```yaml
# <environment>/shared.assets.yml
assets:
  - name: internal-ca
    file: assets/ssl/internal-ca.pem
    to: /var/run/certs/internal-ca.pem
```

The same path can be exported to a container without duplicating it:

```yaml
envs:
  - name: SSL_CERT_FILE
    shared_asset_ref_name: internal-ca
```

`source.ca_file` remains an explicit escape hatch for an unnamed shared asset.
It must be absolute and exactly match a shared asset `to` path.
`source.ca_file` and `source.ca_shared_asset_ref_name` are mutually exclusive,
and neither can be combined with `source.insecure_upstream_tls`.

Defaults:

- `source.type`: `simple_config`
- `source.tenant`: `default`
- `source.environment`: the environment passed through `-e`
- `source.timeout_seconds`: `30`
- `volume.name`: runtime asset group name
- `files[].mode`: `"0440"`
- fetcher resources: CPU `10m..100m`, memory `16Mi..128Mi`

Unknown runtime asset, token or shared asset references are validation errors.
Application mounts are read-only; only the generated fetch init container
receives a writable mount.

The fetcher runs:

```text
simple-idm-token-proxy fetch
```

It sends the projected token directly to `simple-config-server`. It does not
use a localhost proxy because ordinary sidecars start only after init
containers have completed. A long-running application client may independently
use `simple-idm-token-proxy serve` as a regular sidecar.

Runtime assets are fetched once during Pod startup; they are not continuously
synchronized. A remote file change therefore requires a Pod restart or
rollout. Use `source.label` when deployment must be pinned to a reproducible Git
revision.

### 17. Sidecars and Pod Options

Use `sidecar_definitions` in `apps/_defaults.yml` for reusable helper
containers. Apps opt in explicitly with `sidecar_ref_names`:

```yaml
# apps/_defaults.yml
workload_identity:
  service_account:
    create: true
  tokens:
    - name: simple-config
      audience: simple-config-server

sidecar_definitions:
  - name: simple-idm-token-proxy
    image: "{{TSM_REGISTRY_URL}}/simple-idm-token-proxy:{{TSM_RELEASE_ID}}"
    startup:
      command:
        - simple-idm-token-proxy
      arguments:
        - serve
        - --listen
        - 127.0.0.1:9999
        - --upstream
        - https://config.example.test/simple-config-server
        - --token-file
        - /var/run/secrets/workload-identity/simple-config/token
    resources:
      cpu:
        from: "10m"
        to: "100m"
      memory:
        from: "32Mi"
        to: "128Mi"

# apps/api.yml
sidecar_ref_names:
  - simple-idm-token-proxy

containers:
  - name: api
    image: "{{TSM_REGISTRY_URL}}/api:{{TSM_RELEASE_ID}}"
    envs:
      - name: SPRING_CLOUD_CONFIG_URI
        value: http://127.0.0.1:9999
```

Sidecars are rendered as regular Kubernetes containers in the same Pod. They
share Pod networking automatically, so `127.0.0.1` works between the app
container and the sidecar. Workload identity token mounts and Downward API
mounts are mounted into sidecars as well.

An app can patch a selected reusable sidecar by defining a local `sidecars` item
with the same `name`. The sidecar must still be listed in `sidecar_ref_names`;
otherwise the patch is rejected with a migration error. Matching `envs` are
merged by `name`, so app-specific overrides do not require duplicating the whole
sidecar definition. A local `sidecars` item without a matching definition is
appended as an app-only sidecar.

For helper containers that need to see processes from other containers in the same Pod, enable shared process namespace:

```yaml
pod:
  share_process_namespace: true

sidecar_ref_names:
  - cgroup-runtime-exporter

sidecars:
  - name: cgroup-runtime-exporter
    envs:
      - name: CGROUP_EXPORTER_TARGET_PID_REGEXP
        value: '(^|/)java(\s|$)'
```

This renders Kubernetes `shareProcessNamespace: true`. Sidecar ports are not used for service generation; services are generated only from primary `containers`.

### 18. Init Containers

Use app-level `init_containers` for generic Kubernetes init containers:

```yaml
init_containers:
  - name: migrate
    image: registry.example.com/api-migrate:latest
    command: ["/bin/sh", "-c"]
    arguments: ["./migrate.sh"]
    envs:
      - name: LOG_LEVEL
        value: INFO
    env_from:
      - config_map: api-config
    mounts:
      - type: empty_dir
        name: work
        mount_path: /work
    security_context:
      runAsNonRoot: true
    resources:
      cpu:
        from: "50m"
        to: "100m"
      memory:
        from: "64Mi"
        to: "128Mi"
```

Supported init container fields intentionally mirror the common subset of regular containers:

- `name`
- `image`
- `command`
- `arguments`
- `vars`
- `env_from`
- `mounts`
- `security_context`
- `resources`
- `raw`

Existing `tools` remain the preferred shortcut for exposing static utility binaries through generated init containers.

### 19. Raw Escape Hatches

Use `deployment_raw` for rare Deployment-level fields:

```yaml
deployment_raw:
  spec:
    revisionHistoryLimit: 2
```

Use `pod_raw` for rare pod spec fields:

```yaml
pod_raw:
  dnsPolicy: ClusterFirst
  enableServiceLinks: false
```

`deployment_raw` is recursively merged into the generated Deployment object. `pod_raw` is applied to `spec.template.spec`.

### 20. Raw Container Fields

Use raw passthrough only when the model does not expose a dedicated field:

```yaml
containers:
  - name: api
    image: nginx:stable
    raw:
      securityContext:
        runAsNonRoot: true
```

Dedicated model fields are preferred because they can be validated and represented in UI tooling.

### 21. Cgroup Exporter Defaults

At container level you can enable automatic variable injection for cgroup exporter:

```yaml
containers:
  - name: api
    image: api:latest
    enable_cgroup_exporter: true
```

Only missing variables are injected:

```text
CGROUP_EXPORTER_METRICS_PREFIX
CGROUP_EXPORTER_METRICS_STATIC_LABELS
CGROUP_EXPORTER_LISTEN
CGROUP_EXPORTER_CPU_REQUESTS_MCPU
CGROUP_EXPORTER_CPU_LIMITS_MCPU
CGROUP_EXPORTER_MEMORY_REQUESTS_MIB
CGROUP_EXPORTER_MEMORY_LIMITS_MIB
CGROUP_EXPORTER_NODE_NAME
```

### 22. Ignored Apps

Exclude an app from build:

```yaml
ignore: true
name: experimental-api
```

No deployment, service or assets are generated for ignored apps.

## App Defaults

`apps/_defaults.yml` is merged into every app model in that environment.

Example:

```yaml
tools:
  - name: util-apply-env
    image: registry.example.com/tools/apply-env:latest
    expose_bin: /usr/bin/apply-env
    mount_path: /usr/local/bin/apply-env

vars:
  - name: LOG_LEVEL
    value: INFO

container_envs:
  - container_ref_name: "*"
    envs:
      - name: GLOBAL_FLAG
        value: "true"

  - container_ref_name: api
    envs:
      - name: JAVA_OPTS
        value: "-Xms256m"

container_profiles:
  - name: java-jib-service
    defaults:
      image: "<from release manifest>"
      startup:
        command: ["/bin/sh"]
        arguments:
          - /app/start-java.sh
          - /app/jib-classpath-file
          - /app/jib-main-class-file
      envs:
        - name: SPRING_CONFIG_IMPORT
          value: "configserver:http://127.0.0.1:9999"
      probes:
        http:
          path: /actuator/health
          port: "{{env:DEFAULT_EXPOSE_PORT}}"

sidecar_definitions:
  - name: cgroup-runtime-exporter
    image: "{{env:DOCKER_HUB_URL}}/datalite/cgroup-runtime-exporter:2026.07.28.3"
    envs:
      - name: CGROUP_EXPORTER_TARGET_PID_REGEXP
        value: '(^|/)java(\s|$)'
```

Semantics:

- generic map keys are recursively merged, app values win
- `vars` are matched by `name`; app-level item fully replaces default item
- `container_profiles` define reusable container defaults by `name`
- `containers[].profile_ref_names` selects one or more profiles for that container
- `sidecar_definitions` define reusable sidecars by `name`
- `sidecar_ref_names` selects one or more sidecar definitions for an app
- local `sidecars` can patch a selected sidecar by `name` or append an app-only sidecar
- profile defaults are merged in listed order, then local container values win
- `container_profiles[].defaults.name` is not allowed; set container names in app files
- `container_profiles[].defaults.envs` and local `containers[].envs` are matched by `name`
- `container_envs` are applied by `container_ref_name`
- `container_ref_name: "*"` applies to all containers first
- concrete container defaults are applied next
- local `containers[].envs` are applied last
- matching variables are fully replaced by `name`

Removal / tombstone:

```yaml
vars:
  - name: LOG_LEVEL
    remove: true
```

```yaml
containers:
  - name: api
    envs:
      - name: GLOBAL_FLAG
        remove: true
```

`remove: true` removes the item completely and must not be combined with other data fields.

## Replica Profiles

Use environment-level profiles to override replicas without editing app files:

`<environment>/replica-profiles.yml`:

```yaml
defaults:
  replica_profile_ref_name: normal

profiles:
  normal:
    apps:
      api: 2
      worker: 1

  maintenance:
    all: 0
    apps:
      api: 1
```

Run:

```bash
kube-build-app build -e test -p maintenance -R environments -t deploy/test
```

Scale selected apps down directly:

```bash
kube-build-app build -e test -w worker -R environments -t deploy/test
```

## Rollout Checksums

Use rollout checksum annotations when a pod must restart after selected files change.

```yaml
rollout_on:
  checksums:
    config:
      files:
        - env.unsecured.json
        - env.secured.json
    mtls:
      files:
        - assets/infrastructure/mtls-gateway-config.tpl
```

Generated deployment pod template contains:

```yaml
spec:
  template:
    metadata:
      annotations:
        checksum/config: "..."
        checksum/mtls: "..."
```

Behavior:

- paths are relative to `<environment_dir>`
- files are sorted before hashing
- checksum includes relative path and file content
- missing files fail fast
- changing annotation changes pod template and triggers rollout

Use this for deterministic declarative files only. Do not use it for values injected later by sidecars or runtime-only mechanisms.

## Inventory

Inventory prints a structured JSON view of what will be built:

```bash
kube-build-app inventory -e test -R environments
```

Legacy equivalent:

```bash
kube-build-app -i -e test -R environments
```

It is intended for automation, UI tooling and generators such as PKI helpers.

## Validation

Validate model files without writing manifests:

```bash
kube-build-app validate -e test -R environments
```

Validation catches known invalid combinations, for example conflicting startup/simple-init settings and invalid tombstones.

## Helm-Safe Assets

If generated manifests are rendered again by Helm, remaining plain placeholders in text assets can collide with Helm syntax.

Use:

```bash
kube-build-app build -e test -R environments -t deploy/test --helm-escape-assets
```

Remaining placeholders are wrapped from:

```text
{{VAR}}
```

into:

```text
{{`{{VAR}}`}}
```

If `transform: true` is set on the asset, build-time variable transform is applied first, then remaining placeholders are Helm-escaped.

## mTLS Assets

When container-level `mtls.enabled: true` is used, `kube-build-app` automatically mounts encrypted mTLS files into the container.

Inventory exposes the expected generated paths so external tooling can produce the corresponding secured material.

## Docker

Build image:

```bash
docker build -t kube-build-app:latest .
```

Run:

```bash
docker run --rm \
  -v "$PWD/environments:/work/environments:ro" \
  -v "$PWD/deploy:/work/deploy" \
  kube-build-app:latest \
  kube-build-app build -e test -R /work/environments -t /work/deploy/test
```

## Development

Run tests:

```bash
just test
```

Build local binaries:

```bash
just build
```

Build cross-platform binaries:

```bash
just build-cross
just build-cross-all
```

Generated manifests use two-space YAML indentation by default, matching the Ruby implementation. Use `--yaml-indent 4` only where a repository convention requires four spaces:

```bash
kube-build-app build -e test -R environments -t deploy/test --yaml-indent 4
```

`just build-cross` injects the `VERSION`, Git commit and UTC build timestamp into every `kube-build-app` binary. Verify an artifact with `kube-build-app --version`.

The version output is a single text line: `kube-build-app version <version> (commit <commit>, built <UTC timestamp>)`. Running from source without build metadata reports `dev` and `unknown`.

Build release binaries with version metadata and SHA256 checksums:

```bash
just release 0.1.0
```

Run README smoke fixture:

```bash
just readme-smoke
```

The fixture lives in `fixtures/readme-smoke/environments/dev` and verifies the documented CLI flow against a small environment repository:

```bash
kube-build-app validate -e dev -R fixtures/readme-smoke/environments
kube-build-app list -e dev -R fixtures/readme-smoke/environments
kube-build-app summary -e dev -R fixtures/readme-smoke/environments
kube-build-app inventory -e dev -R fixtures/readme-smoke/environments
kube-build-app build -e dev -R fixtures/readme-smoke/environments -t /tmp/kube-build-app-readme-smoke
```

Run parity against Ruby reference implementation:

```bash
scripts/parity-build \
  --name cetin-test \
  --root /path/to/tsm-environments \
  --env test \
  --release-id 2025.08.18.1 \
  --go-bin ./dist/kube-build-app
```

Run representative smoke parity cases:

```bash
just parity-smoke
```

## GitLab Release Pipeline

The GitLab pipeline uses `VERSION` as the release trigger.

On default branch, changing `VERSION` runs:

- release notes generation from git history
- cross-platform `kube-build-app` builds
- `tar.gz` package creation
- `SHA256SUMS` generation
- GitLab Release creation/update
- upload to GitLab Generic Package Registry

Release package names:

```text
kube-build-app-<version>-darwin-arm64.tar.gz
kube-build-app-<version>-darwin-amd64.tar.gz
kube-build-app-<version>-linux-amd64.tar.gz
kube-build-app-<version>-linux-arm64.tar.gz
kube-build-app-<version>-windows-amd64.tar.gz
SHA256SUMS
```

Each platform package contains both binaries:

```text
kube-build-app
kube-edit-app
```

Optional `CHANGELOG.md` push:

```text
RELEASE_PUSH_TOKEN
```

When `RELEASE_PUSH_TOKEN` is set in GitLab CI/CD variables, the publish job updates `CHANGELOG.md` and pushes it back with `[skip ci]`.

## GitHub Release Workflow

The GitHub Actions workflow mirrors the release build when `VERSION` changes on
the `main` branch. A push to the `github` remote then:

- runs the complete Go test suite
- generates release notes from git history
- builds the same cross-platform packages as GitLab
- creates or updates the `v<VERSION>` GitHub Release
- uploads all `tar.gz` packages and `SHA256SUMS`

The workflow uses the repository-provided `GITHUB_TOKEN`; no additional release
token is required. Commits that do not change `VERSION` do not start this
workflow.

## License

This project is licensed under the MIT License.

See [LICENSE](LICENSE) for details.

## Support

This repository is provided as-is and is not maintained as a community-supported project. GitHub Issues and free community support are not provided.

Commercial support, including technical assistance, verified releases, bug fixes, updates and long-term maintenance, is available from [DataLite, spol. s r.o.](https://datalite.cz/).

Acceptance of pull requests is entirely at the discretion of the project maintainer. The maintainer is not obligated to review, accept or respond to a pull request.

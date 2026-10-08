# terraform-provider-webbpulse

Terraform provider for the WebbPulse Terraform control plane. It manages
projects, workspaces with their variables and run notifications, private
registry modules and providers, and reads a workspace's run role check. Per
resource reference lives in `docs/`, generated with `tfplugindocs generate` from the
schema and `examples/`.

Built on the HashiCorp Terraform Plugin Framework v1.19.0, protocol 6.

## Install

Prereleases publish to the staging registry. Log in once with
`terraform login staging.terraform.webbpulse.com` (or set
`TF_TOKEN_staging_terraform_webbpulse_com` to a `wpk_` key with
`registry:read`), then:

```hcl
terraform {
  required_providers {
    webbpulse = {
      source  = "staging.terraform.webbpulse.com/WebbPulse/webbpulse"
      version = "0.2.0-rc.5"
    }
  }
}
```

For a local build, use `go build -o "$(go env GOPATH)/bin/terraform-provider-webbpulse"`
and a `dev_overrides` block for `WebbPulse/webbpulse` in `~/.terraformrc`.

## Configure

```hcl
provider "webbpulse" {
  host  = "https://api.staging.terraform.webbpulse.com"
  token = var.webbpulse_token
}
```

| Attribute | Environment variable | Notes |
| --- | --- | --- |
| `host` | `WEBBPULSE_TF_HOST` | Base URL. The `/api/v1` suffix is added when absent. Required, no default. |
| `token` | `WEBBPULSE_TF_TOKEN` | A bearer token: an agent API key (`wpk_` prefix) or a user JWT. Marked sensitive. |
| `origin_verify` | `WEBBPULSE_TF_ORIGIN_VERIFY` | The edge access gate value, sent as `x-origin-verify` on every API request. Optional and marked sensitive; unset sends no header. |
| `origin_verify_ssm_parameter` | `WEBBPULSE_TF_ORIGIN_VERIFY_SSM_PARAMETER` | Name or ARN of the SSM parameter holding the gate value. Read with decryption using the ambient AWS credentials when no direct `origin_verify` value is set. |

Set the token through the environment rather than in a configuration file, so
it stays out of version control and out of the state file.

Staging and production sit behind an edge access gate: API Gateway answers 403
to any request without the `x-origin-verify` header or a gate cookie, so a
provider driven by a `wpk_` key needs `origin_verify`. The value lives in the
environment's SSM parameter held by the gate module. Either name that
parameter in `origin_verify_ssm_parameter` (or
`WEBBPULSE_TF_ORIGIN_VERIFY_SSM_PARAMETER`) and let the provider read it, or
resolve it in the calling pipeline and pass it through `origin_verify` or
`WEBBPULSE_TF_ORIGIN_VERIFY`.

The provider reads the parameter once per configure with `ssm:GetParameter`
and decryption, using the ambient AWS credential chain and region (environment,
shared profile, container or instance role); a parameter named by its full ARN
is read in the ARN's region. The credentials also need `kms:Decrypt` on the
parameter's key. A direct value, from the attribute or its environment
variable, wins and the parameter is then never read. A failed read is an error
naming the parameter, never a silent fallback to no header. The value is held
in memory only: it is not written to state, plan output or logs.

## Resources

### `webbpulse_workspace`

Creates, reads, updates and deletes one workspace. Imports by workspace id.

The name is not editable, so changing it replaces the workspace. `run_role_arn`
is optional on create on purpose: the role's trust policy names the workspace
id as its external id, so the role cannot exist until the workspace does. Create
the workspace, build the role from the computed `run_role_setup`, then set
`run_role_arn` in a second apply.

Updates are a JSON Merge Patch that carries only the changed attributes.
Removing `run_role_arn`, `working_directory` or `description` from the
configuration sends an explicit JSON null, which clears the field on the API;
clearing `run_role_arn` also drops the recorded check outcome.

Deleting a workspace is refused with a 409 carrying
`WORKSPACE_MANAGES_RESOURCES` while its state tracks any resource (a state the
API cannot parse counts as tracking some), so a destroy never strands running
infrastructure. Destroy those resources first with a destroy run on the
workspace, or set `force_delete = true` to delete it anyway and leave them
unmanaged. As with the tfe provider's `force_delete`, the flag is read from
state, so apply it before the destroy. It sends `?force=true` and skips only
that check: a workspace with an unfinished run is refused with
`WORKSPACE_HAS_ACTIVE_RUN` either way. Both refusals surface as diagnostics
naming the code.

Optional: `force_delete` (default `false`), `trigger_patterns` (default `[]`),
`file_triggers_enabled` (default `true`), `speculative_enabled` (default
`true`), `plan_assume_role_arns` (default `[]`), `project_id` (default
`prj-default`), and the `vcs_repo` block.

`project_id` places the workspace in a project. Unset, or `prj-default`, means
the default project, so existing workspaces plan no change. Changing it moves
the workspace in place, and a project that does not exist is refused with a 422
carrying `PROJECT_NOT_FOUND`, surfaced on `project_id`. A create in the default
project leaves `project_id` out of the request.

### `webbpulse_project`

Creates, reads, updates and deletes one project, like `tfe_project`. Imports by
project id. `name` (up to 40 letters, digits, single spaces, hyphens and
underscores) renames in place and is unique ignoring case, so a taken name is a
409 carrying `PROJECT_NAME_TAKEN`; `Default Project` is reserved. `description`
(up to 256 characters) defaults to empty, and removing it clears it.
`workspace_count` is computed.

A project is deleted only once it is empty: a destroy while it still holds
workspaces is a 409 carrying `PROJECT_NOT_EMPTY`, and nothing is moved for you.
The default project, `prj-default`, is built in, so importing it is refused
and an edit or a delete of it is a 409 carrying `DEFAULT_PROJECT_READ_ONLY`.
Read it with the data source instead.

```sh
terraform import webbpulse_project.platform prj-01JABCDEF0123456789ABCDEFG
```

`plan_assume_role_arns` is a set of exact IAM role ARNs a plan session may
assume beside its read only access, such as a Route 53 reader role in another
account; an apply is not limited by it. The validators mirror the API: at most
10 ARNs, each up to 160 characters and of the form
`arn:aws:iam::<12 digit account id>:role/<name>`, with no wildcards. It updates
in place and the PATCH replaces the whole list. Removing it or setting `[]`
sends an explicit null, which clears it. The data source returns it too.

Computed: `workspace_id`, `created_at`, `updated_at`, `run_role_setup`
(`principal_arn`, `principal_arns`, `external_id`, `role_name`),
`run_role_checked_at`, `run_role_account_id`.

```sh
terraform import webbpulse_workspace.example ws-01JABCDEF0123456789ABCDEF
```

#### VCS connection

The `vcs_repo` block connects the workspace to a GitHub repository through the
environment's GitHub App, following the shape of `tfe_workspace`:

```hcl
resource "webbpulse_workspace" "example" {
  name                  = "example"
  engine_version        = "1.9.8"
  working_directory     = "infra"
  trigger_patterns      = ["/modules/**/*.tf"]
  file_triggers_enabled = true
  speculative_enabled   = true

  vcs_repo {
    identifier = "WebbPulse/example-infra"
    branch     = "main"
  }
}
```

- `identifier` is the repository as `owner/name` and is required inside the
  block. The API records GitHub's canonical spelling; a configured spelling
  that differs only in case is kept, so it causes no diff.
- `branch` is optional and computed. Left out, the API fills in the
  repository's default branch when the repository is connected. It stays
  unchanged while `identifier` names the same repository, so removing a
  configured `branch` keeps the current branch rather than resetting it. Set it
  explicitly to change it.
- `repository_id` and `installation_id` are computed: GitHub's repository id,
  which keeps the connection through a rename, and the App installation that
  covered it. Both are null when the environment has no GitHub App, in which
  case the first upload records the id.
- Removing the block disconnects the repository: the update sends an explicit
  null for both `vcs_repo` and `tracked_branch`.

`working_directory`, `trigger_patterns`, `file_triggers_enabled` and
`speculative_enabled` are top level, as on `tfe_workspace`. An upload starts a
run only when a changed path is under `working_directory` or matches a
`trigger_patterns` glob; `file_triggers_enabled = false` starts a run on every
push to the tracked branch. `speculative_enabled` maps to the API's
`speculative_plans` and controls whether a pull request starts a plan only run.

Connecting a repository the App is not installed on, or whose installation does
not grant it, is a 422 carrying `VCS_REPO_NOT_INSTALLED`, which surfaces as an
error on `vcs_repo.identifier` telling you to install the App on the
repository. A `GITHUB_UNAVAILABLE` error means the API could not reach GitHub
and the apply can be retried.

### `webbpulse_variable`

Creates, reads, updates and deletes one variable. Imports by
`<workspace_id>/<key>`.

`category` is `terraform` for a `-var` on the command line or `env` for a
process environment variable on the task. `value` is marked sensitive at the
schema level whatever `sensitive` is set to.

`hcl` (default `false`) sends `value` as an HCL expression, which is how a list
or map input is set, for example `value = jsonencode(["a", "b"])` with
`hcl = true`. Only a `terraform` variable can be HCL; the API refuses an `env`
one and an expression that cannot parse, with a 422. `hcl` round-trips on read
and import, and changing it updates the variable in place, since the PUT route
rewrites it.

The API never returns a sensitive value, on any route. The provider therefore
keeps the configured value in state and does not overwrite it with the null the
API returns, which is what stops a permanent diff. Two consequences follow: a
sensitive value changed outside Terraform cannot be detected, and a sensitive
variable cannot be imported, so the import is refused with an explanation
rather than silently producing an empty value.

Terraform still stores the configured value in state. The sensitive marker hides
normal CLI output; it does not encrypt state. Protect the state backend accordingly.

```sh
terraform import webbpulse_variable.example ws-01JABCDEF0123456789ABCDEF/region
```

### `webbpulse_notification_configuration`

Creates, reads, updates and deletes one run notification configuration, like
`tfe_notification_configuration`. Imports by `<workspace_id>/<notification_id>`.

`destination_type` is `slack` (an incoming webhook on `hooks.slack.com`),
`discord` (a channel webhook on `discord.com`) or `generic` (any HTTPS URL,
which receives HCP Terraform's version 1 payload). `triggers` is a set of
`run:created`, `run:planning`, `run:needs_attention`, `run:applying`,
`run:completed` and `run:errored` (default `[]`), and `enabled` defaults to
`true`. Only a `generic` destination takes `token`, which signs each body into
`X-TFE-Notification-Signature`; a token on another destination fails validation.

`url` and `token` are sensitive and write only. The API never returns them, so
the provider keeps the configured values in state, as with a sensitive
variable, and a URL or token changed outside Terraform is not detected. The
computed `url_masked` shows the scheme and host and `has_token` whether a token
is set. Updates send only the changed attributes; a change of
`destination_type` resends `url`, which the API requires, and removing `token`
sends an explicit null, which clears it. An imported configuration has no `url`
or `token` in state, so the first apply after an import writes the configured
values.

Writes need `workspaces:write` and a recent login, which a `wpk_` key passes. A
workspace holds at most 50 configurations (a 409), and a refused field is a 422
whose message never quotes the URL.

```sh
terraform import webbpulse_notification_configuration.slack ws-01JABCDEF0123456789ABCDEF/nc-01JABCDEF0123456789ABCDEFG
```

### `webbpulse_registry_module`

Connects a GitHub repository the environment's GitHub App sees as a private
module, like `tfe_registry_module` with `vcs_repo`. Each `vX.Y.Z` or `X.Y.Z` tag
publishes that version through the push webhook. `name` and `module_provider`
default from a `terraform-<provider>-<name>` repository name and are required
otherwise; changing either, or `vcs_repo.identifier`, replaces the module.
`import_tags` (default `true`) is read on create only. There is no update route,
so the only in place change is `resync_triggers`: changing its values queues a
resync of every tag. Delete removes the module and every version. Imports by
`namespace/name/provider`.

### `webbpulse_registry_provider`

The same for a private provider. The repository must be named
`terraform-provider-<type>`, and each published GitHub release carrying signed
GoReleaser artifacts publishes that version. `import_releases` defaults to
`true`. Imports by `namespace/type`.

Connecting and deleting are step-up gated for a user session, which surfaces as
`STEP_UP_REQUIRED`; a `wpk_` key is exempt, so run these with a key.

## Data sources

- `webbpulse_workspace` reads one workspace by `workspace_id` or by `name`.
  Exactly one of the two is set. The API has no name lookup route, so a lookup
  by name lists every workspace and filters in the provider. It returns the
  same attributes as the resource, with `vcs_repo` as a computed object that is
  null when no repository is connected.
- `webbpulse_project` reads one project by `name`, ignoring case, and returns
  its `id`, `description`, `is_default`, `workspace_count` and timestamps. The
  default project answers to `Default Project`, with `id` `prj-default` and null
  timestamps. Like the workspace lookup, it lists every project and filters in
  the provider.
- `webbpulse_workspaces` returns every workspace's `ids` and `names`. The API
  takes no filters on its listing route.
- `webbpulse_registry_module` and `webbpulse_registry_provider` read one
  registry entry by address and return its repository, `published_versions`
  and every version with its status, error, tag or release and commit sha;
  provider versions also carry protocols, the signing key id and platforms.
- `webbpulse_run_role_check` reports whether the runner has assumed a
  workspace's run role and returns `status`, `connected`, `account_id`,
  `error`, `run_id` and `checked_at`.

## The run role check

The `webbpulse_run_role_check` data source calls
`GET /workspaces/{id}/run-role/check`, which records nothing, so a plan or
refresh never changes the workspace. It needs the `workspaces:read` scope. The
`POST` form of the same route, which stamps the outcome on the workspace for the
web UI, is deliberately not used.

The API never calls STS. The answer comes from the runner's own record: the
outcome of the AssumeRole in the newest run made with the current role ARN.
`status` is one of:

- `connected`: that run assumed the role. `account_id`, `run_id` and
  `checked_at` name the account and the run that proved it.
- `failed`: that run was refused. `error` carries the reason and `run_id` the
  run.
- `unverified`: no plan has run on this role yet, so there is nothing to report
  and `run_id` and `checked_at` are null. A plan-only run is the check.

By default every status is data, not an error, because the trust policy may
simply not be in place yet. Set `fail_if_not_connected = true` to fail the read
on anything other than `connected`, including `unverified`. A workspace with no
role at all is a 400 carrying `RUN_ROLE_MISSING`, which surfaces as an error
naming `run_role_arn`.

## Errors

The client keeps the API's own error envelope. A 404 on a resource read removes
the resource from state, so a workspace, variable or notification configuration deleted outside
Terraform is
planned for re-creation; every other status becomes a diagnostic carrying the API's
`message`, its `error_code` and the request id, so a failure can be traced back
to one request in the control plane's logs.
Unstructured response bodies and validation detail payloads are not copied into
diagnostics because they can contain submitted values.

## Development

```sh
go build ./...
go vet ./...
gofmt -l .
go test ./...
```

Acceptance tests are guarded by `TF_ACC` and skip unless `WEBBPULSE_TF_HOST`
and `WEBBPULSE_TF_TOKEN` are both set. The provider under test reads the gate
value from `WEBBPULSE_TF_ORIGIN_VERIFY` like any configuration. `WEBBPULSE_TF_ACC_VCS_REPO` (an `owner/name` the
App is installed on) enables the workspace VCS and registry module tests, and
`WEBBPULSE_TF_ACC_REGISTRY_PROVIDER` (`namespace/type` of an existing provider)
enables the registry provider test, which reads and imports without persisting.

The `acceptance` workflow (manual dispatch, `staging-acceptance` environment)
runs them against staging: it assumes the
`webbpulse-terraform-staging-provider-acceptance` role, reads the gate value
from SSM into `WEBBPULSE_TF_ORIGIN_VERIFY`,
mints a KMS signed admin token, creates an ephemeral `e2e-` user and a `wpk_`
key, runs `go test -run TestAcc`, then revokes the key and deletes the user.
The role lives in WebbPulse-Terraform `terraform/provider_acceptance.tf`.

The `*Lifecycle` tests drive the same flows through real Terraform plans
against an in-memory fake API, so they run in `go test ./...` with no
credentials.

## Releases

A pushed `vX.Y.Z` tag runs `release.yml`, which builds with GoReleaser and
publishes a GitHub release in the Terraform registry layout: one
`terraform-provider-webbpulse_<version>_<os>_<arch>.zip` per platform,
`_SHA256SUMS`, its detached signature `_SHA256SUMS.sig` and `_manifest.json`
(protocol 6.0). A prerelease tag (`v0.1.0-rc.1`) signs with the staging key in
the `staging` environment and is marked a prerelease; a plain tag signs with the
production key in the `production` environment. The workflow checks the
signature and the sums before it finishes, and keeps the zips, sums and manifest
as the `provider-build-<tag>` run artifact for 90 days.

Release rules:

- Only repository admins can create, move or delete `v*` tags (the
  `release-tags` tag ruleset).
- The tag must point at a commit on `main`. The workflow checks this before it
  touches the signing key and fails otherwise, so merge first, then tag the
  merge commit.
- GoReleaser is pinned to an exact version in `release.yml`.

To publish a prerelease to the production registry, dispatch `release.yml` from
`main` with the tag and `environment: production`. It does not rebuild: it
downloads the tag run's build artifact, checks that its `SHA256SUMS` is
byte-identical to the GitHub release's, re-signs those sums with the target
key and uploads the same zips. Registry and GitHub release hashes therefore
match. A tag pushed before this rule has no artifact and cannot be promoted;
cut a new tag.

```sh
git tag v0.2.0-rc.7 origin/main && git push origin v0.2.0-rc.7
gh workflow run release.yml --ref main -f tag=v0.2.0-rc.7 -f environment=production
```

Each environment has its own RSA 4096 signing key, generated once in CI by the
`signing-key` workflow (manual dispatch, run in the `<env>-signing-key`
environment, which only `main` may deploy to). It refuses to run when the key
already exists. The private half goes straight into the
`webbpulse-terraform-<env>-provider-signing-key` secret in that environment's
WebbPulse-Terraform account, whose resource policy lets only the
`webbpulse-terraform-<env>-provider-release` role read it and only the
`...-provider-signing-keygen` role write it. Each role trusts only its one
environment of this repository. The public half and the long key id are the
SSM String parameters `/webbpulse-terraform-<env>/provider-signing/public-key`
and `.../key-id`, where the registry reads them. `<env>` is `staging` or `prod`.
The roles, secret and parameters live in WebbPulse-Terraform `terraform/provider_signing.tf`.

Environment variables on this repository: `SIGNING_KEY_ROLE_ARN` and
`SIGNING_KEY_SECRET_ID` on all four environments, plus
`SIGNING_KEY_PARAMETER_PREFIX` on the two `-signing-key` ones.

After a version publishes, refresh the lock file of every root that pins it, with
hashes taken from the registry:

```sh
terraform login terraform.webbpulse.com
terraform providers lock -platform=linux_amd64 -platform=darwin_arm64
```

Use `staging.terraform.webbpulse.com` for a prerelease. `terraform login` stores a
`registry:read` key in `~/.terraform.d/credentials.tfrc.json`. A `wp-tf login`
access token in `TF_TOKEN_terraform_webbpulse_com` also works, but it expires
within the hour. The lock records an `h1:` hash per platform and the `zh:` hashes
from the registry's signed `SHA256SUMS`. Commit `.terraform.lock.hcl` with the
version bump.

To check a release by hand:

```sh
aws ssm get-parameter --name /webbpulse-terraform-staging/provider-signing/public-key \
  --query Parameter.Value --output text | gpg --import
gpg --verify terraform-provider-webbpulse_<version>_SHA256SUMS.sig \
  terraform-provider-webbpulse_<version>_SHA256SUMS
```

## Backlog

- An `origin_verify_ssm_parameter` attribute that reads the gate value from SSM
  through the AWS SDK default chain, if the Platform factory wants the provider
  to resolve it rather than its pipeline.
- No runs resource or data source. `POST /workspaces/{id}/config-versions`
  exists, but the runs domain is not wrapped here yet, so a run cannot be
  queued from Terraform.
- No live create in the registry provider acceptance test: the only
  `terraform-provider-<type>` repository the staging App sees is this one,
  already connected.
- No filters, pagination or name lookup on `GET /workspaces`, so
  `webbpulse_workspaces` returns everything and a lookup by name filters client
  side. That is fine at the current scale and will not stay fine.
- No bulk variable write, so a workspace with many variables makes one request
  per variable.
- No ETag or version on a workspace, so an update cannot be made conditional
  and a concurrent edit is last write wins.

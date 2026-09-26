# terraform-provider-webbpulse

Terraform provider for the WebbPulse Terraform control plane. It manages
workspaces and their variables, and reads a workspace's run role check.

Built on the HashiCorp Terraform Plugin Framework v1.19.0, protocol 6.

## Install

The provider registry is not live yet, so the current path is a local build
plus a `dev_overrides` block. Registry install is pending.

```sh
go build -o "$(go env GOPATH)/bin/terraform-provider-webbpulse"
```

Then in `~/.terraformrc`:

```hcl
provider_installation {
  dev_overrides {
    "WebbPulse/webbpulse" = "/home/you/go/bin"
  }
  direct {}
}
```

With a `dev_overrides` block in place Terraform skips `terraform init` for this
provider and warns that it is overridden, which is expected. `terraform plan`
and `terraform apply` work as usual.

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

Set the token through the environment rather than in a configuration file, so
it stays out of version control and out of the state file.

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
`true`), and the `vcs_repo` block.

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

## Data sources

- `webbpulse_workspace` reads one workspace by `workspace_id` or by `name`.
  Exactly one of the two is set. The API has no name lookup route, so a lookup
  by name lists every workspace and filters in the provider. It returns the
  same attributes as the resource, with `vcs_repo` as a computed object that is
  null when no repository is connected.
- `webbpulse_workspaces` returns every workspace's `ids` and `names`. The API
  takes no filters on its listing route.
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
the resource from state, so a workspace or variable deleted outside Terraform is
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
and `WEBBPULSE_TF_TOKEN` are both set. They create real workspaces, so CI
leaves them off and they should not be pointed at an environment whose
resources matter.

```sh
TF_ACC=1 WEBBPULSE_TF_HOST=... WEBBPULSE_TF_TOKEN=... go test ./... -run TestAcc -v
```

`TestAccWorkspaceVCS` also needs `WEBBPULSE_TF_ACC_VCS_REPO`, an `owner/name`
the environment's GitHub App is installed on, and skips without it.
`TestWorkspaceVCSLifecycle` drives the same flow through real Terraform plans
against an in-memory fake API, so it runs in `go test ./...` with no
credentials.

## Backlog

- An `hcl` flag on `webbpulse_variable`, once the API ships it
  (WebbPulse-Terraform PR 67, not merged yet).
- No runs resource or data source. `POST /workspaces/{id}/config-versions`
  exists, but the runs domain is not wrapped here yet, so a run cannot be
  queued from Terraform.
- No filters, pagination or name lookup on `GET /workspaces`, so
  `webbpulse_workspaces` returns everything and a lookup by name filters client
  side. That is fine at the current scale and will not stay fine.
- No bulk variable write, so a workspace with many variables makes one request
  per variable.
- No ETag or version on a workspace, so an update cannot be made conditional
  and a concurrent edit is last write wins.
- No registry, so install is a local build plus `dev_overrides`. Publishing and
  GPG signing come later.

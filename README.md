# terraform-provider-webbpulse

Terraform provider for the WebbPulse Terraform control plane. It manages
workspaces and their variables, and checks a workspace's run role.

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
| `host` | `WEBBPULSE_TF_HOST` | Base URL. The `/api/v1` suffix is added when absent. Defaults to staging. |
| `token` | `WEBBPULSE_TF_TOKEN` | An agent API key, carrying a `wpk_` prefix, sent as a bearer token. Marked sensitive. |

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

Computed: `workspace_id`, `created_at`, `updated_at`, `run_role_setup`
(`principal_arn`, `principal_arns`, `external_id`, `role_name`),
`run_role_checked_at`, `run_role_account_id`.

```sh
terraform import webbpulse_workspace.example ws-01JABCDEF0123456789ABCDEF
```

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

```sh
terraform import webbpulse_variable.example ws-01JABCDEF0123456789ABCDEF/region
```

## Data sources

- `webbpulse_workspace` reads one workspace by `workspace_id` or by `name`.
  Exactly one of the two is set. The API has no name lookup route, so a lookup
  by name lists every workspace and filters in the provider.
- `webbpulse_workspaces` returns every workspace's `ids` and `names`. The API
  takes no filters on its listing route.
- `webbpulse_run_role_check` assumes a workspace's run role and returns
  `connected`, `account_id` and `error`.

## The run role check

The control plane exposes the check on `/workspaces/{id}/run-role/check` in two
methods, and this provider uses both:

- `GET` performs the AssumeRole and returns the outcome, writing nothing.
- `POST` performs the same probe and additionally records the outcome on the
  workspace row, which is what the web UI reads between visits.

The check is a non CRUD operation that returns values, so it is shipped in both
shapes the Plugin Framework offers, because neither covers the whole need on its
own:

- The `webbpulse_run_role_check` **data source** is the one to reach for when a
  configuration needs the outcome as values. An action's `Invoke` hands back
  only diagnostics and progress messages, so `connected`, `account_id` and
  `error` cannot reach state through an action at all. It reads the `GET`, so
  the read is pure: the same workspace gives the same answer and no plan or
  refresh mutates anything.
- The `webbpulse_run_role_check` **action** is the one to reach for when a role
  that does not answer should stop an apply. It reports the outcome as a
  progress message and raises an error, or a warning when
  `fail_if_not_connected` is false. An action runs only during an apply, so it
  reads the `POST` and leaves the recorded outcome the UI shows.

A configured role that does not answer is a 200 with `connected` false, not an
error: the trust policy may simply not be in place yet. The data source reports
it as `connected = false` with the reason in `error`, so the problem is visible
in plan output instead of arriving as a provider error. A workspace with no
role at all is a 400 carrying `RUN_ROLE_MISSING`, which both shapes surface as
an error naming `run_role_arn`.

## Errors

The client keeps the API's own error envelope. A 404 on a read removes the
resource from state; every other status becomes a diagnostic carrying the API's
`message`, its `error_code` and the request id, so a failure can be traced back
to one request in the control plane's logs.

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

## Backlog

The API does not yet expose everything the provider will want:

- No runs resource or data source. `POST /workspaces/{id}/config-versions`
  exists, but the runs domain is not wrapped here yet, so a run cannot be
  queued from Terraform.
- No filters, pagination or name lookup on `GET /workspaces`, so
  `webbpulse_workspaces` returns everything and a lookup by name filters client
  side. That is fine at the current scale and will not stay fine.
- No bulk variable write, so a workspace with many variables makes one request
  per variable.
- No route returns a sensitive variable's value, so drift on one cannot be
  detected and it cannot be imported.
- A workspace edit drops null fields server side, so `run_role_arn` cannot be
  cleared once it is set. The provider raises an error rather than reporting a
  removal it cannot perform. Clearing it needs an API change.
- No ETag or version on a workspace, so an update cannot be made conditional
  and a concurrent edit is last write wins.
- No registry, so install is a local build plus `dev_overrides`. Publishing and
  GPG signing are phase 3.

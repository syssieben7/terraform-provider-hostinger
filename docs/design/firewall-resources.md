# Firewall resources for terraform-provider-hostinger — design + acceptance spec

Fork: `syssieben7/terraform-provider-hostinger` (branch `feat/firewall-resources`),
upstream `hostinger/terraform-provider-hostinger` v0.1.23 (SDK v2, hand-written
HTTP client in `hostinger/client.go`). Goal: manage the Hostinger VPS cloud
firewall declaratively; offer upstream as a PR afterwards.

## API contract (docs/design/hostinger-openapi-1.53.1.json, `/api/vps/v1/firewall*`)

- `POST /firewall {name}` → FirewallResource `{id,name,is_synced,rules[],created_at,updated_at}`
- `GET /firewall` (list), `GET /firewall/{id}`, `DELETE /firewall/{id}`
- `PUT /firewall/{id}/rules {rules:[{protocol,port,source,source_detail}], sync?:bool}` — **atomic replace of all rules**, optional sync to all VMs
- `POST /firewall/{id}/rules`, `PUT /firewall/{id}/rules/{ruleId}`, `DELETE …/{ruleId}` — per-rule (not used by the resource; replace-all is simpler and order-free)
- `POST /firewall/{id}/activate/{vmId}` / `deactivate/{vmId}` → ActionResource `{id,state ∈ success|error|delayed|sent|created}` (async: poll `GET /api/vps/v1/virtual-machines/{vmId}/actions/{actionId}`)
- `POST /firewall/{id}/sync/{vmId}` (action), `POST /firewall/{id}/sync` (all VMs)
- VM resource carries `firewall_group_id` (int, nullable) → the Read of an attachment.
- Rule fields: `protocol ∈ TCP,UDP,ICMP,GRE,any,ESP,AH,ICMPv6,SSH,HTTP,HTTPS,MySQL,PostgreSQL`; `port` "22" or "1024:2048" (required, even for ICMP → use "any"? verify live; fallback "1:65535"); `source ∈ any,custom`; `source_detail` = "any" | IP | CIDR | range. **Rules are accept-only** (response has `action: accept`); an active firewall is default-deny for everything else. Order is irrelevant.

## Resources

### `hostinger_firewall`
| attr | type | notes |
|---|---|---|
| `name` | string, required | in-place? API has no PUT for name → **ForceNew** (document) |
| `rule` | set of blocks `{protocol, port, source, source_detail}` | in-place via `PUT /rules` (replace-all, `sync=true`); **TypeSet** (order-free, hash on the 4 fields) |
| `id` | computed string | firewall id |
| `is_synced` | computed bool | from GET |
| `sync_on_update` | bool, default true | pass `sync` on replace |
Create: POST firewall → PUT rules (if any). Read: GET (404 → remove from state). Update: PUT rules. Delete: DELETE (API refuses while attached? → detach first is the attachment resource's job; document ordering with `depends_on`). Import: by id.

### `hostinger_firewall_attachment`
| attr | type | notes |
|---|---|---|
| `firewall_id` | int, required, ForceNew | |
| `virtual_machine_id` | int, required, ForceNew | |
| `id` | computed | `"<firewall_id>/<vm_id>"` |
Create: POST activate → poll action to `success` (timeout 2 min, `delayed` keeps polling). Read: GET VM → `firewall_group_id == firewall_id` else remove from state. Delete: POST deactivate → poll. Import: `"<firewall_id>/<vm_id>"`. Only one firewall per VM (API rule) — activating another replaces it; Read catches that as drift.

### data source `hostinger_firewall` (optional, if time): by `id` or `name`.

## Client methods (`hostinger/firewall_client.go`)
`CreateFirewall(name)`, `GetFirewall(id)`, `ListFirewalls()`, `DeleteFirewall(id)`, `ReplaceFirewallRules(id, rules, sync)`, `ActivateFirewall(id, vmID)`, `DeactivateFirewall(id, vmID)`, `SyncFirewall(id, vmID)`, `GetVMAction(vmID, actionID)`, `waitForAction(...)`. Same style as the existing client (addStandardHeaders, error with HTTP status + body). Handle 429 (90 req/min) with one retry after `Retry-After`.

## Tests (must pass in CI before any real-API test)
- Unit: `net/http/httptest` mock server implementing the endpoints above with an in-memory store; tests for each client method (status codes, 404, 429 retry) and resource CRUD through `resource.Test` with `ProviderFactories` pointing at the mock (`HOSTINGER_API_URL` override — add a `base_url` provider attribute / env var; upstream reads only the token).
- Set semantics: rules in different order → no diff (test).
- Acceptance (`TF_ACC=1`, real token, **only in a job the owner triggers**): create firewall `tf-acc-<rand>` with 2 rules, replace rules, delete. Attachment acceptance only against a VM id given via env — never automatically.

## Implementation notes

- **No `terraform` CLI in the dev container.** `resource.UnitTest`/`resource.Test`
  (SDK v2's harness) shell out to a real `terraform` binary and, absent one,
  `hc-install` tries to download it from releases.hashicorp.com; in
  `Makefile.dev`'s bare `golang:1.25-bookworm` container this fails
  (`unable to verify checksums signature: openpgp: key expired`), so there is
  no network fallback either. Rather than depend on that path, the resource
  tests (`firewall_resource_test.go`, `firewall_attachment_resource_test.go`)
  drive the CRUD functions (`resourceHostingerFirewallCreate/Read/Update/
  Delete/Import`, and the attachment equivalents) directly against the
  httptest mock, using `schema.TestResourceDataRaw` the same way the SDK's
  own internal tests do. This exercises every code path (create, rule
  replace-on-update, read-clears-state-on-404, delete, import, attachment
  activate/read/deactivate/import, drift removal) but does not exercise
  Terraform's own plan/diff engine. If/when a `terraform` binary or
  `TF_ACC_TERRAFORM_PATH` becomes available in CI, swapping in
  `resource.UnitTest` with `ProviderFactories` pointing at `base_url` is a
  drop-in replacement — the resource code itself doesn't change.
- **"Reordered rules => no diff"** is a property of `schema.TypeSet` itself
  (each element is hashed by content via `schema.HashResource`, so a `*schema.
  Set` doesn't care what order its elements were added in). Since we can't
  run a real `terraform plan`, `TestFirewallResource_RuleSetOrderIndependent`
  checks this directly: two configs with the same rules in different order
  produce `.Equal()` `*schema.Set` values.
- **ICMP `port`** is left as a plain required string with only a non-empty
  validator (not restricted to `any`/`1:65535`) — the spec flagged this as
  "verify live" and no token/API access exists here to check it. See "Open
  questions" below.
- **429 retry**: `firewallRequest` retries a request exactly once on HTTP 429,
  waiting `Retry-After` seconds (falling back to `firewallRetryDefaultWait` if
  the header is absent/unparsable). A second 429 is returned to the caller as
  an error.
- **`hostinger_firewall` delete** does not attempt to detach the firewall
  from any VM first; per the spec this is the attachment resource's job
  (`depends_on` ordering in the config). The mock does not simulate the API
  refusing to delete an attached firewall (behaviour unverified without a
  token), so this path isn't covered by a test.

### Open questions for the real-API acceptance run

- Does `PUT .../rules` accept `port: "any"` for `protocol: "ICMP"`/`"ICMPv6"`,
  or does it require an explicit range like `"1:65535"`? The OpenAPI schema
  marks `port` required with no enum/pattern restriction.
- Does `DELETE /firewall/{id}` on a firewall still activated on a VM return
  422/409, or does it silently detach? This affects whether documentation
  should warn about `depends_on` ordering or whether the provider should
  actively detach first.
- Whether `GET /virtual-machines/{id}` reflects `firewall_group_id` update
  synchronously with action completion, or lags briefly (relevant to the
  attachment resource's `Read`, which assumes it's synchronous once the
  action is `success`).

## Definition of done
1. `go build`, `go vet`, `gofmt -l` clean, `go test ./...` green in Forgejo CI (`.forgejo/workflows/ci.yml`, `container: golang`).
2. `docs/resources/firewall.md` + `firewall_attachment.md` generated (`tfplugindocs`) or hand-written in the same format; `examples/resources/…/resource.tf`.
3. CHANGELOG entry; provider version stays upstream's until release.
4. Reviewed diff ≤ ~900 lines; no changes to existing resources.

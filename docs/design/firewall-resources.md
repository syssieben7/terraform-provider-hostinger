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

## Definition of done
1. `go build`, `go vet`, `gofmt -l` clean, `go test ./...` green in Forgejo CI (`.forgejo/workflows/ci.yml`, `container: golang`).
2. `docs/resources/firewall.md` + `firewall_attachment.md` generated (`tfplugindocs`) or hand-written in the same format; `examples/resources/…/resource.tf`.
3. CHANGELOG entry; provider version stays upstream's until release.
4. Reviewed diff ≤ ~900 lines; no changes to existing resources.

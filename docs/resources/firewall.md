# hostinger_firewall

The `hostinger_firewall` resource manages a Hostinger VPS cloud firewall group and its rules. A firewall group is default-deny: only traffic matching one of its `rule` blocks is accepted. Attach it to a VM with `hostinger_firewall_attachment`.

---

## Example Usage

```hcl
resource "hostinger_firewall" "web" {
  name = "HTTP and SSH only"

  rule {
    protocol      = "SSH"
    port          = "22"
    source        = "any"
    source_detail = "any"
  }

  rule {
    protocol      = "HTTPS"
    port          = "443"
    source        = "any"
    source_detail = "any"
  }
}
```

---

## Argument Reference

- `name` – (Required, ForceNew) A name for the firewall group. The API has no rename endpoint, so changing this destroys and recreates the firewall.
- `rule` – (Optional) One or more accept rules. Order does not matter (it's a set). Each block supports:
  - `protocol` – (Required) One of `TCP`, `UDP`, `ICMP`, `GRE`, `any`, `ESP`, `AH`, `ICMPv6`, `SSH`, `HTTP`, `HTTPS`, `MySQL`, `PostgreSQL`.
  - `port` – (Required) A single port (`"22"`) or a range (`"1024:2048"`).
  - `source` – (Required) `any` or `custom`.
  - `source_detail` – (Required) `any`, a single IP, a CIDR, or an IP range.
- `sync_on_update` – (Optional) Whether replacing the rule set also pushes it to every VM the firewall is currently attached to. Defaults to `true`.

---

## Attributes Reference

- `id` – The firewall's ID.
- `is_synced` – Whether the firewall's current rules are synced to its attached VMs.

---

## Import

```
terraform import hostinger_firewall.web <firewall_id>
```

`sync_on_update` is not returned by the API and defaults to `true` on import.

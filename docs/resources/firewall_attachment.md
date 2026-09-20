# hostinger_firewall_attachment

The `hostinger_firewall_attachment` resource activates a `hostinger_firewall` group on a VPS instance. A VM can only have one active firewall at a time; activating a different one replaces it (which this resource's `Read` detects as drift).

---

## Example Usage

```hcl
resource "hostinger_firewall_attachment" "web" {
  firewall_id         = hostinger_firewall.web.id
  virtual_machine_id  = hostinger_vps.example.id
}
```

---

## Argument Reference

- `firewall_id` – (Required, ForceNew) ID of the firewall group to activate.
- `virtual_machine_id` – (Required, ForceNew) ID of the VPS to activate it on.

---

## Attributes Reference

- `id` – `"<firewall_id>/<virtual_machine_id>"`.

---

## Import

```
terraform import hostinger_firewall_attachment.web <firewall_id>/<virtual_machine_id>
```

---

## Notes

- Create and delete are asynchronous on the API side (activate/deactivate return an action that is polled until it reaches a terminal state, or times out after 2 minutes).
- If you also manage the firewall's rules with `hostinger_firewall`, add `depends_on = [hostinger_firewall.web]` if Terraform doesn't otherwise infer the ordering, and be aware that deleting a firewall while it's still attached to a VM is undefined behaviour on the API side — destroy the attachment first.

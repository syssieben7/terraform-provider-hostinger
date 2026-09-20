package hostinger

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
)

// resourceHostingerFirewallAttachment models the (at most one) firewall
// activated on a VM. There is no dedicated "attachment" API object: this
// wraps the activate/deactivate actions and reads back the VM's
// firewall_group_id.
func resourceHostingerFirewallAttachment() *schema.Resource {
	return &schema.Resource{
		CreateContext: resourceHostingerFirewallAttachmentCreate,
		ReadContext:   resourceHostingerFirewallAttachmentRead,
		DeleteContext: resourceHostingerFirewallAttachmentDelete,
		Importer: &schema.ResourceImporter{
			StateContext: resourceHostingerFirewallAttachmentImport,
		},
		Schema: map[string]*schema.Schema{
			"firewall_id": {
				Type:         schema.TypeInt,
				Required:     true,
				ForceNew:     true,
				ValidateFunc: validation.IntAtLeast(1),
			},
			"virtual_machine_id": {
				Type:         schema.TypeInt,
				Required:     true,
				ForceNew:     true,
				ValidateFunc: validation.IntAtLeast(1),
			},
		},
	}
}

func resourceHostingerFirewallAttachmentCreate(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	client := m.(*HostingerClient)
	firewallID := d.Get("firewall_id").(int)
	vmID := d.Get("virtual_machine_id").(int)

	action, err := client.ActivateFirewall(firewallID, vmID)
	if err != nil {
		return diag.FromErr(err)
	}
	if _, err := client.waitForAction(vmID, action.ID, firewallActionTimeout); err != nil {
		return diag.FromErr(err)
	}

	d.SetId(fmt.Sprintf("%d/%d", firewallID, vmID))
	return resourceHostingerFirewallAttachmentRead(ctx, d, m)
}

func resourceHostingerFirewallAttachmentRead(_ context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	client := m.(*HostingerClient)

	firewallID, vmID, err := parseFirewallAttachmentID(d.Id())
	if err != nil {
		return diag.FromErr(err)
	}

	vm, err := client.GetVirtualMachine(vmID)
	if errors.Is(err, ErrNotFound) {
		d.SetId("")
		return nil
	}
	if err != nil {
		return diag.FromErr(err)
	}

	if vm.FirewallGroupID == nil || *vm.FirewallGroupID != firewallID {
		// Drift: either detached, or another firewall was activated on
		// this VM (only one firewall can be active per VM).
		d.SetId("")
		return nil
	}

	if err := d.Set("firewall_id", firewallID); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("virtual_machine_id", vmID); err != nil {
		return diag.FromErr(err)
	}
	return nil
}

func resourceHostingerFirewallAttachmentDelete(_ context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	client := m.(*HostingerClient)

	firewallID, vmID, err := parseFirewallAttachmentID(d.Id())
	if err != nil {
		return diag.FromErr(err)
	}

	action, err := client.DeactivateFirewall(firewallID, vmID)
	if err != nil {
		return diag.FromErr(err)
	}
	if _, err := client.waitForAction(vmID, action.ID, firewallActionTimeout); err != nil {
		return diag.FromErr(err)
	}

	d.SetId("")
	return nil
}

func resourceHostingerFirewallAttachmentImport(ctx context.Context, d *schema.ResourceData, m interface{}) ([]*schema.ResourceData, error) {
	firewallID, vmID, err := parseFirewallAttachmentID(d.Id())
	if err != nil {
		return nil, err
	}
	if err := d.Set("firewall_id", firewallID); err != nil {
		return nil, err
	}
	if err := d.Set("virtual_machine_id", vmID); err != nil {
		return nil, err
	}
	if diags := resourceHostingerFirewallAttachmentRead(ctx, d, m); diags.HasError() {
		return nil, errors.New(diags[0].Summary)
	}
	return []*schema.ResourceData{d}, nil
}

func parseFirewallAttachmentID(id string) (firewallID int, vmID int, err error) {
	parts := strings.SplitN(id, "/", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid firewall attachment id %q, expected \"<firewall_id>/<virtual_machine_id>\"", id)
	}
	firewallID, err = strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid firewall_id in id %q: %w", id, err)
	}
	vmID, err = strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid virtual_machine_id in id %q: %w", id, err)
	}
	return firewallID, vmID, nil
}

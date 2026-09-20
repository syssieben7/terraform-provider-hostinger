package hostinger

import (
	"context"
	"errors"
	"strconv"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
)

var firewallProtocolEnum = []string{
	"TCP", "UDP", "ICMP", "GRE", "any", "ESP", "AH", "ICMPv6",
	"SSH", "HTTP", "HTTPS", "MySQL", "PostgreSQL",
}

var firewallSourceEnum = []string{"any", "custom"}

func resourceHostingerFirewall() *schema.Resource {
	return &schema.Resource{
		CreateContext: resourceHostingerFirewallCreate,
		ReadContext:   resourceHostingerFirewallRead,
		UpdateContext: resourceHostingerFirewallUpdate,
		DeleteContext: resourceHostingerFirewallDelete,
		Importer: &schema.ResourceImporter{
			StateContext: resourceHostingerFirewallImport,
		},
		Schema: map[string]*schema.Schema{
			"name": {
				Type:         schema.TypeString,
				Required:     true,
				ForceNew:     true,
				Description:  "Firewall group name. The API has no rename endpoint, so changing this replaces the firewall.",
				ValidateFunc: validation.StringIsNotEmpty,
			},
			"sync_on_update": {
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     true,
				Description: "Whether replacing the rule set also syncs it to every VM the firewall is attached to.",
			},
			"is_synced": {
				Type:        schema.TypeBool,
				Computed:    true,
				Description: "Whether the firewall's current rules are synced to its attached VMs.",
			},
			"rule": {
				Type:        schema.TypeSet,
				Optional:    true,
				Description: "Accept rule. The firewall is default-deny for anything not matched by a rule.",
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"protocol": {
							Type:         schema.TypeString,
							Required:     true,
							ValidateFunc: validation.StringInSlice(firewallProtocolEnum, false),
						},
						"port": {
							Type:         schema.TypeString,
							Required:     true,
							Description:  "Single port (\"22\") or range (\"1024:2048\").",
							ValidateFunc: validation.StringIsNotEmpty,
						},
						"source": {
							Type:         schema.TypeString,
							Required:     true,
							ValidateFunc: validation.StringInSlice(firewallSourceEnum, false),
						},
						"source_detail": {
							Type:         schema.TypeString,
							Required:     true,
							Description:  "\"any\", a single IP, a CIDR, or an IP range.",
							ValidateFunc: validation.StringIsNotEmpty,
						},
					},
				},
			},
		},
	}
}

func expandFirewallRules(raw *schema.Set) []FirewallRuleInput {
	rules := make([]FirewallRuleInput, 0, raw.Len())
	for _, item := range raw.List() {
		r := item.(map[string]interface{})
		rules = append(rules, FirewallRuleInput{
			Protocol:     r["protocol"].(string),
			Port:         r["port"].(string),
			Source:       r["source"].(string),
			SourceDetail: r["source_detail"].(string),
		})
	}
	return rules
}

func flattenFirewallRules(rules []FirewallRule) []interface{} {
	out := make([]interface{}, 0, len(rules))
	for _, r := range rules {
		out = append(out, map[string]interface{}{
			"protocol":      r.Protocol,
			"port":          r.Port,
			"source":        r.Source,
			"source_detail": r.SourceDetail,
		})
	}
	return out
}

func resourceHostingerFirewallCreate(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	client := m.(*HostingerClient)

	fw, err := client.CreateFirewall(d.Get("name").(string))
	if err != nil {
		return diag.FromErr(err)
	}
	d.SetId(strconv.Itoa(fw.ID))

	if ruleSet := d.Get("rule").(*schema.Set); ruleSet.Len() > 0 {
		if _, err := client.ReplaceFirewallRules(fw.ID, expandFirewallRules(ruleSet), d.Get("sync_on_update").(bool)); err != nil {
			return diag.FromErr(err)
		}
	}

	return resourceHostingerFirewallRead(ctx, d, m)
}

func resourceHostingerFirewallRead(_ context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	client := m.(*HostingerClient)

	id, err := strconv.Atoi(d.Id())
	if err != nil {
		return diag.FromErr(err)
	}

	fw, err := client.GetFirewall(id)
	if errors.Is(err, ErrNotFound) {
		d.SetId("")
		return nil
	}
	if err != nil {
		return diag.FromErr(err)
	}

	if err := d.Set("name", fw.Name); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("is_synced", fw.IsSynced); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("rule", flattenFirewallRules(fw.Rules)); err != nil {
		return diag.FromErr(err)
	}

	return nil
}

func resourceHostingerFirewallUpdate(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	client := m.(*HostingerClient)

	id, err := strconv.Atoi(d.Id())
	if err != nil {
		return diag.FromErr(err)
	}

	if d.HasChange("rule") {
		ruleSet := d.Get("rule").(*schema.Set)
		if _, err := client.ReplaceFirewallRules(id, expandFirewallRules(ruleSet), d.Get("sync_on_update").(bool)); err != nil {
			return diag.FromErr(err)
		}
	}

	return resourceHostingerFirewallRead(ctx, d, m)
}

func resourceHostingerFirewallDelete(_ context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	client := m.(*HostingerClient)

	id, err := strconv.Atoi(d.Id())
	if err != nil {
		return diag.FromErr(err)
	}

	if err := client.DeleteFirewall(id); err != nil && !errors.Is(err, ErrNotFound) {
		return diag.FromErr(err)
	}
	d.SetId("")
	return nil
}

func resourceHostingerFirewallImport(ctx context.Context, d *schema.ResourceData, m interface{}) ([]*schema.ResourceData, error) {
	if err := d.Set("sync_on_update", true); err != nil {
		return nil, err
	}
	if diags := resourceHostingerFirewallRead(ctx, d, m); diags.HasError() {
		return nil, errors.New(diags[0].Summary)
	}
	return []*schema.ResourceData{d}, nil
}

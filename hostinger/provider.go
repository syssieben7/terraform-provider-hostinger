package hostinger

import (
	"context"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

// Provider returns the *schema.Provider for Hostinger VPS
func Provider() *schema.Provider {
	return &schema.Provider{
		Schema: map[string]*schema.Schema{
			"api_token": {
				Type:        schema.TypeString,
				Required:    true,
				Sensitive:   true,
				Description: "API token for authenticating with Hostinger API.",
				DefaultFunc: schema.EnvDefaultFunc("HOSTINGER_API_TOKEN", nil),
			},
			"base_url": {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "Override the Hostinger API base URL. Defaults to https://developers.hostinger.com.",
				DefaultFunc: schema.EnvDefaultFunc("HOSTINGER_API_URL", "https://developers.hostinger.com"),
			},
		},
		ResourcesMap: map[string]*schema.Resource{
			"hostinger_vps":                     resourceHostingerVPS(),
			"hostinger_vps_post_install_script": resourceHostingerVPSPostInstallScript(),
			"hostinger_vps_ssh_key":             resourceHostingerVPSSSHKey(),
			"hostinger_dns_record":              resourceHostingerDNSRecord(),
			"hostinger_firewall":                resourceHostingerFirewall(),
			"hostinger_firewall_attachment":     resourceHostingerFirewallAttachment(),
		},
		DataSourcesMap: map[string]*schema.Resource{
			"hostinger_vps_templates":    dataSourceHostingerVPSTemplates(),
			"hostinger_vps_data_centers": dataSourceHostingerVPSDataCenters(),
			"hostinger_vps_plans":        dataSourceHostingerVPSPlans(),
		},
		ConfigureContextFunc: providerConfigure,
	}
}

// providerConfigure creates a Hostinger API client using the provided API token
func providerConfigure(_ context.Context, d *schema.ResourceData) (interface{}, diag.Diagnostics) {
	var diags diag.Diagnostics

	token := d.Get("api_token").(string)
	if token == "" {
		diags = append(diags, diag.Diagnostic{
			Severity: diag.Error,
			Summary:  "API token is required",
			Detail:   "The Hostinger API token must be provided to use this provider.",
		})
		return nil, diags
	}

	// Initialize the Hostinger API client
	client := NewHostingerClient(token, "0.1.22")
	if baseURL := d.Get("base_url").(string); baseURL != "" {
		client.BaseURL = baseURL
	}
	return client, diags
}

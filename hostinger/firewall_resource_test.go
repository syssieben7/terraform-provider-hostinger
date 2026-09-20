package hostinger

import (
	"context"
	"strconv"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

// The SDK v2 test harness (resource.UnitTest / resource.Test) shells out to
// a real `terraform` binary via hc-install, which this container does not
// have and cannot reach (see docs/design/firewall-resources.md
// "Implementation notes"). These tests instead drive the CRUD functions
// directly against the httptest mock, using schema.TestResourceDataRaw the
// same way the SDK's own internal tests do.

func firewallRawConfig(name string, syncOnUpdate bool, rules []interface{}) map[string]interface{} {
	return map[string]interface{}{
		"name":           name,
		"sync_on_update": syncOnUpdate,
		"rule":           rules,
	}
}

func ruleMap(protocol, port, source, sourceDetail string) map[string]interface{} {
	return map[string]interface{}{
		"protocol":      protocol,
		"port":          port,
		"source":        source,
		"source_detail": sourceDetail,
	}
}

func TestFirewallResource_CreateReadDelete(t *testing.T) {
	mock := newFirewallMockServer()
	defer mock.Close()
	client := newTestClient(mock.URL())
	ctx := context.Background()

	raw := firewallRawConfig("web", true, []interface{}{
		ruleMap("TCP", "22", "any", "any"),
		ruleMap("TCP", "443", "any", "any"),
	})
	d := schema.TestResourceDataRaw(t, resourceHostingerFirewall().Schema, raw)

	if diags := resourceHostingerFirewallCreate(ctx, d, client); diags.HasError() {
		t.Fatalf("create: %v", diags)
	}
	if d.Id() == "" {
		t.Fatal("expected id to be set after create")
	}
	if got := d.Get("rule").(*schema.Set).Len(); got != 2 {
		t.Fatalf("expected 2 rules after create, got %d", got)
	}
	if !d.Get("is_synced").(bool) {
		t.Fatalf("expected is_synced=true after create with sync_on_update=true")
	}

	// Read again from scratch to make sure state round-trips through the API.
	d2 := schema.TestResourceDataRaw(t, resourceHostingerFirewall().Schema, map[string]interface{}{})
	d2.SetId(d.Id())
	if diags := resourceHostingerFirewallRead(ctx, d2, client); diags.HasError() {
		t.Fatalf("read: %v", diags)
	}
	if d2.Get("name").(string) != "web" {
		t.Fatalf("expected name 'web', got %q", d2.Get("name").(string))
	}

	if diags := resourceHostingerFirewallDelete(ctx, d, client); diags.HasError() {
		t.Fatalf("delete: %v", diags)
	}
	if d.Id() != "" {
		t.Fatalf("expected id cleared after delete")
	}

	d3 := schema.TestResourceDataRaw(t, resourceHostingerFirewall().Schema, map[string]interface{}{})
	d3.SetId(d2.Id())
	if diags := resourceHostingerFirewallRead(ctx, d3, client); diags.HasError() {
		t.Fatalf("read after delete: %v", diags)
	}
	if d3.Id() != "" {
		t.Fatalf("expected read to clear id for a deleted firewall (404 handling)")
	}
}

func TestFirewallResource_UpdateRulesChanged(t *testing.T) {
	mock := newFirewallMockServer()
	defer mock.Close()
	client := newTestClient(mock.URL())
	ctx := context.Background()

	createRaw := firewallRawConfig("web", true, []interface{}{ruleMap("TCP", "22", "any", "any")})
	dCreate := schema.TestResourceDataRaw(t, resourceHostingerFirewall().Schema, createRaw)
	if diags := resourceHostingerFirewallCreate(ctx, dCreate, client); diags.HasError() {
		t.Fatalf("create: %v", diags)
	}
	id := dCreate.Id()

	updateRaw := firewallRawConfig("web", true, []interface{}{
		ruleMap("TCP", "22", "any", "any"),
		ruleMap("TCP", "80", "custom", "10.0.0.0/24"),
	})
	dUpdate := schema.TestResourceDataRaw(t, resourceHostingerFirewall().Schema, updateRaw)
	dUpdate.SetId(id)
	if !dUpdate.HasChange("rule") {
		t.Fatal("expected HasChange(rule) to be true when diffing against a nil prior state")
	}
	if diags := resourceHostingerFirewallUpdate(ctx, dUpdate, client); diags.HasError() {
		t.Fatalf("update: %v", diags)
	}

	idInt, err := strconv.Atoi(id)
	if err != nil {
		t.Fatalf("invalid id %q: %v", id, err)
	}
	fw, err := client.GetFirewall(idInt)
	if err != nil {
		t.Fatalf("GetFirewall: %v", err)
	}
	if len(fw.Rules) != 2 {
		t.Fatalf("expected 2 rules after update, got %d", len(fw.Rules))
	}
}

// TestFirewallResource_RuleSetOrderIndependent verifies the property the
// spec's "reordered rules => no diff" acceptance check relies on: TypeSet
// hashes each rule by content, so two configs listing the same rules in a
// different order produce equal sets (and thus no plan diff for `rule`).
func TestFirewallResource_RuleSetOrderIndependent(t *testing.T) {
	rulesA := []interface{}{
		ruleMap("TCP", "22", "any", "any"),
		ruleMap("TCP", "443", "any", "any"),
	}
	rulesB := []interface{}{
		ruleMap("TCP", "443", "any", "any"),
		ruleMap("TCP", "22", "any", "any"),
	}

	dA := schema.TestResourceDataRaw(t, resourceHostingerFirewall().Schema, firewallRawConfig("web", true, rulesA))
	dB := schema.TestResourceDataRaw(t, resourceHostingerFirewall().Schema, firewallRawConfig("web", true, rulesB))

	setA := dA.Get("rule").(*schema.Set)
	setB := dB.Get("rule").(*schema.Set)
	if !setA.Equal(setB) {
		t.Fatalf("expected reordered rule sets to be equal, got %v vs %v", setA.List(), setB.List())
	}
}

func TestFirewallResource_Import(t *testing.T) {
	mock := newFirewallMockServer()
	defer mock.Close()
	client := newTestClient(mock.URL())
	ctx := context.Background()

	fw, err := client.CreateFirewall("imported")
	if err != nil {
		t.Fatalf("CreateFirewall: %v", err)
	}

	d := schema.TestResourceDataRaw(t, resourceHostingerFirewall().Schema, map[string]interface{}{})
	d.SetId(strconv.Itoa(fw.ID))

	results, err := resourceHostingerFirewallImport(ctx, d, client)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 imported resource, got %d", len(results))
	}
	if results[0].Get("name").(string) != "imported" {
		t.Fatalf("expected name 'imported', got %q", results[0].Get("name").(string))
	}
	if !results[0].Get("sync_on_update").(bool) {
		t.Fatalf("expected sync_on_update to default to true on import")
	}
}

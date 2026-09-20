package hostinger

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func TestFirewallAttachmentResource_CreateReadDelete(t *testing.T) {
	withFastPolling(t)
	mock := newFirewallMockServer()
	defer mock.Close()
	client := newTestClient(mock.URL())
	ctx := context.Background()

	mock.putVM(&VirtualMachine{ID: 100})
	fw, err := client.CreateFirewall("attach-me")
	if err != nil {
		t.Fatalf("CreateFirewall: %v", err)
	}

	raw := map[string]interface{}{
		"firewall_id":        fw.ID,
		"virtual_machine_id": 100,
	}
	d := schema.TestResourceDataRaw(t, resourceHostingerFirewallAttachment().Schema, raw)

	if diags := resourceHostingerFirewallAttachmentCreate(ctx, d, client); diags.HasError() {
		t.Fatalf("create: %v", diags)
	}
	wantID := fmt.Sprintf("%d/%d", fw.ID, 100)
	if d.Id() != wantID {
		t.Fatalf("expected id %q, got %q", wantID, d.Id())
	}

	vm, err := client.GetVirtualMachine(100)
	if err != nil {
		t.Fatalf("GetVirtualMachine: %v", err)
	}
	if vm.FirewallGroupID == nil || *vm.FirewallGroupID != fw.ID {
		t.Fatalf("expected vm attached to firewall %d, got %+v", fw.ID, vm.FirewallGroupID)
	}

	// Read from scratch by id, as import would.
	d2 := schema.TestResourceDataRaw(t, resourceHostingerFirewallAttachment().Schema, map[string]interface{}{})
	d2.SetId(wantID)
	if diags := resourceHostingerFirewallAttachmentRead(ctx, d2, client); diags.HasError() {
		t.Fatalf("read: %v", diags)
	}
	if d2.Get("firewall_id").(int) != fw.ID || d2.Get("virtual_machine_id").(int) != 100 {
		t.Fatalf("unexpected read state: firewall_id=%d vm=%d", d2.Get("firewall_id").(int), d2.Get("virtual_machine_id").(int))
	}

	if diags := resourceHostingerFirewallAttachmentDelete(ctx, d, client); diags.HasError() {
		t.Fatalf("delete: %v", diags)
	}

	vmAfter, err := client.GetVirtualMachine(100)
	if err != nil {
		t.Fatalf("GetVirtualMachine after delete: %v", err)
	}
	if vmAfter.FirewallGroupID != nil {
		t.Fatalf("expected firewall detached, got %v", *vmAfter.FirewallGroupID)
	}
}

func TestFirewallAttachmentResource_ReadDriftRemovesFromState(t *testing.T) {
	mock := newFirewallMockServer()
	defer mock.Close()
	client := newTestClient(mock.URL())
	ctx := context.Background()

	other := 999
	mock.putVM(&VirtualMachine{ID: 200, FirewallGroupID: &other})

	d := schema.TestResourceDataRaw(t, resourceHostingerFirewallAttachment().Schema, map[string]interface{}{})
	d.SetId("1/200")

	if diags := resourceHostingerFirewallAttachmentRead(ctx, d, client); diags.HasError() {
		t.Fatalf("read: %v", diags)
	}
	if d.Id() != "" {
		t.Fatalf("expected drift (different active firewall) to remove resource from state")
	}
}

func TestFirewallAttachmentResource_Import(t *testing.T) {
	withFastPolling(t)
	mock := newFirewallMockServer()
	defer mock.Close()
	client := newTestClient(mock.URL())
	ctx := context.Background()

	mock.putVM(&VirtualMachine{ID: 55})
	fw, _ := client.CreateFirewall("import-attach")
	action, err := client.ActivateFirewall(fw.ID, 55)
	if err != nil {
		t.Fatalf("ActivateFirewall: %v", err)
	}
	if _, err := client.waitForAction(55, action.ID, firewallActionTimeout); err != nil {
		t.Fatalf("waitForAction: %v", err)
	}

	d := schema.TestResourceDataRaw(t, resourceHostingerFirewallAttachment().Schema, map[string]interface{}{})
	d.SetId(fmt.Sprintf("%d/%d", fw.ID, 55))

	results, err := resourceHostingerFirewallAttachmentImport(ctx, d, client)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 imported resource, got %d", len(results))
	}
	if results[0].Get("firewall_id").(int) != fw.ID {
		t.Fatalf("expected firewall_id %d, got %d", fw.ID, results[0].Get("firewall_id").(int))
	}
}

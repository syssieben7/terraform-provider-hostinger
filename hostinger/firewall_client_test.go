package hostinger

import (
	"errors"
	"testing"
	"time"
)

func withFastPolling(t *testing.T) {
	t.Helper()
	origInterval, origTimeout := firewallActionPollInterval, firewallActionTimeout
	firewallActionPollInterval = 5 * time.Millisecond
	firewallActionTimeout = 2 * time.Second
	t.Cleanup(func() {
		firewallActionPollInterval = origInterval
		firewallActionTimeout = origTimeout
	})
}

func TestFirewallClient_CreateGetList(t *testing.T) {
	mock := newFirewallMockServer()
	defer mock.Close()
	client := newTestClient(mock.URL())

	fw, err := client.CreateFirewall("web")
	if err != nil {
		t.Fatalf("CreateFirewall: %v", err)
	}
	if fw.Name != "web" || fw.ID == 0 {
		t.Fatalf("unexpected firewall: %+v", fw)
	}

	got, err := client.GetFirewall(fw.ID)
	if err != nil {
		t.Fatalf("GetFirewall: %v", err)
	}
	if got.ID != fw.ID {
		t.Fatalf("expected id %d, got %d", fw.ID, got.ID)
	}

	list, err := client.ListFirewalls()
	if err != nil {
		t.Fatalf("ListFirewalls: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 firewall, got %d", len(list))
	}
}

func TestFirewallClient_GetFirewallNotFound(t *testing.T) {
	mock := newFirewallMockServer()
	defer mock.Close()
	client := newTestClient(mock.URL())

	_, err := client.GetFirewall(999)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestFirewallClient_ReplaceFirewallRules(t *testing.T) {
	mock := newFirewallMockServer()
	defer mock.Close()
	client := newTestClient(mock.URL())

	fw, err := client.CreateFirewall("web")
	if err != nil {
		t.Fatalf("CreateFirewall: %v", err)
	}

	rules := []FirewallRuleInput{
		{Protocol: "TCP", Port: "22", Source: "any", SourceDetail: "any"},
		{Protocol: "TCP", Port: "443", Source: "any", SourceDetail: "any"},
	}
	updated, err := client.ReplaceFirewallRules(fw.ID, rules, true)
	if err != nil {
		t.Fatalf("ReplaceFirewallRules: %v", err)
	}
	if len(updated.Rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(updated.Rules))
	}
	if !updated.IsSynced {
		t.Fatalf("expected is_synced=true")
	}
}

func TestFirewallClient_DeleteFirewall(t *testing.T) {
	mock := newFirewallMockServer()
	defer mock.Close()
	client := newTestClient(mock.URL())

	fw, _ := client.CreateFirewall("web")
	if err := client.DeleteFirewall(fw.ID); err != nil {
		t.Fatalf("DeleteFirewall: %v", err)
	}
	if _, err := client.GetFirewall(fw.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected firewall to be gone, got %v", err)
	}
}

func TestFirewallClient_ActivateAndWaitForAction(t *testing.T) {
	withFastPolling(t)
	mock := newFirewallMockServer()
	defer mock.Close()
	client := newTestClient(mock.URL())
	mock.putVM(&VirtualMachine{ID: 42})

	fw, _ := client.CreateFirewall("web")

	action, err := client.ActivateFirewall(fw.ID, 42)
	if err != nil {
		t.Fatalf("ActivateFirewall: %v", err)
	}
	if action.State != "created" {
		t.Fatalf("expected initial state 'created', got %q", action.State)
	}

	final, err := client.waitForAction(42, action.ID, firewallActionTimeout)
	if err != nil {
		t.Fatalf("waitForAction: %v", err)
	}
	if final.State != "success" {
		t.Fatalf("expected final state 'success', got %q", final.State)
	}

	vm, err := client.GetVirtualMachine(42)
	if err != nil {
		t.Fatalf("GetVirtualMachine: %v", err)
	}
	if vm.FirewallGroupID == nil || *vm.FirewallGroupID != fw.ID {
		t.Fatalf("expected vm.FirewallGroupID=%d, got %+v", fw.ID, vm.FirewallGroupID)
	}
}

func TestFirewallClient_DeactivateClearsGroup(t *testing.T) {
	withFastPolling(t)
	mock := newFirewallMockServer()
	defer mock.Close()
	client := newTestClient(mock.URL())
	mock.putVM(&VirtualMachine{ID: 7})

	fw, _ := client.CreateFirewall("web")
	action, err := client.ActivateFirewall(fw.ID, 7)
	if err != nil {
		t.Fatalf("ActivateFirewall: %v", err)
	}
	if _, err := client.waitForAction(7, action.ID, firewallActionTimeout); err != nil {
		t.Fatalf("waitForAction (activate): %v", err)
	}

	deact, err := client.DeactivateFirewall(fw.ID, 7)
	if err != nil {
		t.Fatalf("DeactivateFirewall: %v", err)
	}
	if _, err := client.waitForAction(7, deact.ID, firewallActionTimeout); err != nil {
		t.Fatalf("waitForAction (deactivate): %v", err)
	}

	vm, err := client.GetVirtualMachine(7)
	if err != nil {
		t.Fatalf("GetVirtualMachine: %v", err)
	}
	if vm.FirewallGroupID != nil {
		t.Fatalf("expected FirewallGroupID nil after deactivate, got %v", *vm.FirewallGroupID)
	}
}

func TestFirewallClient_SyncFirewall(t *testing.T) {
	mock := newFirewallMockServer()
	defer mock.Close()
	client := newTestClient(mock.URL())
	mock.putVM(&VirtualMachine{ID: 3})

	fw, _ := client.CreateFirewall("web")
	action, err := client.SyncFirewall(fw.ID, 3)
	if err != nil {
		t.Fatalf("SyncFirewall: %v", err)
	}
	if action.State != "success" {
		t.Fatalf("expected sync action to report success, got %q", action.State)
	}
}

func TestFirewallClient_Retries429(t *testing.T) {
	mock := newFirewallMockServer()
	defer mock.Close()
	mock.force429Once = true
	client := newTestClient(mock.URL())

	fw, err := client.CreateFirewall("flaky")
	if err != nil {
		t.Fatalf("expected CreateFirewall to succeed after one retry, got %v", err)
	}
	if fw.Name != "flaky" {
		t.Fatalf("unexpected firewall: %+v", fw)
	}
}

func TestFirewallClient_GetVMActionNotFound(t *testing.T) {
	mock := newFirewallMockServer()
	defer mock.Close()
	client := newTestClient(mock.URL())

	_, err := client.GetVMAction(1, 999)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

package hostinger

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// firewallActionPollInterval and firewallActionTimeout bound how waitForAction
// polls a VM action. They are variables so tests can shrink them.
var (
	firewallActionPollInterval = 2 * time.Second
	firewallActionTimeout      = 2 * time.Minute
)

// firewallRetryDefaultWait is used when a 429 response has no (or an
// unparsable) Retry-After header.
var firewallRetryDefaultWait = 1 * time.Second

// FirewallRuleInput is the payload for creating/replacing a firewall rule.
type FirewallRuleInput struct {
	Protocol     string `json:"protocol"`
	Port         string `json:"port"`
	Source       string `json:"source"`
	SourceDetail string `json:"source_detail"`
}

// FirewallRule is a rule as returned by the API (includes its ID and the
// server-assigned action, which is always "accept" for rules the resource
// manages).
type FirewallRule struct {
	ID           int    `json:"id"`
	Action       string `json:"action"`
	Protocol     string `json:"protocol"`
	Port         string `json:"port"`
	Source       string `json:"source"`
	SourceDetail string `json:"source_detail"`
}

// Firewall is a firewall group as returned by the API.
type Firewall struct {
	ID        int            `json:"id"`
	Name      string         `json:"name"`
	IsSynced  bool           `json:"is_synced"`
	Rules     []FirewallRule `json:"rules"`
	CreatedAt string         `json:"created_at,omitempty"`
	UpdatedAt string         `json:"updated_at,omitempty"`
}

type firewallListResponse struct {
	Data []Firewall `json:"data"`
}

// ActionResource mirrors VPS.V1.Action.ActionResource: the async job created
// by activate/deactivate/sync, polled via GetVMAction.
type ActionResource struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	State     string `json:"state"`
	CreatedAt string `json:"created_at,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

// firewallRequest performs an HTTP request against the Hostinger API,
// retrying once on HTTP 429 (respecting Retry-After), and decodes a JSON
// response into out (when out is non-nil and the response has a body).
func (c *HostingerClient) firewallRequest(method, url string, body interface{}, out interface{}) error {
	var bodyBytes []byte
	if body != nil {
		var err error
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("failed to marshal request: %w", err)
		}
	}

	var resp *http.Response
	for attempt := 0; attempt < 2; attempt++ {
		var reader io.Reader
		if bodyBytes != nil {
			reader = bytes.NewReader(bodyBytes)
		}
		req, err := http.NewRequest(method, url, reader)
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}
		c.addStandardHeaders(req)

		resp, err = c.HTTPClient.Do(req)
		if err != nil {
			return fmt.Errorf("API request failed: %w", err)
		}

		if resp.StatusCode == http.StatusTooManyRequests && attempt == 0 {
			wait := firewallRetryDefaultWait
			if ra := resp.Header.Get("Retry-After"); ra != "" {
				if secs, err := strconv.Atoi(ra); err == nil && secs >= 0 {
					wait = time.Duration(secs) * time.Second
				}
			}
			_ = resp.Body.Close()
			time.Sleep(wait)
			continue
		}
		break
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("hostinger API request failed (HTTP %d): %s", resp.StatusCode, string(msg))
	}

	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("invalid API response: %w", err)
	}
	return nil
}

// CreateFirewall creates a new (empty) firewall group.
func (c *HostingerClient) CreateFirewall(name string) (*Firewall, error) {
	var fw Firewall
	url := c.BaseURL + "/api/vps/v1/firewall"
	if err := c.firewallRequest("POST", url, map[string]string{"name": name}, &fw); err != nil {
		return nil, err
	}
	return &fw, nil
}

// GetFirewall retrieves a firewall group by ID. Returns ErrNotFound if it
// does not exist.
func (c *HostingerClient) GetFirewall(id int) (*Firewall, error) {
	var fw Firewall
	url := fmt.Sprintf("%s/api/vps/v1/firewall/%d", c.BaseURL, id)
	if err := c.firewallRequest("GET", url, nil, &fw); err != nil {
		return nil, err
	}
	return &fw, nil
}

// ListFirewalls lists all firewall groups in the account.
func (c *HostingerClient) ListFirewalls() ([]Firewall, error) {
	var list firewallListResponse
	url := c.BaseURL + "/api/vps/v1/firewall"
	if err := c.firewallRequest("GET", url, nil, &list); err != nil {
		return nil, err
	}
	return list.Data, nil
}

// DeleteFirewall deletes a firewall group by ID.
func (c *HostingerClient) DeleteFirewall(id int) error {
	url := fmt.Sprintf("%s/api/vps/v1/firewall/%d", c.BaseURL, id)
	return c.firewallRequest("DELETE", url, nil, nil)
}

// ReplaceFirewallRules atomically replaces all rules of a firewall group,
// optionally syncing the result to every VM the firewall is attached to.
func (c *HostingerClient) ReplaceFirewallRules(id int, rules []FirewallRuleInput, sync bool) (*Firewall, error) {
	if rules == nil {
		rules = []FirewallRuleInput{}
	}
	payload := map[string]interface{}{
		"rules": rules,
		"sync":  sync,
	}
	var fw Firewall
	url := fmt.Sprintf("%s/api/vps/v1/firewall/%d/rules", c.BaseURL, id)
	if err := c.firewallRequest("PUT", url, payload, &fw); err != nil {
		return nil, err
	}
	return &fw, nil
}

// ActivateFirewall attaches a firewall group to a VM. This is async: the
// returned action must be polled (see waitForAction) until it settles.
func (c *HostingerClient) ActivateFirewall(firewallID, vmID int) (*ActionResource, error) {
	var action ActionResource
	url := fmt.Sprintf("%s/api/vps/v1/firewall/%d/activate/%d", c.BaseURL, firewallID, vmID)
	if err := c.firewallRequest("POST", url, nil, &action); err != nil {
		return nil, err
	}
	return &action, nil
}

// DeactivateFirewall detaches a firewall group from a VM (async, see
// ActivateFirewall).
func (c *HostingerClient) DeactivateFirewall(firewallID, vmID int) (*ActionResource, error) {
	var action ActionResource
	url := fmt.Sprintf("%s/api/vps/v1/firewall/%d/deactivate/%d", c.BaseURL, firewallID, vmID)
	if err := c.firewallRequest("POST", url, nil, &action); err != nil {
		return nil, err
	}
	return &action, nil
}

// SyncFirewall re-pushes a firewall group's current rules to a specific VM
// (async, see ActivateFirewall).
func (c *HostingerClient) SyncFirewall(firewallID, vmID int) (*ActionResource, error) {
	var action ActionResource
	url := fmt.Sprintf("%s/api/vps/v1/firewall/%d/sync/%d", c.BaseURL, firewallID, vmID)
	if err := c.firewallRequest("POST", url, nil, &action); err != nil {
		return nil, err
	}
	return &action, nil
}

// GetVMAction retrieves the current state of an async action performed on a
// VM (created by activate/deactivate/sync).
func (c *HostingerClient) GetVMAction(vmID, actionID int) (*ActionResource, error) {
	var action ActionResource
	url := fmt.Sprintf("%s/api/vps/v1/virtual-machines/%d/actions/%d", c.BaseURL, vmID, actionID)
	if err := c.firewallRequest("GET", url, nil, &action); err != nil {
		return nil, err
	}
	return &action, nil
}

// waitForAction polls an action until it reaches a terminal state
// ("success" or "error") or timeout elapses. "delayed", "sent" and
// "created" are treated as still in progress.
func (c *HostingerClient) waitForAction(vmID, actionID int, timeout time.Duration) (*ActionResource, error) {
	deadline := time.Now().Add(timeout)
	for {
		action, err := c.GetVMAction(vmID, actionID)
		if err != nil {
			return nil, err
		}
		switch action.State {
		case "success":
			return action, nil
		case "error":
			return action, fmt.Errorf("action %d on VM %d failed", actionID, vmID)
		}
		if time.Now().After(deadline) {
			return action, fmt.Errorf("timed out waiting for action %d on VM %d (last state: %s)", actionID, vmID, action.State)
		}
		time.Sleep(firewallActionPollInterval)
	}
}

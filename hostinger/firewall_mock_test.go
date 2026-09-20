package hostinger

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
)

// firewallMockServer is an in-memory httptest server implementing enough of
// the /api/vps/v1/firewall* and virtual-machine action endpoints to drive
// the client and resource tests without ever calling the real API.
type firewallMockServer struct {
	mu sync.Mutex

	nextFirewallID int
	nextRuleID     int
	nextActionID   int

	firewalls map[int]*Firewall
	vms       map[int]*VirtualMachine
	actions   map[int]*mockAction

	// force429Once, when true, makes the very next POST /firewall return
	// 429 with Retry-After once, then behaves normally.
	force429Once bool

	server *httptest.Server
}

// mockAction models an async action: created on activate/deactivate, and
// flipped to its terminal state the first time it is polled (so tests
// exercise the "keep polling" path exactly once).
type mockAction struct {
	action     ActionResource
	vmID       int
	firewallID int
	kind       string // "activate" | "deactivate"
	polled     bool
}

func newFirewallMockServer() *firewallMockServer {
	m := &firewallMockServer{
		nextFirewallID: 1,
		nextRuleID:     1,
		nextActionID:   1,
		firewalls:      map[int]*Firewall{},
		vms:            map[int]*VirtualMachine{},
		actions:        map[int]*mockAction{},
	}
	m.server = httptest.NewServer(http.HandlerFunc(m.handle))
	return m
}

func (m *firewallMockServer) Close()      { m.server.Close() }
func (m *firewallMockServer) URL() string { return m.server.URL }

func (m *firewallMockServer) putVM(vm *VirtualMachine) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.vms[vm.ID] = vm
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"message": msg})
}

func (m *firewallMockServer) handle(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()

	path := strings.TrimPrefix(r.URL.Path, "/api/vps/v1/")
	segs := strings.Split(path, "/")

	switch {
	case path == "firewall" && r.Method == http.MethodPost:
		if m.force429Once {
			m.force429Once = false
			w.Header().Set("Retry-After", "0")
			writeErr(w, http.StatusTooManyRequests, "rate limited")
			return
		}
		var body struct {
			Name string `json:"name"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		fw := &Firewall{ID: m.nextFirewallID, Name: body.Name, Rules: []FirewallRule{}}
		m.nextFirewallID++
		m.firewalls[fw.ID] = fw
		writeJSON(w, http.StatusOK, fw)
		return

	case path == "firewall" && r.Method == http.MethodGet:
		list := make([]Firewall, 0, len(m.firewalls))
		for _, fw := range m.firewalls {
			list = append(list, *fw)
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"data": list, "meta": map[string]interface{}{}})
		return

	case len(segs) == 2 && segs[0] == "firewall" && r.Method == http.MethodGet:
		id, _ := strconv.Atoi(segs[1])
		fw, ok := m.firewalls[id]
		if !ok {
			writeErr(w, http.StatusNotFound, "firewall not found")
			return
		}
		writeJSON(w, http.StatusOK, fw)
		return

	case len(segs) == 2 && segs[0] == "firewall" && r.Method == http.MethodDelete:
		id, _ := strconv.Atoi(segs[1])
		if _, ok := m.firewalls[id]; !ok {
			writeErr(w, http.StatusNotFound, "firewall not found")
			return
		}
		delete(m.firewalls, id)
		writeJSON(w, http.StatusOK, map[string]bool{"success": true})
		return

	case len(segs) == 3 && segs[0] == "firewall" && segs[2] == "rules" && r.Method == http.MethodPut:
		id, _ := strconv.Atoi(segs[1])
		fw, ok := m.firewalls[id]
		if !ok {
			writeErr(w, http.StatusNotFound, "firewall not found")
			return
		}
		var body struct {
			Rules []FirewallRuleInput `json:"rules"`
			Sync  bool                `json:"sync"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		rules := make([]FirewallRule, 0, len(body.Rules))
		for _, in := range body.Rules {
			rules = append(rules, FirewallRule{
				ID: m.nextRuleID, Action: "accept",
				Protocol: in.Protocol, Port: in.Port,
				Source: in.Source, SourceDetail: in.SourceDetail,
			})
			m.nextRuleID++
		}
		fw.Rules = rules
		fw.IsSynced = body.Sync
		writeJSON(w, http.StatusOK, fw)
		return

	case len(segs) == 4 && segs[0] == "firewall" && (segs[2] == "activate" || segs[2] == "deactivate") && r.Method == http.MethodPost:
		firewallID, _ := strconv.Atoi(segs[1])
		vmID, _ := strconv.Atoi(segs[3])
		if _, ok := m.firewalls[firewallID]; !ok {
			writeErr(w, http.StatusNotFound, "firewall not found")
			return
		}
		action := ActionResource{ID: m.nextActionID, Name: segs[2], State: "created"}
		m.actions[action.ID] = &mockAction{action: action, vmID: vmID, firewallID: firewallID, kind: segs[2]}
		m.nextActionID++
		writeJSON(w, http.StatusOK, action)
		return

	case len(segs) == 4 && segs[0] == "firewall" && segs[2] == "sync" && r.Method == http.MethodPost:
		firewallID, _ := strconv.Atoi(segs[1])
		vmID, _ := strconv.Atoi(segs[3])
		if _, ok := m.firewalls[firewallID]; !ok {
			writeErr(w, http.StatusNotFound, "firewall not found")
			return
		}
		action := ActionResource{ID: m.nextActionID, Name: "sync", State: "success"}
		m.actions[action.ID] = &mockAction{action: action, vmID: vmID, firewallID: firewallID, kind: "sync", polled: true}
		m.nextActionID++
		writeJSON(w, http.StatusOK, action)
		return

	case len(segs) == 2 && segs[0] == "virtual-machines" && r.Method == http.MethodGet:
		id, _ := strconv.Atoi(segs[1])
		vm, ok := m.vms[id]
		if !ok {
			writeErr(w, http.StatusNotFound, "vm not found")
			return
		}
		writeJSON(w, http.StatusOK, vm)
		return

	case len(segs) == 4 && segs[0] == "virtual-machines" && segs[2] == "actions" && r.Method == http.MethodGet:
		vmID, _ := strconv.Atoi(segs[1])
		actionID, _ := strconv.Atoi(segs[3])
		a, ok := m.actions[actionID]
		if !ok || a.vmID != vmID {
			writeErr(w, http.StatusNotFound, "action not found")
			return
		}
		if !a.polled {
			a.polled = true
			a.action.State = "success"
			vm := m.vms[a.vmID]
			if vm != nil {
				switch a.kind {
				case "activate":
					id := a.firewallID
					vm.FirewallGroupID = &id
				case "deactivate":
					vm.FirewallGroupID = nil
				}
			}
		}
		writeJSON(w, http.StatusOK, a.action)
		return
	}

	writeErr(w, http.StatusNotFound, fmt.Sprintf("unhandled mock route: %s %s", r.Method, r.URL.Path))
}

func newTestClient(baseURL string) *HostingerClient {
	return &HostingerClient{
		BaseURL:    baseURL,
		HTTPClient: http.DefaultClient,
		Token:      "test-token",
	}
}

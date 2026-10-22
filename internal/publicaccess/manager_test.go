package publicaccess

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/lehuunghi/vpn/internal/store"
)

type fixture struct {
	mu                        sync.Mutex
	ip                        string
	records                   []record
	creates, updates, deletes int
	fail                      bool
}

func newFixture(t *testing.T) (*Manager, *fixture) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	f := &fixture{ip: "8.8.8.8", records: []record{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.URL.Path == "/ip" {
			if r.Header.Get("Authorization") != "" {
				t.Error("API token leaked to IP lookup")
			}
			_, _ = fmt.Fprint(w, f.ip)
			return
		}
		if r.Header.Get("Authorization") != "Bearer secret-token-test" {
			t.Error("API authorization missing")
		}
		if f.fail {
			w.WriteHeader(403)
			_, _ = fmt.Fprint(w, "secret-token-test should never be returned to browser")
			return
		}
		base := "/zones/" + strings.Repeat("a", 32) + "/dns_records"
		if !strings.HasPrefix(r.URL.Path, base) {
			t.Errorf("unexpected API path: %s", r.URL.Path)
			w.WriteHeader(404)
			return
		}
		var result any
		switch r.Method {
		case http.MethodGet:
			if r.URL.Query().Get("name.exact") != "office.example.com" || r.URL.Query().Get("per_page") != "100" {
				t.Errorf("unbounded or incorrect record lookup: %s", r.URL.RawQuery)
			}
			result = f.records
		case http.MethodPost, http.MethodPut:
			var in record
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
				t.Error(err)
			}
			if in.Proxied || in.Type != "A" || in.Name != "office.example.com" || in.TTL != 300 {
				t.Errorf("invalid DNS payload: %+v", in)
			}
			if r.Method == http.MethodPost {
				f.creates++
				in.ID = strings.Repeat("b", 32)
			} else {
				f.updates++
				in.ID = strings.TrimPrefix(r.URL.Path, base+"/")
			}
			f.records = []record{in}
			result = in
		case http.MethodDelete:
			f.deletes++
			f.records = []record{}
			result = map[string]string{"id": strings.Repeat("b", 32)}
		default:
			t.Errorf("unexpected method %s", r.Method)
		}
		// Cloudflare documents total_count as the unfiltered zone total. A
		// large zone must not reject a correctly filtered single-host result.
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": result, "result_info": map[string]int{"total_count": 2000}})
	}))
	t.Cleanup(srv.Close)
	m := New(Options{Domain: "example.com", ZoneID: strings.Repeat("a", 32), Token: "secret-token-test"}, st)
	m.apiURL = srv.URL
	m.ipURL = srv.URL + "/ip"
	return m, f
}

func TestDNSLifecycleAndRestart(t *testing.T) {
	m, f := newFixture(t)
	s, err := m.Save(context.Background(), Input{Subdomain: "OFFICE", AutoIPv4: true})
	if err != nil || !s.Registered || s.Hostname != "office.example.com" || s.LastIP != "8.8.8.8" {
		t.Fatalf("create: %+v %v", s, err)
	}
	f.mu.Lock()
	f.ip = "8.8.4.4"
	f.mu.Unlock()
	if err = m.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if m.Status().LastIP != "8.8.4.4" {
		t.Fatal("DDNS did not follow changed WAN IP")
	}
	// Load the same database with a new manager, as after an actual restart.
	restarted := New(m.opt, m.st)
	restarted.apiURL = m.apiURL
	restarted.ipURL = m.ipURL
	if err = restarted.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	creates, updates := f.creates, f.updates
	f.mu.Unlock()
	if creates != 1 || updates != 1 {
		t.Fatalf("restart created duplicate DNS or rewrote unchanged record: %d %d", creates, updates)
	}
	raw, _ := m.st.GetSetting(context.Background(), settingsKey)
	if strings.Contains(raw, "secret-token-test") {
		t.Fatal("API token stored in database")
	}
	if _, err = restarted.Delete(context.Background()); err != nil {
		t.Fatal(err)
	}
	if restarted.Status().Hostname != "" {
		t.Fatal("deleted name remains enabled")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.deletes != 1 || len(f.records) != 0 {
		t.Fatal("owned DNS record was not removed")
	}
}

func TestDNSCannotOverwriteOrDeleteExistingRecords(t *testing.T) {
	for _, kind := range []string{"A", "AAAA", "CNAME"} {
		t.Run(kind, func(t *testing.T) {
			m, f := newFixture(t)
			f.mu.Lock()
			f.records = []record{{ID: strings.Repeat("c", 32), Name: "office.example.com", Type: kind, Content: "8.8.4.4", Comment: "existing application"}}
			f.mu.Unlock()
			if _, err := m.Save(context.Background(), Input{Subdomain: "office", AutoIPv4: true}); err == nil {
				t.Fatal("existing record adopted")
			}
			// Clearing a failed selection must leave unrelated DNS intact.
			if _, err := m.Delete(context.Background()); err != nil {
				t.Fatal(err)
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.creates+f.updates+f.deletes != 0 || len(f.records) != 1 {
				t.Fatal("existing DNS was modified")
			}
		})
	}
}

func TestDNSRecoversCreateWhoseResultWasLost(t *testing.T) {
	m, f := newFixture(t)
	if _, err := m.Save(context.Background(), Input{Subdomain: "office", AutoIPv4: true}); err != nil {
		t.Fatal(err)
	}
	// State before a create response was received: same persisted owner/name,
	// but no record ID. A retry must recover that record, not adopt by name alone.
	m.mu.Lock()
	m.state.RecordID = ""
	if err := m.persist(context.Background()); err != nil {
		t.Fatal(err)
	}
	m.mu.Unlock()
	restarted := New(m.opt, m.st)
	restarted.apiURL = m.apiURL
	restarted.ipURL = m.ipURL
	if err := restarted.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !restarted.Status().Registered {
		t.Fatal("did not recover owned record")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.creates != 1 {
		t.Fatal("retry created a duplicate record")
	}
}

func TestDNSValidationAndSecretRedaction(t *testing.T) {
	m, f := newFixture(t)
	for _, label := range []string{"", "../zones/other", "office.example.com", "-vpn", "vpn-", "*", strings.Repeat("a", 64)} {
		if _, err := m.Save(context.Background(), Input{Subdomain: label, AutoIPv4: true}); err == nil {
			t.Errorf("invalid subdomain accepted: %q", label)
		}
	}
	for _, ip := range []string{"127.0.0.1", "192.168.1.100", "100.64.1.1", "169.254.1.1", "10.0.0.1", "::1", "0.0.0.0", "203.0.113.1", "224.0.0.1", "255.255.255.255"} {
		if _, err := m.Save(context.Background(), Input{Subdomain: "office", IPv4: ip}); err == nil {
			t.Errorf("non-public IPv4 accepted: %s", ip)
		}
	}
	f.mu.Lock()
	f.fail = true
	f.mu.Unlock()
	_, err := m.Save(context.Background(), Input{Subdomain: "office", IPv4: "8.8.8.8"})
	if err == nil || strings.Contains(err.Error(), "secret-token-test") {
		t.Fatalf("unsafe provider error: %v", err)
	}
	b, _ := json.Marshal(m.Status())
	if strings.Contains(string(b), "secret-token-test") {
		t.Fatal("token leaked in status")
	}
}

func TestDNSRefusesChangedDomain(t *testing.T) {
	m, _ := newFixture(t)
	if _, err := m.Save(context.Background(), Input{Subdomain: "office", AutoIPv4: true}); err != nil {
		t.Fatal(err)
	}
	opt := m.opt
	opt.Domain = "other.example.com"
	restarted := New(opt, m.st)
	if restarted.Status().Available {
		t.Fatal("changed parent domain silently took over a different record")
	}
	if err := restarted.Sync(context.Background()); err == nil {
		t.Fatal("sync permitted with changed domain")
	}
}

func TestDNSRejectsIgnoredNameFilter(t *testing.T) {
	m, f := newFixture(t)
	f.mu.Lock()
	f.records = []record{{ID: strings.Repeat("c", 32), Name: "another.example.com", Type: "A", Content: "8.8.8.8"}}
	f.mu.Unlock()
	if _, err := m.Save(context.Background(), Input{Subdomain: "office", AutoIPv4: true}); err == nil {
		t.Fatal("DNS provider returned a different name without an error")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.creates+f.updates+f.deletes != 0 {
		t.Fatal("DNS mutated after an incorrect lookup")
	}
}

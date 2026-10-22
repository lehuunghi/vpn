package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/lehuunghi/vpn/internal/store"
)

func testContainer() containerInfo {
	var c containerInfo
	_ = json.Unmarshal([]byte(`{"Id":"0123456789abcdef","Name":"/vpn20","Image":"sha256:old","Config":{"Hostname":"0123456789ab","Image":"ghcr.io/lehuunghi/vpn20:latest","Env":["VPN20_DATA_DIR=/data"],"Labels":{"custom":"keep"},"Volumes":{"/data":{}}},"HostConfig":{"Binds":["/srv/config:/config:ro,rshared"],"Mounts":[{"Type":"volume","Source":"database","Target":"/database","VolumeOptions":{"NoCopy":true}}],"RestartPolicy":{"Name":"unless-stopped"},"CapAdd":["NET_ADMIN"],"PortBindings":{"51820/udp":[{"HostPort":"51820"}]},"Sysctls":{"net.ipv4.ip_forward":"1"}},"Mounts":[{"Type":"bind","Source":"/srv/config","Destination":"/config","RW":false},{"Type":"volume","Name":"database","Destination":"/database","RW":true},{"Type":"volume","Name":"existing-anonymous-data","Destination":"/data","RW":true}],"State":{"Running":true,"Health":{"Status":"healthy"}},"NetworkSettings":{"Networks":{"vpn_default":{"NetworkID":"network","Aliases":["vpn20","0123456789ab"]}}}}`), &c)
	return c
}

func TestDockerReplacementPreservesVolumesPortsAndOptions(t *testing.T) {
	old := testContainer()
	config, host, network, err := replacementConfig(old, Image+"@sha256:"+strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"Binds", "RestartPolicy", "CapAdd", "PortBindings", "Sysctls"} {
		if !reflect.DeepEqual(host[key], old.HostConfig[key]) {
			t.Fatalf("changed %s", key)
		}
	}
	if !reflect.DeepEqual(config["Env"], old.Config["Env"]) {
		t.Fatal("lost environment")
	}
	if _, ok := config["Hostname"]; ok {
		t.Fatal("retained generated old hostname")
	}
	var mounts []map[string]any
	if err = json.Unmarshal(host["Mounts"], &mounts); err != nil {
		t.Fatal(err)
	}
	if len(mounts) != 2 || mounts[0]["VolumeOptions"] == nil || mounts[1]["Source"] != "existing-anonymous-data" {
		t.Fatalf("lost volume identity/options: %+v", mounts)
	}
	networkJSON, _ := json.Marshal(network)
	if strings.Contains(string(networkJSON), "0123456789ab") {
		t.Fatal("old container network alias leaked")
	}
}

func TestDockerReplacementPreservesAllocatedHostPorts(t *testing.T) {
	old := testContainer()
	old.HostConfig["PortBindings"] = mustJSON(map[string][]portBinding{"51821/tcp": {{HostIP: "127.0.0.1", HostPort: "0"}, {HostIP: "127.0.0.1", HostPort: "51821"}}})
	old.NetworkSettings.Ports = map[string][]portBinding{"51821/tcp": {{HostIP: "127.0.0.1", HostPort: "51821"}, {HostIP: "127.0.0.1", HostPort: "32781"}}}
	_, host, _, err := replacementConfig(old, Image+":latest")
	if err != nil {
		t.Fatal(err)
	}
	var ports map[string][]portBinding
	if err = json.Unmarshal(host["PortBindings"], &ports); err != nil {
		t.Fatal(err)
	}
	if bindings := ports["51821/tcp"]; bindings[0].HostPort != "32781" || bindings[1].HostPort != "51821" {
		t.Fatalf("changed assigned/fixed ports: %+v", bindings)
	}
	old.NetworkSettings.Ports = nil
	if _, _, _, err = replacementConfig(old, Image+":latest"); err == nil {
		t.Fatal("accepted unresolvable allocated port")
	}
}

func TestDockerUpdateAndRollback(t *testing.T) {
	for _, healthy := range []bool{true, false} {
		t.Run(fmt.Sprint(healthy), func(t *testing.T) {
			dir := t.TempDir()
			id := strings.Repeat("a", 24)
			root := filepath.Join(dir, "updates")
			_ = os.MkdirAll(filepath.Join(root, id), 0700)
			db := filepath.Join(dir, "vpn20.db")
			st, err := store.Open(db)
			if err != nil {
				t.Fatal(err)
			}
			_ = st.SetSetting(context.Background(), "marker", "before")
			_ = st.Close()
			old := testContainer()
			oldID := old.ID
			name := "/vpn20"
			running := true
			restored := false
			removedNew := false
			var created map[string]json.RawMessage
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/images/"):
					_ = json.NewEncoder(w).Encode(dockerImage{ID: "sha256:new"})
				case r.Method == "GET" && r.URL.Path == "/containers/"+oldID+"/json":
					c := old
					c.Name = name
					c.State.Running = running
					_ = json.NewEncoder(w).Encode(c)
				case r.Method == "POST" && r.URL.Path == "/containers/"+oldID+"/update":
					w.WriteHeader(200)
				case r.Method == "POST" && r.URL.Path == "/containers/"+oldID+"/stop":
					running = false
					w.WriteHeader(204)
				case r.Method == "POST" && r.URL.Path == "/containers/"+oldID+"/rename":
					name = "/" + r.URL.Query().Get("name")
					if name == "/vpn20" {
						restored = true
					}
					w.WriteHeader(204)
				case r.Method == "POST" && r.URL.Path == "/containers/create":
					_ = json.NewDecoder(r.Body).Decode(&created)
					_ = json.NewEncoder(w).Encode(map[string]string{"Id": "new"})
				case r.Method == "POST" && r.URL.Path == "/containers/new/start":
					s, _ := store.Open(db)
					_ = s.SetSetting(context.Background(), "marker", "new-version")
					_ = s.Close()
					w.WriteHeader(204)
				case r.Method == "GET" && r.URL.Path == "/containers/new/json":
					status := "unhealthy"
					if healthy {
						status = "healthy"
					}
					fmt.Fprintf(w, `{"Image":"sha256:new","State":{"Running":true,"Health":{"Status":%q}}}`, status)
				case r.Method == "DELETE" && r.URL.Path == "/containers/new":
					if r.URL.Query().Get("v") != "0" {
						t.Error("update tried to delete data volumes")
					}
					removedNew = true
					w.WriteHeader(204)
				case r.Method == "DELETE" && r.URL.Path == "/containers/"+oldID:
					if r.URL.Query().Get("v") != "0" {
						t.Error("deleted old data volumes")
					}
					w.WriteHeader(204)
				case r.Method == "POST" && r.URL.Path == "/containers/"+oldID+"/start":
					running = true
					w.WriteHeader(204)
				default:
					t.Errorf("unexpected Docker operation: %s %s", r.Method, r.URL)
					http.Error(w, "unexpected operation", 500)
				}
			}))
			defer server.Close()
			base, _ := url.Parse(server.URL)
			d := &dockerAPI{client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				clone := r.Clone(r.Context())
				u := *r.URL
				u.Scheme = base.Scheme
				u.Host = base.Host
				clone.URL = &u
				return http.DefaultTransport.RoundTrip(clone)
			})}}
			p := dockerPlan{Job: Job{ID: id, Version: "1.1.0"}, DataDir: dir, DBPath: db, Target: oldID, Name: "vpn20", ImageID: "sha256:new", ImageRef: Image + "@sha256:" + strings.Repeat("a", 64)}
			if err = applyContainer(context.Background(), d, p); err != nil {
				t.Fatal(err)
			}
			job := readJob(root)
			want := "complete"
			if !healthy {
				want = "rolled_back"
			}
			if job.Phase != want {
				t.Fatalf("job phase: %+v", job)
			}
			if created == nil {
				t.Fatal("no new container")
			}
			if !healthy && (!restored || !removedNew || !running) {
				t.Fatal("did not restore old container")
			}
			st, err = store.Open(db)
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			marker, _ := st.GetSetting(context.Background(), "marker")
			if !healthy && marker != "before" {
				t.Fatalf("did not restore pre-migration database: %s", marker)
			}
		})
	}
}

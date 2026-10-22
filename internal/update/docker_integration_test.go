//go:build linux

package update

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lehuunghi/vpn/internal/store"
)

// Runs against the actual Docker daemon in CI, with isolated test containers
// and a private bind mount. No existing installation is selected or changed.
func TestDockerDaemonUpdate(t *testing.T) {
	oldImage := os.Getenv("VPN20_TEST_DOCKER_OLD_IMAGE")
	if oldImage == "" {
		t.Skip("real Docker upgrade test runs in CI")
	}
	if os.Geteuid() != 0 {
		t.Fatal("Docker integration needs root for private root-owned data files")
	}
	for _, healthy := range []bool{true, false} {
		t.Run(strconv.FormatBool(healthy), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			d := dockerClient("/var/run/docker.sock")
			image := os.Getenv("VPN20_TEST_DOCKER_NEW_IMAGE")
			if !healthy {
				image = os.Getenv("VPN20_TEST_DOCKER_BAD_IMAGE")
			}
			var img dockerImage
			if err := d.call(ctx, "GET", "/images/"+image+"/json", nil, &img); err != nil {
				t.Fatal(err)
			}
			data := t.TempDir()
			id := strings.Repeat("b", 23) + map[bool]string{true: "1", false: "2"}[healthy]
			if err := os.Chmod(filepath.Dir(data), 0755); err != nil {
				t.Fatal(err)
			}
			name := "vpn20-update-integration-" + id
			ref := Image + "@" + img.ID
			socket := registryAliasProxy(t, ref, image)
			var created struct {
				ID string `json:"Id"`
			}
			body := map[string]any{"Image": oldImage, "Env": []string{"VPN20_BACKEND=mock", "VPN20_MANAGE_FIREWALL=false", "VPN20_MANAGE_SYSCTL=false", "VPN20_HTTP_LISTEN=0.0.0.0:51821", "VPN20_DATA_DIR=" + data}, "Labels": map[string]string{"com.vpn20.integration": id}, "HostConfig": map[string]any{"Binds": []string{data + ":" + data, socket + ":" + socket}, "RestartPolicy": map[string]string{"Name": "unless-stopped"}, "PortBindings": map[string]any{"51821/tcp": []any{map[string]string{"HostIp": "127.0.0.1", "HostPort": "0"}}}}}
			if err := d.call(ctx, "POST", "/containers/create?name="+name, body, &created); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				var containers []struct {
					ID string `json:"Id"`
				}
				filter := `{"label":["com.vpn20.integration=` + id + `"]}`
				if err := d.call(cleanup, "GET", "/containers/json?all=1&filters="+url.QueryEscape(filter), nil, &containers); err != nil {
					t.Error(err)
					return
				}
				for _, c := range containers {
					if err := d.call(cleanup, "DELETE", "/containers/"+c.ID+"?force=1&v=0", nil, nil); err != nil {
						t.Error(err)
					}
				}
			})
			if err := d.call(ctx, "POST", "/containers/"+created.ID+"/start", nil, nil); err != nil {
				t.Fatal(err)
			}
			old, err := d.inspect(ctx, created.ID)
			if err != nil {
				t.Fatal(err)
			}
			if err = waitContainer(ctx, d, old.ID, old.Image); err != nil {
				t.Fatal(err)
			}
			db := filepath.Join(data, "vpn20.db")
			s, err := store.Open(db)
			if err != nil {
				t.Fatal(err)
			}
			_ = s.SetSetting(ctx, "update-marker", "retained")
			_ = s.Close()
			root := filepath.Join(data, "updates")
			if err = os.MkdirAll(root, 0700); err != nil {
				t.Fatal(err)
			}
			m := New(Options{Mode: "docker", DataDir: data, DBPath: db, DockerSocket: socket, DockerContainer: name})
			if err = os.WriteFile(filepath.Join(root, "active"), nil, 0600); err != nil {
				t.Fatal(err)
			}
			job := Job{ID: id, Version: "integration"}
			if err = m.prepareDocker(ctx, &job, &Candidate{ImageDigest: img.ID}); !errors.Is(err, errHandedOff) {
				t.Fatal(err)
			}
			for exists(filepath.Join(root, "active")) {
				select {
				case <-ctx.Done():
					t.Fatalf("detached helper did not complete: %+v", readJob(root))
				case <-time.After(200 * time.Millisecond):
				}
			}
			job = readJob(root)
			want := "complete"
			if !healthy {
				want = "rolled_back"
			}
			if job.Phase != want {
				t.Fatalf("unexpected state: %+v", job)
			}
			current, err := d.inspect(ctx, name)
			if err != nil {
				t.Fatal(err)
			}
			if healthy && current.Image != img.ID {
				t.Fatal("did not start the new image")
			}
			if !healthy && current.Image != old.Image {
				t.Fatal("did not restore previous image")
			}
			before, after := old.NetworkSettings.Ports["51821/tcp"], current.NetworkSettings.Ports["51821/tcp"]
			if len(before) != 1 || len(after) != 1 || before[0] != after[0] {
				t.Fatalf("changed reachable host port: before=%+v after=%+v", before, after)
			}
			s, err = store.Open(db)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			marker, err := s.GetSetting(ctx, "update-marker")
			if err != nil || marker != "retained" {
				t.Fatalf("lost database contents: %q %v", marker, err)
			}
			t.Log("PASS: real detached Docker helper, image replacement, retained data/host port and health/rollback", healthy)
		})
	}
}

// Only registry resolution is simulated: local fixture images are addressed
// through a synthetic GHCR digest. All container operations, the isolated
// helper process and SQLite snapshot/rollback use the actual Docker daemon.
func registryAliasProxy(t *testing.T, ref, localImage string) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "vpn20-update-socket-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "docker.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	target, _ := url.Parse("http://docker")
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", "/var/run/docker.sock")
	}}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/images/create" && r.URL.Query().Get("fromImage") == ref {
			_, _ = io.WriteString(w, "{\"status\":\"local integration image present\"}\n")
			return
		}
		if r.URL.Path == "/images/"+ref+"/json" {
			r.URL.Path = "/images/" + localImage + "/json"
			r.URL.RawPath = ""
		}
		if r.Method == "POST" && r.URL.Path == "/containers/create" {
			var body map[string]json.RawMessage
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			_ = r.Body.Close()
			var image string
			_ = json.Unmarshal(body["Image"], &image)
			if image == ref {
				body["Image"] = mustJSON(localImage)
			}
			b, _ := json.Marshal(body)
			r.Body = io.NopCloser(bytes.NewReader(b))
			r.ContentLength = int64(len(b))
		}
		proxy.ServeHTTP(w, r)
	})}
	t.Cleanup(func() { _ = server.Close() })
	go func() { _ = server.Serve(listener) }()
	return socket
}

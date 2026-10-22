//go:build linux

package update

import (
	"context"
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
			var created struct {
				ID string `json:"Id"`
			}
			body := map[string]any{"Image": oldImage, "Env": []string{"VPN20_BACKEND=mock", "VPN20_MANAGE_FIREWALL=false", "VPN20_MANAGE_SYSCTL=false", "VPN20_HTTP_LISTEN=0.0.0.0:51821"}, "Labels": map[string]string{"com.vpn20.integration": id}, "HostConfig": map[string]any{"Binds": []string{data + ":/data"}, "RestartPolicy": map[string]string{"Name": "unless-stopped"}, "PortBindings": map[string]any{"51821/tcp": []any{map[string]string{"HostIp": "127.0.0.1", "HostPort": "0"}}}}}
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
			if err = os.MkdirAll(filepath.Join(root, id), 0700); err != nil {
				t.Fatal(err)
			}
			plan := dockerPlan{Job: Job{ID: id, Version: "integration"}, DataDir: data, DBPath: db, Target: old.ID, Name: name, ImageID: img.ID, ImageRef: image}
			if err = applyContainer(ctx, d, plan); err != nil {
				t.Fatal(err)
			}
			job := readJob(root)
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
			t.Log("PASS: real Docker image replacement, private data preservation and health/rollback", healthy)
		})
	}
}

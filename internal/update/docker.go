package update

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type dockerAPI struct{ client *http.Client }

func dockerClient(socket string) *dockerAPI {
	return &dockerAPI{client: &http.Client{Timeout: 6 * time.Minute, Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}}}
}

func (d *dockerAPI) call(ctx context.Context, method, path string, body, out any) error {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://docker"+path, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return fmt.Errorf("Docker %s: HTTP %d: %s", path, res.StatusCode, b)
	}
	if out != nil {
		return json.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(out)
	}
	_, err = io.Copy(io.Discard, io.LimitReader(res.Body, 8<<20))
	return err
}

func (d *dockerAPI) pull(ctx context.Context, ref string) error {
	req, _ := http.NewRequestWithContext(ctx, "POST", "http://docker/images/create?fromImage="+url.QueryEscape(ref), nil)
	res, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("Docker image pull HTTP %d", res.StatusCode)
	}
	dec := json.NewDecoder(io.LimitReader(res.Body, 16<<20))
	for {
		var p struct {
			Error string `json:"error"`
		}
		err = dec.Decode(&p)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if p.Error != "" {
			return errors.New(p.Error)
		}
	}
}

type dockerImage struct {
	ID          string `json:"Id"`
	RepoDigests []string
	Config      struct{ Labels map[string]string }
}
type dockerMount struct {
	Type, Name, Source, Destination string
	RW                              bool
}
type portBinding struct {
	HostIP   string `json:"HostIp"`
	HostPort string `json:"HostPort"`
}
type containerInfo struct {
	ID          string `json:"Id"`
	Name, Image string
	Config      map[string]json.RawMessage
	HostConfig  map[string]json.RawMessage
	Mounts      []dockerMount
	State       struct {
		Running bool
		Health  *struct{ Status string }
	}
	NetworkSettings struct {
		Ports    map[string][]portBinding
		Networks map[string]struct {
			NetworkID  string
			Aliases    []string
			IPAMConfig json.RawMessage
			DriverOpts map[string]string
		}
	}
}

func containerTarget(opts Options) string {
	if opts.DockerContainer != "" {
		return opts.DockerContainer
	}
	name, _ := os.Hostname()
	return name
}

func (d *dockerAPI) inspect(ctx context.Context, target string) (containerInfo, error) {
	var c containerInfo
	err := d.call(ctx, "GET", "/containers/"+url.PathEscape(target)+"/json", nil, &c)
	return c, err
}

func (m *Manager) dockerLatest(ctx context.Context) (*Candidate, error) {
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("registry redirect refused") }}
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://ghcr.io/token?service=ghcr.io&scope=repository:lehuunghi/vpn20:pull", nil)
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	var token struct {
		Token string `json:"token"`
	}
	err = json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&token)
	_ = res.Body.Close()
	if res.StatusCode != 200 || err != nil || token.Token == "" {
		return nil, errors.New("Không đọc được GHCR; cần đặt package lehuunghi/vpn20 ở chế độ public.")
	}
	req, _ = http.NewRequestWithContext(ctx, "HEAD", "https://ghcr.io/v2/lehuunghi/vpn20/manifests/latest", nil)
	req.Header.Set("Authorization", "Bearer "+token.Token)
	req.Header.Set("Accept", "application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json")
	res, err = client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	digest := res.Header.Get("Docker-Content-Digest")
	if res.StatusCode != 200 || !validImageDigest(digest) {
		return nil, errors.New("GHCR không cung cấp digest hợp lệ cho image latest.")
	}
	d := dockerClient(m.opts.DockerSocket)
	old, err := d.inspect(ctx, containerTarget(m.opts))
	if err != nil {
		return nil, err
	}
	var img dockerImage
	if err = d.call(ctx, "GET", "/images/"+url.PathEscape(old.Image)+"/json", nil, &img); err != nil {
		return nil, err
	}
	for _, ref := range img.RepoDigests {
		if ref == Image+"@"+digest {
			return nil, nil
		}
	}
	return &Candidate{Version: "latest · " + digest[7:19], URL: "https://github.com/" + Repository + "/pkgs/container/vpn20", ImageDigest: digest}, nil
}

func validImageDigest(s string) bool {
	if len(s) != 71 || !strings.HasPrefix(s, "sha256:") {
		return false
	}
	for _, c := range s[7:] {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return false
		}
	}
	return true
}

type dockerPlan struct {
	Job                                              Job
	DataDir, DBPath, Target, Name, ImageID, ImageRef string
}

func (m *Manager) prepareDocker(ctx context.Context, j *Job, c *Candidate) error {
	if !pathInside(m.opts.DataDir, m.opts.DBPath) {
		return errors.New("Docker updater yêu cầu database nằm trong volume dữ liệu VPN20.")
	}
	d := dockerClient(m.opts.DockerSocket)
	old, err := d.inspect(ctx, containerTarget(m.opts))
	if err != nil {
		return err
	}
	var labels map[string]string
	_ = json.Unmarshal(old.Config["Labels"], &labels)
	if old.ID == "" || !old.State.Running || labels["org.opencontainers.image.source"] != "https://github.com/"+Repository {
		return errors.New("Chỉ được cập nhật container VPN20 đang chạy từ repo này.")
	}
	if !validImageDigest(c.ImageDigest) {
		return errors.New("invalid image digest")
	}
	ref := Image + "@" + c.ImageDigest
	if err = d.pull(ctx, ref); err != nil {
		return err
	}
	var img dockerImage
	if err = d.call(ctx, "GET", "/images/"+url.PathEscape(ref)+"/json", nil, &img); err != nil {
		return err
	}
	if img.ID == old.Image {
		return nil
	}
	if img.Config.Labels["org.opencontainers.image.source"] != "https://github.com/"+Repository {
		return errors.New("Image mới không thuộc repo VPN20.")
	}
	if v := img.Config.Labels["org.opencontainers.image.version"]; v != "" {
		j.Version = v
	}
	dir := filepath.Join(m.root, j.ID)
	if err = os.Mkdir(dir, 0700); err != nil {
		return err
	}
	p := dockerPlan{Job: *j, DataDir: m.opts.DataDir, DBPath: m.opts.DBPath, Target: old.ID, Name: strings.TrimPrefix(old.Name, "/"), ImageID: img.ID, ImageRef: ref}
	path := filepath.Join(dir, "docker-plan.json")
	if err = writeJSON(path, p); err != nil {
		return err
	}
	var dataMount, socketMount map[string]any
	for _, mount := range old.Mounts {
		if mount.Destination == m.opts.DataDir {
			source := mount.Source
			if mount.Type == "volume" {
				source = mount.Name
			}
			if mount.RW {
				dataMount = map[string]any{"Type": mount.Type, "Source": source, "Target": mount.Destination}
			}
		}
		if mount.Destination == m.opts.DockerSocket {
			socketMount = map[string]any{"Type": "bind", "Source": mount.Source, "Target": "/var/run/docker.sock"}
		}
	}
	if dataMount == nil || socketMount == nil {
		return errors.New("Gắn riêng volume dữ liệu và Docker socket bằng docker-compose.update.yml trước khi cập nhật.")
	}
	j.Phase = "installing"
	j.Message = "Đang sao lưu và thay container. Các cổng, volume, cấu hình mạng và thiết bị sẽ được giữ lại."
	_ = writeJob(m.root, *j)
	body := map[string]any{"Image": old.Image, "User": "0:0", "Entrypoint": []string{"/usr/local/bin/vpn20"}, "Cmd": []string{"docker-apply-update", path}, "Labels": map[string]string{"com.vpn20.update.target": old.ID}, "HostConfig": map[string]any{"AutoRemove": true, "NetworkMode": "none", "ReadonlyRootfs": true, "CapDrop": []string{"ALL"}, "SecurityOpt": []string{"no-new-privileges:true"}, "Mounts": []any{dataMount, socketMount}, "LogConfig": map[string]any{"Type": "json-file", "Config": map[string]string{"max-size": "5m", "max-file": "1"}}}}
	var created struct {
		ID string `json:"Id"`
	}
	if err = d.call(ctx, "POST", "/containers/create?name=vpn20-update-"+j.ID, body, &created); err != nil {
		return err
	}
	if err = d.call(ctx, "POST", "/containers/"+created.ID+"/start", nil, nil); err != nil {
		_ = d.call(ctx, "DELETE", "/containers/"+created.ID+"?force=1", nil, nil)
		return err
	}
	return errHandedOff
}

// DockerApply runs in an ephemeral, networkless helper using the old image.
// It only mounts the daemon socket and the existing private data volume.
func DockerApply(path string) (result error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var p dockerPlan
	if err = json.Unmarshal(b, &p); err != nil {
		return err
	}
	root := filepath.Join(p.DataDir, "updates")
	if len(p.Job.ID) != 24 || strings.ContainsAny(p.Job.ID, "/\\.") || filepath.Clean(path) != filepath.Join(root, p.Job.ID, "docker-plan.json") || !pathInside(p.DataDir, p.DBPath) || !strings.HasPrefix(p.ImageRef, Image+"@") || !validImageDigest(strings.TrimPrefix(p.ImageRef, Image+"@")) {
		return errors.New("invalid Docker update plan")
	}
	defer func() {
		if result != nil {
			p.Job.Phase = "failed"
			p.Job.Message = result.Error()
			_ = writeJob(root, p.Job)
		}
		_ = os.Remove(filepath.Join(root, "active"))
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	d := dockerClient("/var/run/docker.sock")
	return applyContainer(ctx, d, p)
}

func applyContainer(ctx context.Context, d *dockerAPI, p dockerPlan) (result error) {
	root := filepath.Join(p.DataDir, "updates")
	dir := filepath.Join(root, p.Job.ID)
	old, err := d.inspect(ctx, p.Target)
	if err != nil {
		return err
	}
	if strings.TrimPrefix(old.Name, "/") != p.Name || !old.State.Running {
		return errors.New("Container đã thay đổi; không áp dụng bản cập nhật cũ.")
	}
	var image dockerImage
	if err = d.call(ctx, "GET", "/images/"+url.PathEscape(p.ImageRef)+"/json", nil, &image); err != nil {
		return err
	}
	if image.ID != p.ImageID {
		return errors.New("Pinned Docker image changed before installation.")
	}
	// Preserve every existing setting; only the image and generated hostname
	// change. Both named and anonymous volumes must remain the same volumes.
	config, host, network, err := replacementConfig(old, p.ImageRef)
	if err != nil {
		return err
	}
	var labels map[string]string
	_ = json.Unmarshal(config["Labels"], &labels)
	if labels == nil {
		labels = map[string]string{}
	}
	for key, value := range image.Config.Labels {
		if strings.HasPrefix(key, "org.opencontainers.image.") {
			labels[key] = value
		}
	}
	config["Labels"] = mustJSON(labels)
	var policy any
	if err = json.Unmarshal(old.HostConfig["RestartPolicy"], &policy); err != nil {
		return err
	}
	if err = d.call(ctx, "POST", "/containers/"+old.ID+"/update", map[string]any{"RestartPolicy": map[string]string{"Name": "no"}}, nil); err != nil {
		return err
	}
	stopped := false
	renamed := false
	newID := ""
	backupReady := false
	committed := false
	defer func() {
		if result == nil || committed {
			return
		}
		rollbackCtx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		var rollbackErr error
		if newID != "" {
			rollbackErr = d.call(rollbackCtx, "DELETE", "/containers/"+newID+"?force=1&v=0", nil, nil)
		}
		if rollbackErr == nil && backupReady {
			rollbackErr = restoreDatabase(p.DBPath, filepath.Join(dir, "backup", "vpn20.db"))
		}
		if rollbackErr == nil && renamed {
			rollbackErr = d.call(rollbackCtx, "POST", "/containers/"+old.ID+"/rename?name="+url.QueryEscape(p.Name), nil, nil)
		}
		if rollbackErr == nil {
			rollbackErr = d.call(rollbackCtx, "POST", "/containers/"+old.ID+"/update", map[string]any{"RestartPolicy": policy}, nil)
		}
		if rollbackErr == nil && stopped {
			rollbackErr = d.call(rollbackCtx, "POST", "/containers/"+old.ID+"/start", nil, nil)
		}
		if rollbackErr == nil && stopped {
			rollbackErr = waitContainer(rollbackCtx, d, old.ID, old.Image)
		}
		if rollbackErr != nil {
			result = fmt.Errorf("%v; rollback failed: %v; previous container: %s", result, rollbackErr, old.ID)
			return
		}
		p.Job.Phase = "rolled_back"
		p.Job.Message = "Đã khôi phục container và dữ liệu trước cập nhật: " + result.Error()
		_ = writeJob(root, p.Job)
		result = nil // persistent rolled_back state contains the original failure.
	}()
	p.Job.Phase = "backing_up"
	p.Job.Message = "Đang dừng container và sao lưu dữ liệu nhất quán."
	_ = writeJob(root, p.Job)
	if err = d.call(ctx, "POST", "/containers/"+old.ID+"/stop?t=90", nil, nil); err != nil {
		return err
	}
	stopped = true
	backup := filepath.Join(dir, "backup")
	if err = os.MkdirAll(backup, 0700); err != nil {
		return err
	}
	if err = writeJSON(filepath.Join(backup, "container.json"), old); err != nil {
		return err
	}
	if err = backupDatabase(ctx, p.DBPath, filepath.Join(backup, "vpn20.db")); err != nil {
		return err
	}
	backupReady = true
	if err = d.call(ctx, "POST", "/containers/"+old.ID+"/rename?name="+url.QueryEscape(p.Name+"-before-"+p.Job.ID[:8]), nil, nil); err != nil {
		return err
	}
	renamed = true
	config["HostConfig"] = mustJSON(host)
	config["NetworkingConfig"] = mustJSON(network)
	var created struct {
		ID string `json:"Id"`
	}
	if err = d.call(ctx, "POST", "/containers/create?name="+url.QueryEscape(p.Name), config, &created); err != nil {
		return err
	}
	newID = created.ID
	if err = d.call(ctx, "POST", "/containers/"+newID+"/start", nil, nil); err != nil {
		return err
	}
	p.Job.Phase = "restarting"
	p.Job.Message = "Đang kiểm tra phiên bản mới và kết nối lại giao diện."
	_ = writeJob(root, p.Job)
	if err = waitContainer(ctx, d, newID, p.ImageID); err != nil {
		return err
	}
	committed = true
	// Leaving the exited predecessor with Compose's immutable service labels
	// makes the next `compose up` see two instances. Remove only the container,
	// retaining its image, data volumes, inspect metadata and DB snapshot.
	cleanupErr := d.call(ctx, "DELETE", "/containers/"+old.ID+"?v=0", nil, nil)
	p.Job.Phase = "complete"
	p.Job.Message = "Đã cập nhật image, kiểm tra dịch vụ và giữ nguyên dữ liệu; bản image, cấu hình và DB trước được giữ để khôi phục."
	if cleanupErr != nil {
		p.Job.Message += " Không xóa được container cũ đã dừng: " + cleanupErr.Error()
	}
	return writeJob(root, p.Job)
}

func mustJSON(v any) json.RawMessage { b, _ := json.Marshal(v); return b }

func replacementConfig(old containerInfo, image string) (map[string]json.RawMessage, map[string]json.RawMessage, map[string]any, error) {
	config := map[string]json.RawMessage{}
	for k, v := range old.Config {
		config[k] = v
	}
	config["Image"] = mustJSON(image)
	var hostname string
	_ = json.Unmarshal(config["Hostname"], &hostname)
	if hostname == old.ID[:min(12, len(old.ID))] {
		delete(config, "Hostname")
	}
	host := map[string]json.RawMessage{}
	for k, v := range old.HostConfig {
		host[k] = v
	}
	// Docker keeps an automatically allocated host port as "" or "0" in
	// HostConfig. Reusing that request would assign another port at upgrade.
	var ports map[string][]portBinding
	if raw := host["PortBindings"]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &ports); err != nil {
			return nil, nil, nil, err
		}
		changed := false
		for name, bindings := range ports {
			used := map[int]bool{}
			for _, binding := range bindings {
				if binding.HostPort == "" || binding.HostPort == "0" {
					continue
				}
				for j, active := range old.NetworkSettings.Ports[name] {
					if binding.HostPort == active.HostPort && (binding.HostIP == "" || binding.HostIP == active.HostIP) {
						used[j] = true
					}
				}
			}
			for i, binding := range bindings {
				if binding.HostPort != "" && binding.HostPort != "0" {
					continue
				}
				resolved := false
				for j, active := range old.NetworkSettings.Ports[name] {
					if used[j] || (binding.HostIP != "" && binding.HostIP != active.HostIP) || active.HostPort == "" || active.HostPort == "0" {
						continue
					}
					bindings[i].HostPort = active.HostPort
					used[j], resolved, changed = true, true, true
					break
				}
				if !resolved {
					return nil, nil, nil, fmt.Errorf("cannot preserve allocated Docker host port for %s", name)
				}
			}
		}
		if changed {
			host["PortBindings"] = mustJSON(ports)
		}
	}
	// Keep declared mount options, bind propagation, tmpfs and volume subpaths.
	// Add only anonymous volumes missing from HostConfig, using their existing
	// volume names instead of letting Docker silently allocate empty volumes.
	var mounts []map[string]any
	if raw := host["Mounts"]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &mounts); err != nil {
			return nil, nil, nil, err
		}
	}
	covered := map[string]bool{}
	for _, m := range mounts {
		if target, ok := m["Target"].(string); ok {
			covered[target] = true
		}
	}
	var binds []string
	if raw := host["Binds"]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &binds); err != nil {
			return nil, nil, nil, err
		}
	}
	for _, bind := range binds {
		parts := strings.Split(bind, ":")
		if len(parts) >= 2 {
			target := parts[len(parts)-1]
			if !strings.HasPrefix(target, "/") {
				target = parts[len(parts)-2]
			}
			covered[target] = true
		}
	}
	for _, m := range old.Mounts {
		if covered[m.Destination] || (m.Type != "volume" && m.Type != "bind") {
			continue
		}
		source := m.Source
		if m.Type == "volume" {
			source = m.Name
		}
		mounts = append(mounts, map[string]any{"Type": m.Type, "Source": source, "Target": m.Destination, "ReadOnly": !m.RW})
	}
	host["Mounts"] = mustJSON(mounts)
	endpoints := map[string]any{}
	for name, n := range old.NetworkSettings.Networks {
		e := map[string]any{"NetworkID": n.NetworkID}
		if name != "bridge" && name != "host" && name != "none" {
			var aliases []string
			for _, a := range n.Aliases {
				if a != old.ID && a != old.ID[:min(12, len(old.ID))] {
					aliases = append(aliases, a)
				}
			}
			e["Aliases"] = aliases
		}
		if len(n.IPAMConfig) > 0 && string(n.IPAMConfig) != "null" {
			return nil, nil, nil, errors.New("Container dùng IP tĩnh; cập nhật bằng Docker Compose để tránh thay đổi IP ngoài ý muốn.")
		}
		if len(n.DriverOpts) > 0 {
			e["DriverOpts"] = n.DriverOpts
		}
		endpoints[name] = e
	}
	return config, host, map[string]any{"EndpointsConfig": endpoints}, nil
}

func waitContainer(ctx context.Context, d *dockerAPI, id, image string) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	for {
		c, err := d.inspect(ctx, id)
		if err == nil && c.State.Running && c.Image == image && c.State.Health != nil && c.State.Health.Status == "healthy" {
			return nil
		}
		if err == nil && !c.State.Running {
			return errors.New("Container mới đã dừng.")
		}
		if err == nil && c.State.Health != nil && c.State.Health.Status == "unhealthy" {
			return errors.New("Container mới không đạt kiểm tra sức khỏe.")
		}
		select {
		case <-ctx.Done():
			return errors.New("Container chưa đạt kiểm tra sức khỏe sau hai phút.")
		case <-time.After(2 * time.Second):
		}
	}
}

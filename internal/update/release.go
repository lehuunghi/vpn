// Package update installs releases from the project's fixed GitHub repository.
// A browser never supplies a download URL, executable, image, or shell command.
package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

const Repository = "lehuunghi/vpn"
const Image = "ghcr.io/lehuunghi/vpn20"
const maxDownload = 256 << 20

type Asset struct {
	Name   string `json:"name"`
	URL    string `json:"browser_download_url"`
	Digest string `json:"digest"`
	Size   int64  `json:"size"`
}

type Release struct {
	Tag        string  `json:"tag_name"`
	URL        string  `json:"html_url"`
	Draft      bool    `json:"draft"`
	Prerelease bool    `json:"prerelease"`
	Assets     []Asset `json:"assets"`
}

type Candidate struct {
	Version     string `json:"version"`
	URL         string `json:"url"`
	Prerelease  bool   `json:"prerelease"`
	Asset       Asset  `json:"-"`
	ImageDigest string `json:"-"`
}

func releaseClient() *http.Client {
	return &http.Client{Timeout: 10 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || !allowedDownload(req.URL) {
			return errors.New("untrusted release redirect")
		}
		return nil
	}}
}

func allowedDownload(u *url.URL) bool {
	if u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return false
	}
	switch u.Hostname() {
	case "github.com", "api.github.com", "objects.githubusercontent.com", "release-assets.githubusercontent.com", "github-releases.githubusercontent.com":
		return true
	}
	return false
}

func assetName(tag, goos, arch string) string {
	v := strings.TrimPrefix(tag, "v")
	if goos == "windows" {
		a := map[string]string{"amd64": "x64", "386": "x86"}[arch]
		if a == "" {
			return ""
		}
		return "VPN20-" + v + "-windows-" + a + "-setup.exe"
	}
	if goos == "linux" {
		return "VPN20-" + v + "-linux-" + arch + ".tar.gz"
	}
	return ""
}

func chooseRelease(releases []Release, current, goos, arch string, prerelease bool) (*Candidate, error) {
	var best *Candidate
	for _, r := range releases {
		if r.Draft || (r.Prerelease && !prerelease) || !semver.IsValid(r.Tag) {
			continue
		}
		if semver.IsValid("v"+strings.TrimPrefix(current, "v")) && semver.Compare(r.Tag, "v"+strings.TrimPrefix(current, "v")) <= 0 {
			continue
		}
		if best != nil && semver.Compare(r.Tag, "v"+best.Version) <= 0 {
			continue
		}
		name := assetName(r.Tag, goos, arch)
		if name == "" {
			continue
		}
		for _, a := range r.Assets {
			u, err := url.Parse(a.URL)
			if a.Name != name || a.Size <= 0 || a.Size > maxDownload || err != nil || !allowedDownload(u) || !strings.HasPrefix(a.URL, "https://github.com/"+Repository+"/releases/download/"+r.Tag+"/") {
				continue
			}
			if _, err = assetDigest(a); err != nil {
				continue
			}
			best = &Candidate{Version: strings.TrimPrefix(r.Tag, "v"), URL: "https://github.com/" + Repository + "/releases/tag/" + url.PathEscape(r.Tag), Prerelease: r.Prerelease, Asset: a}
		}
	}
	return best, nil
}

func assetDigest(a Asset) ([]byte, error) {
	if !strings.HasPrefix(a.Digest, "sha256:") {
		return nil, errors.New("release asset has no GitHub SHA-256 digest")
	}
	b, err := hex.DecodeString(strings.TrimPrefix(a.Digest, "sha256:"))
	if err != nil || len(b) != sha256.Size {
		return nil, errors.New("invalid release digest")
	}
	return b, nil
}

func (m *Manager) latest(ctx context.Context) (*Candidate, error) {
	if m.mode == "docker" {
		return m.dockerLatest(ctx)
	}
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/repos/"+Repository+"/releases?per_page=50", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "VPN20/"+m.opts.Version)
	res, err := m.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("GitHub releases returned HTTP %d", res.StatusCode)
	}
	var releases []Release
	if err = json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(&releases); err != nil {
		return nil, err
	}
	return chooseRelease(releases, m.opts.Version, runtime.GOOS, runtime.GOARCH, m.opts.Prerelease || strings.Contains(m.opts.Version, "-"))
}

func (m *Manager) download(ctx context.Context, c *Candidate, path string) error {
	if _, err := assetDigest(c.Asset); err != nil {
		return err
	}
	u, err := url.Parse(c.Asset.URL)
	if err != nil || !allowedDownload(u) {
		return errors.New("untrusted release URL")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", c.Asset.URL, nil)
	if err != nil {
		return err
	}
	res, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("release download returned HTTP %d", res.StatusCode)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(res.Body, maxDownload+1))
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if n != c.Asset.Size || n > maxDownload || "sha256:"+hex.EncodeToString(h.Sum(nil)) != c.Asset.Digest {
		_ = os.Remove(path)
		return errors.New("release size or SHA-256 verification failed")
	}
	return nil
}

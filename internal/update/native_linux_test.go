//go:build linux

package update

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestLinuxArchiveRejectsLinksAndNonExecutables(t *testing.T) {
	for _, kind := range []byte{tar.TypeSymlink, tar.TypeReg} {
		t.Run(string([]byte{kind}), func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "release.tar.gz")
			f, err := os.Create(path)
			if err != nil {
				t.Fatal(err)
			}
			gz := gzip.NewWriter(f)
			tw := tar.NewWriter(gz)
			h := &tar.Header{Name: "vpn20", Mode: 0755, Typeflag: kind, Linkname: "/bin/sh"}
			if kind == tar.TypeReg {
				h.Size = 3
			}
			if err = tw.WriteHeader(h); err != nil {
				t.Fatal(err)
			}
			if kind == tar.TypeReg {
				_, _ = tw.Write([]byte("bad"))
			}
			_ = tw.Close()
			_ = gz.Close()
			_ = f.Close()
			if err = extractBinary(path, filepath.Join(dir, "vpn20")); err == nil {
				t.Fatal("accepted unsafe/non-ELF archive")
			}
		})
	}
}

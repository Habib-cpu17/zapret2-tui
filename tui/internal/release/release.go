// Package release downloads and verifies an upstream zapret2 release tarball.
package release

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Repo is the GitHub repo releases come from. Binaries are only published in
// upstream releases, so this defaults to upstream; override with ZAPRET_REPO.
var Repo = func() string {
	if r := os.Getenv("ZAPRET_REPO"); r != "" {
		return r
	}
	return "bol-van/zapret2"
}()

var client = &http.Client{Timeout: 5 * time.Minute}

type Asset struct {
	Name   string `json:"name"`
	URL    string `json:"browser_download_url"`
	Size   int64  `json:"size"`
	Digest string `json:"digest"` // "sha256:<hex>", set by GitHub
}

type Release struct {
	Tag    string  `json:"tag_name"`
	Assets []Asset `json:"assets"`
}

var tarRe = regexp.MustCompile(`^zapret2-v[0-9.]+\.tar\.gz$`) // not the openwrt-embedded one

func (r Release) Tarball() (Asset, bool) {
	for _, a := range r.Assets {
		if tarRe.MatchString(a.Name) {
			return a, true
		}
	}
	return Asset{}, false
}

func (r Release) Checksums() (Asset, bool) {
	for _, a := range r.Assets {
		if a.Name == "sha256sum.txt" {
			return a, true
		}
	}
	return Asset{}, false
}

func Latest() (Release, error) {
	var rel Release
	req, _ := http.NewRequest("GET", "https://api.github.com/repos/"+Repo+"/releases/latest", nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return rel, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return rel, fmt.Errorf("github api: %s", resp.Status)
	}
	return rel, json.NewDecoder(resp.Body).Decode(&rel)
}

// Download fetches the release tarball into dir and verifies it against the
// SHA-256 digest GitHub publishes for the asset. progress receives (done, total).
func Download(rel Release, dir string, progress func(done, total int64)) (string, error) {
	tb, ok := rel.Tarball()
	if !ok {
		return "", fmt.Errorf("release %s has no zapret2 tarball", rel.Tag)
	}
	want, ok := strings.CutPrefix(strings.ToLower(tb.Digest), "sha256:")
	if !ok || len(want) != 64 {
		return "", fmt.Errorf("github did not provide a sha256 digest for %s", tb.Name)
	}

	resp, err := client.Get(tb.URL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("download: %s", resp.Status)
	}
	path := filepath.Join(dir, tb.Name)
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	var done int64
	buf := make([]byte, 64*1024)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			f.Write(buf[:n])
			h.Write(buf[:n])
			done += int64(n)
			if progress != nil {
				progress(done, tb.Size)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return "", rerr
		}
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return "", fmt.Errorf("checksum mismatch for %s: got %s want %s", tb.Name, got, want)
	}
	return path, nil
}

func get(url string) ([]byte, error) {
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

// Extract unpacks tarball into dest, stripping the top-level directory.
func Extract(tarball, dest string) error {
	f, err := os.Open(tarball)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	destAbs, _ := filepath.Abs(dest)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		name := h.Name
		if i := strings.IndexByte(name, '/'); i >= 0 {
			name = name[i+1:]
		} else {
			continue
		}
		if name == "" {
			continue
		}
		target := filepath.Join(destAbs, name)
		if !strings.HasPrefix(target, destAbs+string(os.PathSeparator)) {
			return fmt.Errorf("unsafe path in archive: %s", h.Name)
		}
		mode := os.FileMode(h.Mode) & 0o777
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			os.MkdirAll(filepath.Dir(target), 0o755)
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
			if err != nil {
				return err
			}
			_, err = io.Copy(out, tr)
			out.Close()
			if err != nil {
				return err
			}
		case tar.TypeSymlink:
			if filepath.IsAbs(h.Linkname) || !strings.HasPrefix(filepath.Join(filepath.Dir(target), h.Linkname), destAbs+string(os.PathSeparator)) {
				return fmt.Errorf("unsafe symlink in archive: %s -> %s", h.Name, h.Linkname)
			}
			os.MkdirAll(filepath.Dir(target), 0o755)
			os.Remove(target)
			if err := os.Symlink(h.Linkname, target); err != nil {
				return err
			}
		}
	}
}

// VerifyBinaries checks extracted binaries against the release's
// sha256sum.txt (which lists files inside the archive, e.g.
// "zapret2-v1.0.7/binaries/linux-x86_64/nfqws2"). Returns the count verified.
func VerifyBinaries(rel Release, dest string) (int, error) {
	cs, ok := rel.Checksums()
	if !ok {
		return 0, fmt.Errorf("release has no sha256sum.txt")
	}
	sums, err := get(cs.URL)
	if err != nil {
		return 0, err
	}
	n := 0
	sc := bufio.NewScanner(strings.NewReader(string(sums)))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) != 2 {
			continue
		}
		name := strings.TrimPrefix(f[1], "*")
		i := strings.Index(name, "/binaries/")
		if i < 0 {
			continue
		}
		p := filepath.Join(dest, name[i+1:])
		b, err := os.ReadFile(p)
		if err != nil {
			continue // not shipped in this tarball
		}
		h := sha256.Sum256(b)
		if hex.EncodeToString(h[:]) != strings.ToLower(f[0]) {
			return n, fmt.Errorf("checksum mismatch: %s", name[i+1:])
		}
		n++
	}
	if n == 0 {
		return 0, fmt.Errorf("no binaries matched sha256sum.txt")
	}
	return n, nil
}

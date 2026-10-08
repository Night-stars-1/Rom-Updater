package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

var (
	errPackageExists  = errors.New("package already exists")
	errPackageInUse   = errors.New("package is referenced by a release")
	errReleaseMissing = errors.New("release not found")
)

// Delivery selects how an updater obtains the release. Empty and "app" mean
// in-app download and install; "browser" opens a URL instead.
const (
	deliveryApp     = "app"
	deliveryBrowser = "browser"
)

// releaseConfig is one entry of the operator-maintained manifest. Everything
// derivable from the package itself (timestamp, incremental, size, hash) is
// computed, never configured, and so may only be configured for a browser
// release without file.
type releaseConfig struct {
	Device         string `json:"device"`
	Channel        string `json:"channel"`
	Version        string `json:"version"`
	Changelog      string `json:"changelog"`
	Delivery       string `json:"delivery,omitempty"`
	File           string `json:"file"`
	DownloadURL    string `json:"download_url,omitempty"`
	BuildTimestamp int64  `json:"build_timestamp,omitempty"`
	Incremental    string `json:"incremental,omitempty"`
	Size           int64  `json:"size,omitempty"`
	Type           string `json:"type,omitempty"`
}

type manifest struct {
	Releases []releaseConfig `json:"releases"`
}

type packageInfo struct {
	Name    string
	Path    string
	Size    int64
	ModTime time.Time
	SHA256  string
	Meta    otaMetadata
}

// ETag is strong and content-derived, so a replaced package never matches a
// client's If-Range from an older download.
func (p *packageInfo) ETag() string { return `"` + p.SHA256 + `"` }

type releaseResponse struct {
	SchemaVersion  int              `json:"schema_version"`
	Device         string           `json:"device"`
	Channel        string           `json:"channel"`
	Version        string           `json:"version"`
	BuildTimestamp int64            `json:"build_timestamp"`
	Incremental    string           `json:"incremental"`
	Changelog      string           `json:"changelog"`
	Delivery       string           `json:"delivery,omitempty"`
	DownloadURL    string           `json:"download_url,omitempty"`
	Package        *packageResponse `json:"package,omitempty"`
}

// packageResponse is fully populated for every release with a hosted package.
// An external-only browser release has no package to fetch, so it carries only
// the display-only fields the operator configured.
type packageResponse struct {
	Type   string `json:"type,omitempty"`
	URL    string `json:"url,omitempty"`
	Size   int64  `json:"size,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
}

type snapshot struct {
	releases map[string]releaseResponse // key: device + "/" + channel
	packages map[string]*packageInfo    // key: file name
}

type catalog struct {
	manifestPath string
	filesDir     string
	baseURL      *url.URL

	mu          sync.Mutex
	manifestMod time.Time
	manifest    manifest
	cache       map[string]*packageInfo
	current     *snapshot
}

func newCatalog(manifestPath, filesDir string, baseURL *url.URL) *catalog {
	return &catalog{
		manifestPath: manifestPath,
		filesDir:     filesDir,
		baseURL:      baseURL,
		cache:        make(map[string]*packageInfo),
	}
}

// load returns the current snapshot, re-reading the manifest and re-hashing
// only packages whose size or mtime changed since the last call.
func (c *catalog) load() (*snapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.loadLocked()
}

func (c *catalog) loadLocked() (*snapshot, error) {
	changed, err := c.syncManifestLocked()
	if err != nil {
		return nil, err
	}
	dirty := changed || c.current == nil

	used := make(map[string]bool, len(c.manifest.Releases))
	for _, r := range c.manifest.Releases {
		if r.File == "" {
			continue
		}
		used[r.File] = true
		changed, err := c.refreshPackage(r.File)
		if err != nil {
			return nil, err
		}
		dirty = dirty || changed
	}
	for name, p := range c.cache {
		if used[name] {
			continue
		}
		// Keep unpublished uploads so publishing them does not re-hash.
		if st, err := os.Stat(p.Path); err != nil || st.Size() != p.Size || !st.ModTime().Equal(p.ModTime) {
			delete(c.cache, name)
		}
	}
	if !dirty {
		return c.current, nil
	}

	snap := &snapshot{
		releases: make(map[string]releaseResponse, len(c.manifest.Releases)),
		packages: make(map[string]*packageInfo, len(c.cache)),
	}
	for _, r := range c.manifest.Releases {
		p := c.cache[r.File]
		if r.File != "" {
			if !slices.Contains(p.Meta.Devices, r.Device) {
				return nil, fmt.Errorf("%s: built for %v, not device %q", r.File, p.Meta.Devices, r.Device)
			}
			snap.packages[r.File] = p
		}
		snap.releases[r.Device+"/"+r.Channel] = newReleaseResponse(c.baseURL, r, p)
	}
	c.current = snap
	return snap, nil
}

// newReleaseResponse renders one release for clients. App delivery is the
// original schema 1 with a fully populated package; browser delivery is
// schema 2 so schema-1 clients reject it instead of installing in-app.
func newReleaseResponse(baseURL *url.URL, r releaseConfig, p *packageInfo) releaseResponse {
	rel := releaseResponse{
		SchemaVersion: 1,
		Device:        r.Device,
		Channel:       r.Channel,
		Version:       r.Version,
		Changelog:     r.Changelog,
	}
	hosted := ""
	if p != nil {
		rel.BuildTimestamp = p.Meta.BuildTimestamp
		rel.Incremental = p.Meta.Incremental
		hosted = baseURL.JoinPath(r.File).String()
		rel.Package = &packageResponse{
			Type:   p.Meta.Type,
			URL:    hosted,
			Size:   p.Size,
			SHA256: p.SHA256,
		}
	}
	if r.Delivery != deliveryBrowser {
		return rel
	}
	rel.SchemaVersion = 2
	rel.Delivery = deliveryBrowser
	rel.DownloadURL = r.DownloadURL
	if rel.DownloadURL == "" {
		rel.DownloadURL = hosted
	}
	if p == nil {
		rel.BuildTimestamp = r.BuildTimestamp
		rel.Incremental = r.Incremental
		if r.Size != 0 || r.Type != "" {
			rel.Package = &packageResponse{Type: r.Type, Size: r.Size}
		}
	}
	return rel
}

// syncManifestLocked re-reads the manifest if it changed on disk. A missing
// manifest is an empty one, so a fresh deployment can be populated via the
// admin API.
func (c *catalog) syncManifestLocked() (changed bool, err error) {
	st, err := os.Stat(c.manifestPath)
	if errors.Is(err, os.ErrNotExist) {
		changed = len(c.manifest.Releases) > 0 || !c.manifestMod.IsZero()
		c.manifest, c.manifestMod = manifest{}, time.Time{}
		return changed, nil
	}
	if err != nil {
		return false, fmt.Errorf("stat manifest: %w", err)
	}
	if st.ModTime().Equal(c.manifestMod) {
		return false, nil
	}
	m, err := readManifest(c.manifestPath)
	if err != nil {
		return false, err
	}
	c.manifest, c.manifestMod = m, st.ModTime()
	return true, nil
}

// addPackage moves a fully written and indexed upload into filesDir under
// name. Existing packages are never replaced: clients may be resuming them.
func (c *catalog) addPackage(tmpPath, name string, p *packageInfo) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	path := filepath.Join(c.filesDir, name)
	if _, err := os.Lstat(path); err == nil {
		return errPackageExists
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	p.Name, p.Path, p.ModTime = name, path, st.ModTime()
	c.cache[name] = p
	return nil
}

// publish points r.Device/r.Channel at r.File, persisting the manifest
// atomically. The package must already be in filesDir.
func (c *catalog) publish(r releaseConfig) (releaseResponse, error) {
	if err := validateRelease(r); err != nil {
		return releaseResponse{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, err := c.syncManifestLocked(); err != nil {
		return releaseResponse{}, err
	}
	if r.File != "" {
		if _, err := c.refreshPackage(r.File); err != nil {
			return releaseResponse{}, err
		}
		if p := c.cache[r.File]; !slices.Contains(p.Meta.Devices, r.Device) {
			return releaseResponse{}, fmt.Errorf("%s: built for %v, not device %q", r.File, p.Meta.Devices, r.Device)
		}
	}

	m := manifest{Releases: slices.Clone(c.manifest.Releases)}
	i := slices.IndexFunc(m.Releases, func(e releaseConfig) bool {
		return e.Device == r.Device && e.Channel == r.Channel
	})
	if i >= 0 {
		m.Releases[i] = r
	} else {
		m.Releases = append(m.Releases, r)
	}
	if err := c.saveManifestLocked(m); err != nil {
		return releaseResponse{}, err
	}
	snap, err := c.loadLocked()
	if err != nil {
		return releaseResponse{}, err
	}
	return snap.releases[r.Device+"/"+r.Channel], nil
}

// unpublish removes the device/channel release. Its package stays on disk.
func (c *catalog) unpublish(device, channel string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, err := c.syncManifestLocked(); err != nil {
		return err
	}
	m := manifest{Releases: slices.DeleteFunc(slices.Clone(c.manifest.Releases), func(e releaseConfig) bool {
		return e.Device == device && e.Channel == channel
	})}
	if len(m.Releases) == len(c.manifest.Releases) {
		return errReleaseMissing
	}
	if err := c.saveManifestLocked(m); err != nil {
		return err
	}
	_, err := c.loadLocked()
	return err
}

// deletePackage removes a package no release points at.
func (c *catalog) deletePackage(name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, err := c.syncManifestLocked(); err != nil {
		return err
	}
	if slices.ContainsFunc(c.manifest.Releases, func(e releaseConfig) bool { return e.File == name }) {
		return errPackageInUse
	}
	if err := os.Remove(filepath.Join(c.filesDir, name)); err != nil {
		return err
	}
	delete(c.cache, name)
	return nil
}

type adminState struct {
	BaseURL  string          `json:"base_url"`
	Error    string          `json:"error,omitempty"`
	Releases []releaseConfig `json:"releases"`
	Packages []packageState  `json:"packages"`
}

type packageState struct {
	File           string    `json:"file"`
	Size           int64     `json:"size"`
	ModTime        time.Time `json:"mod_time"`
	SHA256         string    `json:"sha256,omitempty"` // empty until first hashed
	Type           string    `json:"type,omitempty"`
	Devices        []string  `json:"devices,omitempty"`
	BuildTimestamp int64     `json:"build_timestamp,omitempty"`
	Incremental    string    `json:"incremental,omitempty"`
	Error          string    `json:"error,omitempty"`
	UsedBy         []string  `json:"used_by"`
}

// state lists the manifest and every package in filesDir. Packages that were
// never hashed only get their (cheap) OTA metadata read, so listing a fresh
// directory of multi-GB packages does not block on hashing.
func (c *catalog) state() (adminState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	s := adminState{BaseURL: c.baseURL.String()}
	// A broken manifest or package is reported, not fatal, so the operator
	// can still see and fix things from the UI.
	if _, err := c.loadLocked(); err != nil {
		s.Error = err.Error()
	}
	s.Releases = slices.Clone(c.manifest.Releases)
	if s.Releases == nil {
		s.Releases = []releaseConfig{}
	}

	entries, err := os.ReadDir(c.filesDir)
	if err != nil {
		return s, fmt.Errorf("list packages: %w", err)
	}
	s.Packages = []packageState{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !validFileName(name) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		ps := packageState{File: name, Size: info.Size(), ModTime: info.ModTime(), UsedBy: []string{}}
		for _, r := range c.manifest.Releases {
			if r.File == name {
				ps.UsedBy = append(ps.UsedBy, r.Device+"/"+r.Channel)
			}
		}
		meta, err := c.packageMetaLocked(name, info)
		if err != nil {
			ps.Error = err.Error()
		} else {
			ps.Type, ps.Devices = meta.Meta.Type, meta.Meta.Devices
			ps.BuildTimestamp, ps.Incremental = meta.Meta.BuildTimestamp, meta.Meta.Incremental
			ps.SHA256 = meta.SHA256
		}
		s.Packages = append(s.Packages, ps)
	}
	return s, nil
}

// packageMetaLocked returns the cached packageInfo when it is current, or
// metadata only (no SHA256) otherwise.
func (c *catalog) packageMetaLocked(name string, info os.FileInfo) (*packageInfo, error) {
	if p := c.cache[name]; p != nil && p.Size == info.Size() && p.ModTime.Equal(info.ModTime()) {
		return p, nil
	}
	f, err := os.Open(filepath.Join(c.filesDir, name))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	meta, err := readOTAMetadata(f, info.Size())
	if err != nil {
		return nil, err
	}
	return &packageInfo{Meta: meta}, nil
}

func (c *catalog) saveManifestLocked(m manifest) error {
	if err := writeManifest(c.manifestPath, m); err != nil {
		return err
	}
	st, err := os.Stat(c.manifestPath)
	if err != nil {
		return fmt.Errorf("stat manifest: %w", err)
	}
	c.manifest, c.manifestMod, c.current = m, st.ModTime(), nil
	return nil
}

func (c *catalog) refreshPackage(name string) (changed bool, err error) {
	path := filepath.Join(c.filesDir, name)
	st, err := os.Stat(path)
	if err != nil {
		return false, fmt.Errorf("stat package: %w", err)
	}
	if p := c.cache[name]; p != nil && p.Size == st.Size() && p.ModTime.Equal(st.ModTime()) {
		return false, nil
	}
	p, err := indexPackage(name, path)
	if err != nil {
		return false, fmt.Errorf("%s: %w", name, err)
	}
	c.cache[name] = p
	return true, nil
}

func indexPackage(name, path string) (*packageInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	h := sha256.New()
	if _, err := io.Copy(h, io.NewSectionReader(f, 0, st.Size())); err != nil {
		return nil, fmt.Errorf("hash: %w", err)
	}
	p, err := inspectPackage(f, st.Size(), hex.EncodeToString(h.Sum(nil)))
	if err != nil {
		return nil, err
	}
	p.Name, p.Path, p.ModTime = name, path, st.ModTime()
	return p, nil
}

// inspectPackage builds the content-derived part of packageInfo for an
// already hashed package.
func inspectPackage(r io.ReaderAt, size int64, sha string) (*packageInfo, error) {
	meta, err := readOTAMetadata(r, size)
	if err != nil {
		return nil, err
	}
	return &packageInfo{Size: size, SHA256: sha, Meta: meta}, nil
}

func readManifest(path string) (manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return manifest{}, fmt.Errorf("read manifest: %w", err)
	}
	var m manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return manifest{}, fmt.Errorf("parse manifest: %w", err)
	}
	seen := make(map[string]bool, len(m.Releases))
	for i, r := range m.Releases {
		if err := validateRelease(r); err != nil {
			return manifest{}, fmt.Errorf("manifest release %d: %w", i, err)
		}
		key := r.Device + "/" + r.Channel
		if seen[key] {
			return manifest{}, fmt.Errorf("manifest: duplicate release for %s", key)
		}
		seen[key] = true
	}
	return m, nil
}

func validateRelease(r releaseConfig) error {
	if r.Device == "" || r.Channel == "" || r.Version == "" {
		return errors.New("device, channel and version are required")
	}
	switch r.Delivery {
	case "", deliveryApp, deliveryBrowser:
	default:
		return fmt.Errorf("delivery %q must be %q or %q", r.Delivery, deliveryApp, deliveryBrowser)
	}
	if r.File == "" && !(r.Delivery == deliveryBrowser && r.DownloadURL != "") {
		return errors.New("file is required unless browser delivery has a download_url")
	}
	if r.File != "" && !validFileName(r.File) {
		return fmt.Errorf("file %q must be a bare file name", r.File)
	}
	if r.DownloadURL != "" {
		if err := validateDownloadURL(r.DownloadURL); err != nil {
			return err
		}
	}

	switch {
	case r.Delivery != deliveryBrowser:
		// App delivery: every field beyond the manifest's own is derived.
		if r.DownloadURL != "" {
			return errors.New("download_url is only valid for browser delivery")
		}
		if r.BuildTimestamp != 0 || r.Incremental != "" || r.Size != 0 || r.Type != "" {
			return errors.New("build_timestamp, incremental, size and type are derived from the package for app delivery")
		}
	case r.File != "":
		// Browser delivery of the hosted package: still derived.
		if r.BuildTimestamp != 0 || r.Incremental != "" || r.Size != 0 || r.Type != "" {
			return errors.New("build_timestamp, incremental, size and type are derived from the package when file is set")
		}
	default:
		// External-only browser delivery: nothing is derivable.
		if r.BuildTimestamp <= 0 {
			return errors.New("build_timestamp is required for browser delivery without file")
		}
		if r.Incremental == "" {
			return errors.New("incremental is required for browser delivery without file")
		}
		if r.Size < 0 {
			return errors.New("size must not be negative")
		}
		switch r.Type {
		case "", "full", "incremental":
		default:
			return fmt.Errorf("type %q must be %q or %q", r.Type, "full", "incremental")
		}
	}
	return nil
}

// validateDownloadURL enforces the browser delivery contract: an absolute
// https URL with a host, no userinfo and no fragment.
func validateDownloadURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("download_url: %w", err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("download_url %q must be an absolute https URL", raw)
	}
	if u.Host == "" {
		return fmt.Errorf("download_url %q must have a host", raw)
	}
	if u.User != nil {
		return errors.New("download_url must not contain userinfo")
	}
	if u.Fragment != "" {
		return errors.New("download_url must not contain a fragment")
	}
	return nil
}

func validFileName(name string) bool {
	return name == filepath.Base(name) && !strings.HasPrefix(name, ".") &&
		!strings.ContainsAny(name, `/\:`)
}

func writeManifest(path string, m manifest) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(m); err != nil {
		return err
	}
	return writeFileAtomic(path, buf.Bytes())
}

func writeFileAtomic(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

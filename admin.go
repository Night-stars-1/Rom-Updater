package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

//go:embed web/dist/index.html
var adminHTML []byte

//go:embed web/dist/assets
var adminAssets embed.FS

var adminAssetHandler = func() http.Handler {
	assets, err := fs.Sub(adminAssets, "web/dist/assets")
	if err != nil {
		panic(err)
	}
	return http.StripPrefix("/admin/assets/", http.FileServerFS(assets))
}()

type admin struct {
	cat       *catalog
	token     []byte
	maxUpload int64
}

type uploadResponse struct {
	File           string   `json:"file"`
	Size           int64    `json:"size"`
	SHA256         string   `json:"sha256"`
	Type           string   `json:"type"`
	Devices        []string `json:"devices"`
	BuildTimestamp int64    `json:"build_timestamp"`
	Incremental    string   `json:"incremental"`
}

func (a *admin) authorize(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || subtle.ConstantTimeCompare([]byte(got), a.token) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="ota-admin"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

// upload streams the request body into filesDir, hashing it on the way, and
// only exposes it under its final name once the OTA metadata is readable.
// An optional ?sha256= guards against truncated or corrupted transfers.
func (a *admin) upload(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !validFileName(name) || !strings.HasSuffix(name, ".zip") {
		http.Error(w, "file name must be a bare *.zip name", http.StatusBadRequest)
		return
	}
	want := strings.ToLower(r.URL.Query().Get("sha256"))
	if want != "" && !isSHA256Hex(want) {
		http.Error(w, "sha256 must be 64 hex characters", http.StatusBadRequest)
		return
	}
	// Reject before the client sends gigabytes; addPackage re-checks atomically.
	if _, err := os.Lstat(filepath.Join(a.cat.filesDir, name)); err == nil {
		http.Error(w, errPackageExists.Error(), http.StatusConflict)
		return
	}

	// The leading dot keeps partial uploads unservable (validFileName).
	f, err := os.CreateTemp(a.cat.filesDir, ".upload-*")
	if err != nil {
		log.Printf("upload %s: %v", name, err)
		http.Error(w, "cannot create upload file", http.StatusInternalServerError)
		return
	}
	tmp := f.Name()
	defer os.Remove(tmp) // no-op once renamed into place
	defer f.Close()

	h := sha256.New()
	size, err := io.Copy(io.MultiWriter(f, h), http.MaxBytesReader(w, r.Body, a.maxUpload))
	if err != nil {
		if errors.As(err, new(*http.MaxBytesError)) {
			http.Error(w, fmt.Sprintf("package exceeds %d bytes", a.maxUpload), http.StatusRequestEntityTooLarge)
			return
		}
		log.Printf("upload %s: %v", name, err)
		http.Error(w, "upload interrupted", http.StatusBadRequest)
		return
	}
	if err := f.Sync(); err != nil {
		log.Printf("upload %s: %v", name, err)
		http.Error(w, "cannot persist upload", http.StatusInternalServerError)
		return
	}
	sum := hex.EncodeToString(h.Sum(nil))
	if want != "" && want != sum {
		http.Error(w, fmt.Sprintf("sha256 mismatch: got %s", sum), http.StatusUnprocessableEntity)
		return
	}
	p, err := inspectPackage(f, size, sum)
	if err != nil {
		http.Error(w, "not a valid OTA package: "+err.Error(), http.StatusUnprocessableEntity)
		return
	}
	if err := f.Close(); err != nil {
		log.Printf("upload %s: %v", name, err)
		http.Error(w, "cannot persist upload", http.StatusInternalServerError)
		return
	}
	if err := a.cat.addPackage(tmp, name, p); err != nil {
		if errors.Is(err, errPackageExists) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		log.Printf("upload %s: %v", name, err)
		http.Error(w, "cannot store package", http.StatusInternalServerError)
		return
	}
	log.Printf("uploaded %s (%d bytes, sha256 %s)", name, size, sum)
	writeJSON(w, http.StatusCreated, uploadResponse{
		File:           name,
		Size:           p.Size,
		SHA256:         p.SHA256,
		Type:           p.Meta.Type,
		Devices:        p.Meta.Devices,
		BuildTimestamp: p.Meta.BuildTimestamp,
		Incremental:    p.Meta.Incremental,
	})
}

// publish points a device/channel at an uploaded package.
func (a *admin) publish(w http.ResponseWriter, r *http.Request) {
	var rc releaseConfig
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&rc); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	rel, err := a.cat.publish(rc)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	target := rc.File
	if target == "" {
		target = rc.DownloadURL
	}
	log.Printf("published %s/%s -> %s (%s)", rc.Device, rc.Channel, target, rc.Version)
	writeJSON(w, http.StatusOK, rel)
}

func (a *admin) listState(w http.ResponseWriter, r *http.Request) {
	s, err := a.cat.state()
	if err != nil {
		log.Printf("admin state: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, s)
}

func (a *admin) unpublish(w http.ResponseWriter, r *http.Request) {
	device, channel := r.PathValue("device"), r.PathValue("channel")
	if err := a.cat.unpublish(device, channel); err != nil {
		if errors.Is(err, errReleaseMissing) {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		log.Printf("unpublish %s/%s: %v", device, channel, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("unpublished %s/%s", device, channel)
	w.WriteHeader(http.StatusNoContent)
}

func (a *admin) deletePackage(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !validFileName(name) {
		http.Error(w, "invalid file name", http.StatusBadRequest)
		return
	}
	if err := a.cat.deletePackage(name); err != nil {
		switch {
		case errors.Is(err, errPackageInUse):
			http.Error(w, err.Error(), http.StatusConflict)
		case errors.Is(err, os.ErrNotExist):
			http.Error(w, "package not found", http.StatusNotFound)
		default:
			log.Printf("delete %s: %v", name, err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	log.Printf("deleted %s", name)
	w.WriteHeader(http.StatusNoContent)
}

// serveUI serves the single-page admin console. It is static and holds no
// secrets; every API call it makes carries the bearer token.
func serveUI(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-cache")
	h.Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; connect-src 'self'; img-src data:; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	w.Write(adminHTML)
}

// serveAdminAsset serves Vite's content-hashed files from the binary.
func serveAdminAsset(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	adminAssetHandler.ServeHTTP(w, r)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	body, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	w.Write(append(body, '\n'))
}

func isSHA256Hex(s string) bool {
	_, err := hex.DecodeString(s)
	return err == nil && len(s) == sha256.Size*2
}

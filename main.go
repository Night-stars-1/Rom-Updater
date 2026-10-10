package main

import (
	"encoding/json"
	"errors"
	"flag"
	"log"
	"net/http"
	"net/url"
	"os"
	"time"
)

func main() {
	addr := flag.String("addr", ":8443", "listen address")
	manifestPath := flag.String("manifest", "releases.json", "release manifest")
	filesDir := flag.String("files", "files", "directory holding OTA packages")
	base := flag.String("base-url", "", "public origin used in package URLs, e.g. https://ota.example.com")
	certFile := flag.String("tls-cert", "", "TLS certificate (omit only behind an HTTPS reverse proxy)")
	keyFile := flag.String("tls-key", "", "TLS private key")
	maxUpload := flag.Int64("max-upload", 8<<30, "maximum OTA package upload size in bytes")
	flag.Parse()

	baseURL, err := url.Parse(*base)
	if err != nil || baseURL.Scheme == "" || baseURL.Host == "" {
		log.Fatalf("-base-url must be an absolute URL, got %q", *base)
	}
	if baseURL.Scheme != "https" {
		log.Printf("warning: -base-url is not https; updater clients require HTTPS")
	}
	if (*certFile == "") != (*keyFile == "") {
		log.Fatal("-tls-cert and -tls-key must be given together")
	}

	if err := os.MkdirAll(*filesDir, 0o755); err != nil {
		log.Fatal(err)
	}
	cat := newCatalog(*manifestPath, *filesDir, baseURL)
	// Fail fast on a broken manifest and pay the hashing cost before serving.
	if _, err := cat.load(); err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /ota/{device}/{channel}", cat.serveRelease)
	mux.HandleFunc("GET /{name}", cat.servePackage)
	// Read from the environment so the token stays out of the process list.
	if token := os.Getenv("OTA_ADMIN_TOKEN"); token != "" {
		a := &admin{cat: cat, token: []byte(token), maxUpload: *maxUpload}
		mux.HandleFunc("GET /admin/{$}", serveUI)
		mux.HandleFunc("GET /admin/assets/{path...}", serveAdminAsset)
		mux.Handle("GET /admin", http.RedirectHandler("/admin/", http.StatusMovedPermanently))
		mux.HandleFunc("GET /admin/state", a.authorize(a.listState))
		mux.HandleFunc("PUT /admin/files/{name}", a.authorize(a.upload))
		mux.HandleFunc("DELETE /admin/files/{name}", a.authorize(a.deletePackage))
		mux.HandleFunc("POST /admin/releases", a.authorize(a.publish))
		mux.HandleFunc("DELETE /admin/releases/{device}/{channel}", a.authorize(a.unpublish))
	} else {
		log.Printf("OTA_ADMIN_TOKEN not set; admin API disabled")
	}

	srv := &http.Server{
		Addr:              *addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	log.Printf("listening on %s", *addr)
	if *certFile != "" {
		err = srv.ListenAndServeTLS(*certFile, *keyFile)
	} else {
		log.Printf("warning: serving plain HTTP; terminate TLS in front of this server")
		err = srv.ListenAndServe()
	}
	log.Fatal(err)
}

func (c *catalog) serveRelease(w http.ResponseWriter, r *http.Request) {
	channel := r.PathValue("channel")
	snap, err := c.load()
	if err != nil {
		log.Printf("catalog: %v", err)
		http.Error(w, "catalog unavailable", http.StatusServiceUnavailable)
		return
	}
	rel, ok := snap.releases[r.PathValue("device")+"/"+channel]
	if !ok {
		http.NotFound(w, r)
		return
	}
	body, err := json.MarshalIndent(rel, "", "  ")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(append(body, '\n'))
}

// servePackage relies on http.ServeContent for HEAD, Range/206,
// Content-Range, Accept-Ranges, 416, If-Range and conditional requests;
// it only has to supply a strong ETag that matches the bytes being sent.
func (c *catalog) servePackage(w http.ResponseWriter, r *http.Request) {
	snap, err := c.load()
	if err != nil {
		log.Printf("catalog: %v", err)
		http.Error(w, "catalog unavailable", http.StatusServiceUnavailable)
		return
	}
	pkg, ok := snap.packages[r.PathValue("name")]
	if !ok {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(pkg.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "open failed", http.StatusInternalServerError)
		return
	}
	defer f.Close()
	// The file may have been replaced after the snapshot was taken; never
	// pair the old ETag with new bytes, or a resumed download gets spliced.
	st, err := f.Stat()
	if err != nil || st.Size() != pkg.Size || !st.ModTime().Equal(pkg.ModTime) {
		w.Header().Set("Retry-After", "5")
		http.Error(w, "package is being updated", http.StatusServiceUnavailable)
		return
	}

	h := w.Header()
	h.Set("Content-Type", "application/zip")
	h.Set("ETag", pkg.ETag())
	h.Set("Cache-Control", "public, max-age=0, must-revalidate")
	http.ServeContent(w, r, pkg.Name, pkg.ModTime, f)
}

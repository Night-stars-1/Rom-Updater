package main

import (
	"archive/zip"
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// metadataEntry is the plain-text metadata written by ota_from_target_files.
const metadataEntry = "META-INF/com/android/metadata"

type otaMetadata struct {
	Devices        []string
	BuildTimestamp int64
	Incremental    string
	Type           string // "full" or "incremental"
}

func readOTAMetadata(r io.ReaderAt, size int64) (otaMetadata, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return otaMetadata{}, fmt.Errorf("open zip: %w", err)
	}
	for _, f := range zr.File {
		if f.Name != metadataEntry {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return otaMetadata{}, fmt.Errorf("open %s: %w", metadataEntry, err)
		}
		defer rc.Close()
		return parseOTAMetadata(rc)
	}
	return otaMetadata{}, fmt.Errorf("%s not found", metadataEntry)
}

func parseOTAMetadata(r io.Reader) (otaMetadata, error) {
	props := make(map[string]string)
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			props[k] = v
		}
	}
	if err := sc.Err(); err != nil {
		return otaMetadata{}, fmt.Errorf("read %s: %w", metadataEntry, err)
	}

	var m otaMetadata
	ts, err := strconv.ParseInt(props["post-timestamp"], 10, 64)
	if err != nil || ts <= 0 {
		return m, fmt.Errorf("invalid post-timestamp %q", props["post-timestamp"])
	}
	m.BuildTimestamp = ts
	if m.Incremental = props["post-build-incremental"]; m.Incremental == "" {
		return m, errors.New("missing post-build-incremental")
	}
	for _, d := range strings.Split(props["pre-device"], ",") {
		if d = strings.TrimSpace(d); d != "" {
			m.Devices = append(m.Devices, d)
		}
	}
	if len(m.Devices) == 0 {
		return m, errors.New("missing pre-device")
	}
	// Incremental packages carry a source build precondition.
	m.Type = "full"
	if props["pre-build"] != "" {
		m.Type = "incremental"
	}
	return m, nil
}

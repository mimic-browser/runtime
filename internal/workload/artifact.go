// Package workload implements experimental, offline workload specialization.
// Captures describe transport inputs, never browser state or recorded JS effects.
package workload

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/moreveal/mimic/internal/network"
)

const artifactVersion = 1

type CaptureInfo struct {
	BranchURLs    map[string]bool
	VolatileQuery []string
	EncodedCosts  map[string]int64
}

// CaptureInventory validates metadata while keeping bodies lazy and disk-owned.
func CaptureInventory(path string) (CaptureInfo, error) {
	s, err := New(false, path, nil)
	if err != nil {
		return CaptureInfo{}, err
	}
	defer s.Close()
	info := CaptureInfo{VolatileQuery: s.capture.VolatileQuery, EncodedCosts: map[string]int64{}, BranchURLs: map[string]bool{}}
	for _, e := range s.capture.Entries {
		info.EncodedCosts[e.URL] += e.BodyBytes
		if e.BranchCaptureSHA256 != "" {
			info.BranchURLs[e.URL] = true
		}
	}
	return info, nil
}

func WritePlan(path string, plan Plan) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	z := zip.NewWriter(f)
	w, err := z.Create("manifest.json")
	if err == nil {
		err = json.NewEncoder(w).Encode(plan)
	}
	if e := z.Close(); err == nil {
		err = e
	}
	if e := f.Close(); err == nil {
		err = e
	}
	return err
}

type Plan struct {
	Format          string                 `json:"format"`
	Version         int                    `json:"version"`
	OfflineOnly     bool                   `json:"offlineOnly"`
	CaptureSHA256   string                 `json:"captureSHA256"`
	BinarySHA256    string                 `json:"binarySHA256"`
	Policy          network.ResourcePolicy `json:"policy"`
	SuppressClassic []string               `json:"suppressClassic,omitempty"`
}

// ReadPlan deliberately has no live activation path. The runner checks both
// capture and executable identity before enabling a specialization.
func ReadPlan(path string) (Plan, error) {
	var p Plan
	r, err := zip.OpenReader(path)
	if err != nil {
		return p, err
	}
	defer r.Close()
	for _, f := range r.File {
		if f.Name != "manifest.json" {
			continue
		}
		rd, err := f.Open()
		if err != nil {
			return p, err
		}
		defer rd.Close()
		err = json.NewDecoder(io.LimitReader(rd, 8<<20)).Decode(&p)
		if err != nil {
			return p, err
		}
		if p.Format != "mimic-workload-plan" || p.Version != artifactVersion || !p.OfflineOnly {
			return p, fmt.Errorf("unsupported workload plan")
		}
		_, err = network.ParseResourcePolicy(mustJSON(p.Policy))
		return p, err
	}
	return p, fmt.Errorf("missing plan manifest")
}

func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }

// writeCapture uses an atomic replacement, so a failed recording cannot look
// like a usable capture. Bodies are separate ZIP members, not base64 JSON.
func writeCapture(path string, c capture, spool *os.File) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".capture-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	z := zip.NewWriter(f)
	w, err := z.Create("manifest.json")
	if err == nil {
		err = json.NewEncoder(w).Encode(c)
	}
	for i, e := range c.Entries {
		if err != nil {
			break
		}
		w, err = z.CreateHeader(&zip.FileHeader{Name: fmt.Sprintf("bodies/%d", i), Method: zip.Store})
		if err == nil {
			if e.file != nil {
				var reader io.ReadCloser
				reader, err = e.file.Open()
				if err == nil {
					digest := sha256.New()
					var copied int64
					copied, err = io.Copy(io.MultiWriter(w, digest), reader)
					reader.Close()
					if err == nil && (copied != e.BodyBytes || hex.EncodeToString(digest.Sum(nil)) != e.BodySHA256) {
						err = fmt.Errorf("capture body integrity failed: %s", e.URL)
					}
				}
				continue
			}
			for _, part := range e.segments {
				_, err = io.Copy(w, io.NewSectionReader(spool, part.offset, part.size))
				if err != nil {
					break
				}
			}
		}
	}
	if closeErr := z.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, path)
}

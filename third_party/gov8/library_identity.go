//go:build (windows || linux) && amd64

package gov8

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

// NativeLibraryIdentity identifies the native artifact loaded by this process.
// Snapshot compatibility depends on embedder callbacks and engine patches as
// well as the upstream V8 version. Capture this identity during loading rather
// than reading a mutable override path when a later snapshot is requested.
func NativeLibraryIdentity() (string, error) {
	if err := loadShim(); err != nil {
		return "", err
	}
	return shimIdentity, nil
}

func nativeLibraryDigest(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("gov8: read native shim identity: %w", err)
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", fmt.Errorf("gov8: hash native shim identity: %w", err)
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"example.com/meshwork-prototype/meshwork"
)

// artifactStore holds immutable file snapshots for one run. Tokens carry their digests.
type artifactStore string

func (store artifactStore) capture(path, tokenType string) (meshwork.Token, error) {
	hash := sha256.New()
	temp, err := stageCopy(path, string(store), hash)
	if err != nil {
		return meshwork.Token{}, err
	}
	defer os.Remove(temp)
	digest := hex.EncodeToString(hash.Sum(nil))
	if err := os.Rename(temp, filepath.Join(string(store), digest)); err != nil {
		return meshwork.Token{}, err
	}
	return meshwork.NewToken(tokenType, digest)
}

func (store artifactStore) materialize(token meshwork.Token, dest string) error {
	var digest string
	if err := token.Decode(&digest); err != nil {
		return fmt.Errorf("decode artifact reference: %w", err)
	}
	decoded, err := hex.DecodeString(digest)
	if err != nil || len(decoded) != sha256.Size || hex.EncodeToString(decoded) != digest {
		return fmt.Errorf("invalid artifact digest %q", digest)
	}
	dir := filepath.Dir(dest)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	temp, err := stageCopy(filepath.Join(string(store), digest), dir, nil)
	if err != nil {
		return err
	}
	defer os.Remove(temp)
	return os.Rename(temp, dest)
}

// stageCopy keeps an incomplete copy out of its final path, even on write failure.
func stageCopy(source, dir string, hash io.Writer) (string, error) {
	info, err := os.Stat(source)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a regular file", source)
	}
	in, err := os.Open(source)
	if err != nil {
		return "", err
	}
	defer in.Close()
	temp, err := os.CreateTemp(dir, ".meshwork-copy-")
	if err != nil {
		return "", err
	}
	var dest io.Writer = temp
	if hash != nil {
		dest = io.MultiWriter(temp, hash)
	}
	_, err = io.Copy(dest, in)
	if err := errors.Join(err, temp.Close()); err != nil {
		os.Remove(temp.Name())
		return "", err
	}
	return temp.Name(), nil
}

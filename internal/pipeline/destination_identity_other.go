//go:build !darwin && !linux && !windows

package pipeline

import (
	"os"
	"path/filepath"
)

type portableDirectoryEntries struct {
	exact      map[string]string
	normalized map[string]string
	ambiguous  map[string]bool
}

func newCanonicalOpenedPathResolver() func(string) (string, bool) {
	directories := make(map[string]portableDirectoryEntries)
	return func(path string) (string, bool) {
		if _, err := os.Lstat(path); err != nil {
			return "", false
		}
		parent := filepath.Dir(path)
		entries, cached := directories[parent]
		if !cached {
			entries = loadPortableDirectoryEntries(parent)
			directories[parent] = entries
		}
		requestedBase := filepath.Base(path)
		actualName, found := entries.exact[requestedBase]
		if !found {
			key := normalizedDirectoryEntryName(requestedBase)
			if !entries.ambiguous[key] {
				actualName, found = entries.normalized[key]
			}
		}
		if !found {
			actualName = requestedBase
		}
		resolvedParent, err := filepath.EvalSymlinks(parent)
		if err != nil {
			resolvedParent, err = filepath.Abs(parent)
		}
		if err != nil {
			return "", false
		}
		return filepath.Join(filepath.Clean(resolvedParent), actualName), true
	}
}

func loadPortableDirectoryEntries(parent string) portableDirectoryEntries {
	result := portableDirectoryEntries{
		exact:      make(map[string]string),
		normalized: make(map[string]string),
		ambiguous:  make(map[string]bool),
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		return result
	}
	for _, entry := range entries {
		name := entry.Name()
		result.exact[name] = name
		key := normalizedDirectoryEntryName(name)
		if previous, exists := result.normalized[key]; exists && previous != name {
			result.ambiguous[key] = true
			continue
		}
		result.normalized[key] = name
	}
	return result
}

func normalizedDirectoryEntryName(name string) string {
	return normalizeDirectoryEntryName(name)
}

package serve

import (
	"hash/fnv"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
)

// ignoredDirs are never walked: VCS data, dependencies, our own output and
// build directories.
var ignoredDirs = map[string]bool{
	".git": true, "node_modules": true, ".archmodel": true,
	"build": true, "dist": true, "target": true, ".dart_tool": true, "vendor": true,
}

// watchedExt are the file types that can change the model: Go sources and
// modules, OpenAPI and config YAML, SQL.
var watchedExt = map[string]bool{
	".go": true, ".yaml": true, ".yml": true, ".sql": true, ".mod": true, ".sum": true,
}

// fingerprint hashes the path, mtime and size of every relevant file under the
// given roots. A root may also be a single file (the config file, the
// coverprofile), which is watched whatever its extension. Missing roots are
// skipped, so a config file created later is noticed.
func fingerprint(roots []string) uint64 {
	var entries []string
	seen := map[string]bool{}
	for _, root := range roots {
		info, err := os.Stat(root)
		if err != nil {
			continue
		}
		if !info.IsDir() {
			entries = appendEntry(entries, seen, root, info)
			continue
		}
		filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if p != root && ignoredDirs[d.Name()] {
					return filepath.SkipDir
				}
				return nil
			}
			if !watchedExt[filepath.Ext(p)] {
				return nil
			}
			if fi, err := d.Info(); err == nil {
				entries = appendEntry(entries, seen, p, fi)
			}
			return nil
		})
	}
	sort.Strings(entries)
	h := fnv.New64a()
	for _, e := range entries {
		h.Write([]byte(e))
		h.Write([]byte{0})
	}
	return h.Sum64()
}

func appendEntry(entries []string, seen map[string]bool, p string, fi fs.FileInfo) []string {
	if seen[p] {
		return entries
	}
	seen[p] = true
	return append(entries, p+"|"+strconv.FormatInt(fi.ModTime().UnixNano(), 10)+"|"+strconv.FormatInt(fi.Size(), 10))
}

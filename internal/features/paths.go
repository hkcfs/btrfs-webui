package features

import (
	"path/filepath"
	"strings"
)

// resolvePath resolves symlinks for the deepest existing ancestor of p and
// reappends the not-yet-existing tail. This lets us validate containment for
// paths that do not exist yet.
func resolvePath(p string) string {
	p = filepath.Clean(p)
	cur := p
	var suffix []string
	for {
		if resolved, err := filepath.EvalSymlinks(cur); err == nil {
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return resolved
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return p
		}
		suffix = append(suffix, filepath.Base(cur))
		cur = parent
	}
}

// pathWithin reports whether req is contained inside base after resolving
// symlinks. It rejects "../" traversal, sibling paths such as /dest-evil, and
// symlink escapes out of the backup tree.
func pathWithin(base, req string) bool {
	if base == "" || req == "" {
		return false
	}
	baseR := filepath.Clean(resolvePath(base))
	reqR := filepath.Clean(resolvePath(req))
	return reqR == baseR || strings.HasPrefix(reqR, baseR+string(filepath.Separator))
}

//go:build !linux

package api

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// openNoSymlinkWrite on non-Linux platforms provides directory-handle confinement
// via Go standard library *os.Root.
func openNoSymlinkWrite(rootHandle *os.Root, relOS string) (*os.File, error) {
	// Refuse if destination is a symlink
	if fi, err := rootHandle.Lstat(relOS); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			return nil, httpErr("refusing to overwrite symbolic link")
		}
		if fi.IsDir() {
			return nil, httpErr("destination is not a regular file")
		}
	} else if !errors.Is(err, os.ErrNotExist) && !isPathEscape(err) {
		return nil, fmt.Errorf("could not inspect destination: %w", err)
	}

	if parentDir := filepath.Dir(relOS); parentDir != "." && parentDir != "" {
		if err := rootHandle.MkdirAll(parentDir, 0o755); err != nil {
			if isPathEscape(err) {
				return nil, httpErr("path escapes the working directory")
			}
			return nil, fmt.Errorf("could not create directories: %w", err)
		}
	}

	f, err := rootHandle.OpenFile(relOS, os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		if isPathEscape(err) {
			return nil, httpErr("path escapes the working directory")
		}
		return nil, fmt.Errorf("could not open file: %w", err)
	}

	stat, err := f.Stat()
	if err != nil || !stat.Mode().IsRegular() {
		_ = f.Close()
		return nil, httpErr("destination is not a regular file")
	}
	return f, nil
}

// isFdInGit on non-Linux platforms verifies lexical path containment.
func isFdInGit(f *os.File, rootPath string) bool {
	return isGitPath(f.Name())
}

// copyDir on non-Linux platforms copies files confined under os.Root directory handles.
func copyDir(src, dst string) error {
	srcRoot, err := os.OpenRoot(src)
	if err != nil {
		return err
	}
	defer srcRoot.Close()

	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	dstRoot, err := os.OpenRoot(dst)
	if err != nil {
		return err
	}
	defer dstRoot.Close()

	return fs.WalkDir(srcRoot.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == "." {
			return nil
		}
		if d.Name() == ".git" {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil // skip symlinks on non-Linux
		}
		if d.IsDir() {
			return dstRoot.Mkdir(p, 0o755)
		}
		if d.Type().IsRegular() {
			in, err := srcRoot.Open(p)
			if err != nil {
				return nil
			}
			defer in.Close()

			st, err := in.Stat()
			if err != nil || !st.Mode().IsRegular() {
				return nil
			}

			out, err := dstRoot.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
			if err != nil {
				return nil
			}
			defer out.Close()
			_, _ = io.Copy(out, in)
		}
		return nil
	})
}

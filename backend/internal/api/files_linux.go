//go:build linux

package api

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// openNoSymlinkWrite atomically opens relOS for writing beneath rootHandle
// with the guarantee that NO symbolic links (internal or external, terminal or
// intermediate) are followed during path resolution.
//
// Ordering & Safety Guarantees:
//  1. Parent directories are created/verified using O_DIRECTORY | O_NOFOLLOW.
//  2. Opened with O_NONBLOCK so FIFOs or special files never hang.
//  3. Opened WITHOUT O_TRUNC to prevent truncation of special files, devices, or hardlinks.
//  4. If an existing file has multiple hard links (Nlink > 1), it is unlinked and recreated
//     (break-on-write) to guarantee external shared inodes are never mutated.
//  5. Verifies opened descriptor is a regular file before returning.
//  6. Truncation occurs ONLY after descriptor verification.
func openNoSymlinkWrite(rootHandle *os.Root, relOS string) (*os.File, error) {
	dotFile, err := rootHandle.Open(".")
	if err != nil {
		return nil, fmt.Errorf("could not open root directory handle: %w", err)
	}
	defer dotFile.Close()
	rootFd := int(dotFile.Fd())

	// 1. Ensure all parent directories exist beneath rootFd without traversing symlinks.
	if err := ensureParentDirsNoSymlink(rootFd, relOS); err != nil {
		if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOTDIR) {
			return nil, httpErr("refusing to overwrite symbolic link")
		}
		if errors.Is(err, unix.EXDEV) {
			return nil, httpErr("path escapes the working directory")
		}
		return nil, fmt.Errorf("could not create directories: %w", err)
	}

	// 2. If file already exists and has multiple hard links, break the link before writing
	// so external inodes are guaranteed untouched.
	breakHardlinkIfShared(rootFd, relOS)

	// 3. Atomic openat2 with RESOLVE_BENEATH | RESOLVE_NO_SYMLINKS | RESOLVE_NO_MAGICLINKS.
	// NOTE: O_TRUNC is deliberately omitted here. O_NONBLOCK prevents hanging on FIFOs.
	how := &unix.OpenHow{
		Flags:   uint64(unix.O_WRONLY | unix.O_CREAT | unix.O_NONBLOCK | unix.O_CLOEXEC),
		Mode:    0o644,
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS,
	}

	fd, err := unix.Openat2(rootFd, relOS, how)
	if err == nil {
		var st unix.Stat_t
		if err := unix.Fstat(fd, &st); err != nil || (st.Mode&unix.S_IFMT) != unix.S_IFREG {
			_ = unix.Close(fd)
			return nil, httpErr("destination is not a regular file")
		}
		if st.Nlink != 1 {
			_ = unix.Close(fd)
			return nil, httpErr("destination has multiple hard links")
		}
		// Clear O_NONBLOCK for clean sequential writes
		fl, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
		if err != nil {
			_ = unix.Close(fd)
			return nil, fmt.Errorf("could not get flags: %w", err)
		}
		if _, err := unix.FcntlInt(uintptr(fd), unix.F_SETFL, fl&^unix.O_NONBLOCK); err != nil {
			_ = unix.Close(fd)
			return nil, fmt.Errorf("could not clear non-blocking mode: %w", err)
		}
		return os.NewFile(uintptr(fd), relOS), nil
	}

	if errors.Is(err, unix.ELOOP) {
		return nil, httpErr("refusing to overwrite symbolic link")
	}
	if errors.Is(err, unix.EXDEV) {
		return nil, httpErr("path escapes the working directory")
	}
	if errors.Is(err, unix.ENXIO) || errors.Is(err, unix.EISDIR) {
		return nil, httpErr("destination is not a regular file")
	}

	// 4. Fallback for older Linux kernels (< 5.6) where openat2 returns ENOSYS.
	if errors.Is(err, unix.ENOSYS) {
		return openNoFollowStepByStep(rootFd, relOS)
	}

	return nil, fmt.Errorf("could not open file for writing: %w", err)
}

// breakHardlinkIfShared checks if relOS exists and has Nlink > 1.
// If so, it unlinks relOS so a subsequent create gets a clean, independent inode.
func breakHardlinkIfShared(rootFd int, relOS string) {
	// Stat with AT_SYMLINK_NOFOLLOW to avoid following symlinks
	var stat unix.Stat_t
	if err := unix.Fstatat(rootFd, relOS, &stat, unix.AT_SYMLINK_NOFOLLOW); err == nil {
		if stat.Nlink > 1 && (stat.Mode&unix.S_IFMT) == unix.S_IFREG {
			_ = unix.Unlinkat(rootFd, relOS, 0)
		}
	}
}

// openNoFollowStepByStep implements the secure fallback when openat2 is unavailable.
func openNoFollowStepByStep(rootFd int, relOS string) (*os.File, error) {
	parts := strings.Split(filepath.Clean(relOS), string(filepath.Separator))
	currFd := rootFd
	mustClose := false
	defer func() {
		if mustClose && currFd != rootFd {
			_ = unix.Close(currFd)
		}
	}()

	for i := 0; i < len(parts)-1; i++ {
		comp := parts[i]
		if comp == "." || comp == "" {
			continue
		}
		if comp == ".." {
			return nil, httpErr("path escapes the working directory")
		}
		nextFd, err := unix.Openat(currFd, comp, unix.O_DIRECTORY|unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOTDIR) {
				return nil, httpErr("refusing to overwrite symbolic link")
			}
			return nil, err
		}
		if mustClose {
			_ = unix.Close(currFd)
		}
		currFd = nextFd
		mustClose = true
	}

	finalComp := parts[len(parts)-1]
	if finalComp == ".." || finalComp == "." {
		return nil, httpErr("path escapes the working directory")
	}

	fallbackFd, err := unix.Openat(currFd, finalComp, unix.O_WRONLY|unix.O_CREAT|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o644)
	if err != nil {
		if errors.Is(err, unix.ELOOP) {
			return nil, httpErr("refusing to overwrite symbolic link")
		}
		if errors.Is(err, unix.ENXIO) || errors.Is(err, unix.EISDIR) {
			return nil, httpErr("destination is not a regular file")
		}
		return nil, err
	}
	var st unix.Stat_t
	if err := unix.Fstat(fallbackFd, &st); err != nil || (st.Mode&unix.S_IFMT) != unix.S_IFREG {
		_ = unix.Close(fallbackFd)
		return nil, httpErr("destination is not a regular file")
	}
	if st.Nlink != 1 {
		_ = unix.Close(fallbackFd)
		return nil, httpErr("destination has multiple hard links")
	}
	fl, err := unix.FcntlInt(uintptr(fallbackFd), unix.F_GETFL, 0)
	if err != nil {
		_ = unix.Close(fallbackFd)
		return nil, fmt.Errorf("could not get flags: %w", err)
	}
	if _, err := unix.FcntlInt(uintptr(fallbackFd), unix.F_SETFL, fl&^unix.O_NONBLOCK); err != nil {
		_ = unix.Close(fallbackFd)
		return nil, fmt.Errorf("could not clear non-blocking mode: %w", err)
	}
	return os.NewFile(uintptr(fallbackFd), relOS), nil
}

// ensureParentDirsNoSymlink ensures all parent directories of relPath exist beneath rootFd
// without following any symlinks.
func ensureParentDirsNoSymlink(rootFd int, relPath string) error {
	clean := filepath.Clean(relPath)
	dir := filepath.Dir(clean)
	if dir == "." || dir == "" || dir == string(filepath.Separator) {
		return nil
	}
	parts := strings.Split(dir, string(filepath.Separator))
	currFd := rootFd
	mustClose := false
	defer func() {
		if mustClose && currFd != rootFd {
			_ = unix.Close(currFd)
		}
	}()

	for _, comp := range parts {
		if comp == "." || comp == "" {
			continue
		}
		if comp == ".." {
			return unix.EXDEV
		}

		nextFd, err := unix.Openat(currFd, comp, unix.O_DIRECTORY|unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err == nil {
			if mustClose {
				_ = unix.Close(currFd)
			}
			currFd = nextFd
			mustClose = true
			continue
		}

		if err == unix.ELOOP || err == unix.ENOTDIR {
			return err
		}

		if err := unix.Mkdirat(currFd, comp, 0o755); err != nil && !errors.Is(err, unix.EEXIST) {
			return err
		}

		nextFd, err = unix.Openat(currFd, comp, unix.O_DIRECTORY|unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			return err
		}
		if mustClose {
			_ = unix.Close(currFd)
		}
		currFd = nextFd
		mustClose = true
	}
	return nil
}

// isFdInGit checks whether the open file descriptor resolves to a path inside .git.
// This prevents internal symlinks from reading or writing .git files.
func isFdInGit(f *os.File, rootPath string) bool {
	target, err := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", f.Fd()))
	if err != nil {
		return false
	}
	if realRoot, err := filepath.EvalSymlinks(rootPath); err == nil {
		rootPath = realRoot
	}
	rel, err := filepath.Rel(rootPath, target)
	if err != nil {
		return false
	}
	relSlash := filepath.ToSlash(rel)
	if isGitPath(relSlash) || strings.Contains(relSlash, "/.git/") || strings.HasSuffix(relSlash, "/.git") {
		return true
	}
	if relSlash == ".." || strings.HasPrefix(relSlash, "../") {
		return true
	}
	return false
}

// copyDir recursively copies src into dst beneath directory descriptors
// using strictly descriptor-relative traversal and O_NOFOLLOW opens.
// This guarantees that intermediate directories and source files cannot be
// swapped with symlinks during copy (eliminating TOCTOU symlink traversal).
func copyDir(src, dst string) error {
	srcFd, err := unix.Open(src, unix.O_DIRECTORY|unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("could not open source directory: %w", err)
	}
	defer unix.Close(srcFd)

	if err := os.MkdirAll(dst, 0o755); err != nil {
		return fmt.Errorf("could not create destination directory: %w", err)
	}
	dstFd, err := unix.Open(dst, unix.O_DIRECTORY|unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("could not open destination directory: %w", err)
	}
	defer unix.Close(dstFd)

	return copyDirFd(srcFd, dstFd, "")
}

func copyDirFd(srcFd, dstFd int, relPrefix string) error {
	dupFd, err := unix.Dup(srcFd)
	if err != nil {
		return err
	}
	dirFile := os.NewFile(uintptr(dupFd), "")
	entries, err := dirFile.ReadDir(-1)
	_ = dirFile.Close()
	if err != nil {
		return err
	}

	for _, e := range entries {
		name := e.Name()
		if name == ".git" {
			continue
		}
		currentRel := name
		if relPrefix != "" {
			currentRel = filepath.Join(relPrefix, name)
		}

		switch {
		case e.Type()&os.ModeSymlink != 0:
			// Read symlink relative to srcFd
			target, err := readlinkat(srcFd, name)
			if err != nil {
				continue
			}
			if filepath.IsAbs(target) {
				continue // skip absolute symlinks
			}
			// Verify lexical containment relative to workspace root
			resolved := filepath.Clean(filepath.Join(relPrefix, target))
			if strings.HasPrefix(resolved, "..") || filepath.IsAbs(resolved) {
				continue // escapes root, skip
			}
			_ = unix.Symlinkat(target, dstFd, name)

		case e.Type().IsDir():
			// Open source subdirectory with O_DIRECTORY | O_NOFOLLOW
			subSrcFd, err := unix.Openat(srcFd, name, unix.O_DIRECTORY|unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
			if err != nil {
				// If ELOOP or ENOTDIR, directory was replaced with a symlink or file; skip!
				if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOTDIR) {
					continue
				}
				return err
			}

			_ = unix.Mkdirat(dstFd, name, 0o755)
			subDstFd, err := unix.Openat(dstFd, name, unix.O_DIRECTORY|unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
			if err != nil {
				_ = unix.Close(subSrcFd)
				return err
			}

			err = copyDirFd(subSrcFd, subDstFd, currentRel)
			_ = unix.Close(subSrcFd)
			_ = unix.Close(subDstFd)
			if err != nil {
				return err
			}

		case e.Type().IsRegular():
			// Open source file descriptor-relative with O_NOFOLLOW
			inFd, err := unix.Openat(srcFd, name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
			if err != nil {
				// If ELOOP, file was replaced with a symlink (TOCTOU attempt); skip!
				if errors.Is(err, unix.ELOOP) {
					continue
				}
				return err
			}

			var st unix.Stat_t
			if err := unix.Fstat(inFd, &st); err != nil || (st.Mode&unix.S_IFMT) != unix.S_IFREG {
				_ = unix.Close(inFd)
				continue
			}

			// Open destination file with O_CREAT | O_EXCL | O_NOFOLLOW
			outFd, err := unix.Openat(dstFd, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o644)
			if err != nil {
				_ = unix.Close(inFd)
				continue
			}

			inFile := os.NewFile(uintptr(inFd), "")
			outFile := os.NewFile(uintptr(outFd), "")
			_, copyErr := io.Copy(outFile, inFile)
			_ = inFile.Close()
			_ = outFile.Close()
			if copyErr != nil {
				return copyErr
			}
		}
	}
	return nil
}

func readlinkat(dirfd int, path string) (string, error) {
	buf := make([]byte, 1024)
	for {
		n, err := unix.Readlinkat(dirfd, path, buf)
		if err != nil {
			return "", err
		}
		if n < len(buf) {
			return string(buf[:n]), nil
		}
		buf = make([]byte, len(buf)*2)
	}
}

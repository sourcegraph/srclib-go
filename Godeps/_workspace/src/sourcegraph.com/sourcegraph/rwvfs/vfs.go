// Package rwvfs augments vfs to support write operations.
package rwvfs

import (
	"io"
	"os"
	"path/filepath"
	"syscall"

	"github.com/kr/fs"

	"golang.org/x/tools/godoc/vfs"
)

type FileSystem interface {
	vfs.FileSystem

	// Create creates the named file, truncating it if it already exists.
	Create(path string) (io.WriteCloser, error)

	// Mkdir creates a new directory. If name is already a directory, Mkdir
	// returns an error (that can be detected using os.IsExist).
	Mkdir(name string) error

	// Remove removes the named file or directory.
	Remove(name string) error
}

// MkdirAll creates a directory named path, along with any necessary parents. If
// path is already a directory, MkdirAll does nothing and returns nil.
func MkdirAll(fs FileSystem, path string) error {
	// adapted from os/MkdirAll

	dir, err := fs.Stat(path)
	if err == nil {
		if dir.IsDir() {
			return nil
		}
		return &os.PathError{"mkdir", path, syscall.ENOTDIR}
	}

	i := len(path)
	for i > 0 && os.IsPathSeparator(path[i-1]) {
		i--
	}

	j := i
	for j > 0 && !os.IsPathSeparator(path[j-1]) {
		j--
	}

	if j > 1 {
		err = MkdirAll(fs, path[0:j-1])
		if err != nil {
			return err
		}
	}

	err = fs.Mkdir(path)
	if err != nil {
		dir, err1 := fs.Lstat(path)
		if err1 == nil && dir.IsDir() {
			return nil
		}
		return err
	}
	return nil
}

// Glob returns the names of all files under prefix matching pattern or nil if
// there is no matching file. The syntax of patterns is the same as in
// path/filepath.Match.
func Glob(wfs WalkableFileSystem, prefix, pattern string) (matches []string, err error) {
	walker := fs.WalkFS(filepath.Clean(prefix), wfs)
	for walker.Step() {
		path := walker.Path()
		matched, err := filepath.Match(pattern, path)
		if err != nil {
			return nil, err
		}
		if matched {
			matches = append(matches, path)
		}
	}
	return
}

type WalkableFileSystem interface {
	FileSystem
	Join(elem ...string) string
}

// Walkable creates a walkable VFS by wrapping fs.
func Walkable(fs FileSystem) WalkableFileSystem {
	wfs := walkableFS{fs}
	switch fs.(type) {
	case LinkReader:
		return walkableFSLinkReader{wfs}
	default:
		return wfs
	}
}

type walkableFS struct{ FileSystem }

func (_ walkableFS) Join(elem ...string) string { return filepath.Join(elem...) }

type walkableFSLinkReader struct{ walkableFS }

func (f walkableFSLinkReader) ReadLink(name string) (string, error) {
	return f.FileSystem.(LinkReader).ReadLink(name)
}

var _ LinkReader = walkableFSLinkReader{}

// A LinkReader is a filesystem that supports dereferencing symlinks.
type LinkReader interface {
	// ReadLink returns the destination of the named symbolic link.
	ReadLink(name string) (string, error)
}

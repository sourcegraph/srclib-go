package rwvfs

import (
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/tools/godoc/vfs"
)

// Sub returns an implementation of FileSystem mounted at prefix on the
// underlying fs. If fs doesn't have an existing directory at prefix, you can
// can call Mkdir("/") on the new filesystem to create it.
func Sub(fs FileSystem, prefix string) FileSystem {
	subfs := subFS{fs, prefix}
	switch fs.(type) {
	case LinkReader:
		return subFSLinkReader{subfs}
	default:
		return subfs
	}
}

type subFS struct {
	fs     FileSystem
	prefix string
}

var _ FileSystem = subFS{}

type subFSLinkReader struct{ subFS }

var _ LinkReader = subFSLinkReader{}

func (s subFS) resolve(path string) string {
	return filepath.Join(s.prefix, strings.TrimPrefix(path, "/"))
}

func (s subFS) Lstat(path string) (os.FileInfo, error) { return s.fs.Lstat(s.resolve(path)) }

func (s subFS) Stat(path string) (os.FileInfo, error) { return s.fs.Stat(s.resolve(path)) }

func (s subFSLinkReader) ReadLink(name string) (string, error) {
	dst, err := s.fs.(LinkReader).ReadLink(s.resolve(name))
	if err != nil {
		return dst, err
	}
	return filepath.Rel(s.prefix, dst)
}

func (s subFS) ReadDir(path string) ([]os.FileInfo, error) { return s.fs.ReadDir(s.resolve(path)) }

func (s subFS) String() string { return "sub(" + s.fs.String() + ", " + s.prefix + ")" }

func (s subFS) Open(name string) (vfs.ReadSeekCloser, error) { return s.fs.Open(s.resolve(name)) }

func (s subFS) Create(path string) (io.WriteCloser, error) { return s.fs.Create(s.resolve(path)) }

func (s subFS) Mkdir(name string) error {
	err := s.mkdir(name)
	if os.IsNotExist(err) {
		// Automatically create subFS's prefix dirs they don't exist.
		if osErr, ok := err.(*os.PathError); ok && slash(osErr.Path) == slash(s.prefix) {

			if err := MkdirAll(s.fs, s.prefix); err != nil {
				return err
			}
			return s.mkdir(name)
		}
	}
	return err
}

func (s subFS) mkdir(name string) error { return s.fs.Mkdir(s.resolve(name)) }

func (s subFS) Remove(name string) error { return s.fs.Remove(s.resolve(name)) }

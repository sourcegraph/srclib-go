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
	return &subFS{fs, prefix}
}

type subFS struct {
	fs     FileSystem
	prefix string
}

func (s *subFS) resolve(path string) string {
	return filepath.Join(s.prefix, strings.TrimPrefix(path, "/"))
}

func (s *subFS) Lstat(path string) (os.FileInfo, error) { return s.fs.Lstat(s.resolve(path)) }

func (s *subFS) Stat(path string) (os.FileInfo, error) { return s.fs.Stat(s.resolve(path)) }

func (s *subFS) ReadDir(path string) ([]os.FileInfo, error) { return s.fs.ReadDir(s.resolve(path)) }

func (s *subFS) String() string { return "sub(" + s.fs.String() + ", " + s.prefix + ")" }

func (s *subFS) Open(name string) (vfs.ReadSeekCloser, error) { return s.fs.Open(s.resolve(name)) }

func (s *subFS) Create(path string) (io.WriteCloser, error) { return s.fs.Create(s.resolve(path)) }

func (s *subFS) Mkdir(name string) error { return s.fs.Mkdir(s.resolve(name)) }

func (s *subFS) Remove(name string) error { return s.fs.Remove(s.resolve(name)) }

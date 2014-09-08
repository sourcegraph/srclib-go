package rwvfs

import (
	"io"
	"os"
	pathpkg "path"
	"path/filepath"
	"strings"
	"time"

	"code.google.com/p/go.tools/godoc/vfs"
	"code.google.com/p/go.tools/godoc/vfs/mapfs"
)

// Map returns a new FileSystem from the provided map. Map keys should be
// forward slash-separated pathnames and not contain a leading slash.
func Map(m map[string]string) FileSystem {
	return mapFS{m, map[string]struct{}{"": struct{}{}}, mapfs.New(m)}
}

type mapFS struct {
	m    map[string]string
	dirs map[string]struct{}
	vfs.FileSystem
}

func (mfs mapFS) Create(path string) (io.WriteCloser, error) {
	// Mimic behavior of OS filesystem: truncate to empty string upon creation;
	// immediately update string values with writes.
	path = filename(path)
	mfs.m[path] = ""
	return &mapFile{mfs.m, path}, nil
}

func filename(p string) string {
	if p == "." {
		return "/"
	}
	return strings.TrimPrefix(p, "/")
}

// slashdir returns path.Dir(p), but special-cases paths not beginning
// with a slash to be in the root.
func slashdir(p string) string {
	d := pathpkg.Dir(p)
	if d == "." {
		return "/"
	}
	if strings.HasPrefix(p, "/") {
		return d
	}
	return "/" + d
}

type mapFile struct {
	m    map[string]string
	path string
}

func (f *mapFile) Write(p []byte) (int, error) {
	f.m[f.path] = f.m[f.path] + string(p)
	return len(p), nil
}

func (f mapFile) Close() error { return nil }

func (mfs mapFS) Lstat(p string) (os.FileInfo, error) {
	// proxy mapfs.mapFS.Lstat to not return errors for empty directories
	// created with Mkdir
	p = filename(p)
	fi, err := mfs.FileSystem.Lstat(p)
	if os.IsNotExist(err) {
		_, ok := mfs.dirs[p]
		if ok {
			return mapFI{name: pathpkg.Base(p), dir: true}, nil
		}
	}
	return fi, err
}

func (mfs mapFS) Stat(p string) (os.FileInfo, error) {
	return mfs.Lstat(p)
}

func dirInfo(name string) os.FileInfo {
	return mapFI{name: pathpkg.Base(name), dir: true}
}

func fileInfo(name, contents string) os.FileInfo {
	return mapFI{name: pathpkg.Base(name), size: len(contents)}
}

func (mfs mapFS) ReadDir(p string) ([]os.FileInfo, error) {
	// proxy mapfs.mapFS.ReadDir to not return errors for empty directories
	// created with Mkdir
	p = filename(p)
	fis, err := mfs.FileSystem.ReadDir(p)
	if os.IsNotExist(err) {
		_, ok := mfs.dirs[p]
		if ok {
			// return a list of subdirs and files (the underlying ReadDir impl
			// fails here because it thinks the directories don't exist).
			fis = nil
			for dir, _ := range mfs.dirs {
				if filepath.Dir(dir) == p {
					fis = append(fis, dirInfo(dir))
				}
			}
			for fn, b := range mfs.m {
				if slashdir(fn) == "/"+p {
					fis = append(fis, fileInfo(fn, b))
				}
			}
			return fis, nil
		}
	}
	return fis, err
}

func (mfs mapFS) Mkdir(name string) error {
	name = filename(name)
	_, err := mfs.Stat(slashdir(name))
	if os.IsNotExist(err) {
		return err
	}
	fi, _ := mfs.Stat(name)
	if fi != nil {
		return os.ErrExist
	}
	mfs.dirs[name] = struct{}{}
	return nil
}

func (mfs mapFS) Remove(name string) error {
	name = filename(name)
	delete(mfs.dirs, name)
	delete(mfs.m, name)
	return nil
}

// mapFI is the map-based implementation of FileInfo.
type mapFI struct {
	name string
	size int
	dir  bool
}

func (fi mapFI) IsDir() bool        { return fi.dir }
func (fi mapFI) ModTime() time.Time { return time.Time{} }
func (fi mapFI) Mode() os.FileMode {
	if fi.IsDir() {
		return 0755 | os.ModeDir
	}
	return 0444
}
func (fi mapFI) Name() string     { return pathpkg.Base(fi.name) }
func (fi mapFI) Size() int64      { return int64(fi.size) }
func (fi mapFI) Sys() interface{} { return nil }

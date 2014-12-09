package rwvfs

import (
	"io"
	"os"
	pathpkg "path"
	"path/filepath"
	"strings"

	"golang.org/x/tools/godoc/vfs"
	"golang.org/x/tools/godoc/vfs/mapfs"
)

// Map returns a new FileSystem from the provided map. Map keys should be
// forward slash-separated pathnames and not contain a leading slash.
func Map(m map[string]string) FileSystem {
	fs := mapFS{
		m:          m,
		dirs:       map[string]struct{}{"": struct{}{}},
		FileSystem: mapfs.New(m),
	}

	// Create initial dirs.
	for path := range m {
		if err := MkdirAll(fs, filepath.Dir(path)); err != nil {
			panic(err.Error())
		}
	}

	return fs
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

func slash(p string) string {
	if p == "." {
		return "/"
	}
	return "/" + strings.TrimPrefix(p, "/")
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

func (mfs mapFS) lstat(p string) (os.FileInfo, error) {
	// proxy mapfs.mapFS.Lstat to not return errors for empty directories
	// created with Mkdir
	p = filename(p)
	fi, err := mfs.FileSystem.Lstat(p)
	if os.IsNotExist(err) {
		_, ok := mfs.dirs[p]
		if ok {
			return fileInfo{name: pathpkg.Base(p), dir: true}, nil
		}
	}
	return fi, err
}

func (mfs mapFS) Lstat(p string) (os.FileInfo, error) {
	fi, err := mfs.lstat(p)
	if err != nil {
		err = &os.PathError{Op: "lstat", Path: p, Err: err}
	}
	return fi, err
}

func (mfs mapFS) Stat(p string) (os.FileInfo, error) {
	fi, err := mfs.lstat(p)
	if err != nil {
		err = &os.PathError{Op: "stat", Path: p, Err: err}
	}
	return fi, err
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
				if filepath.Dir(dir) == p || (p == "" && filepath.Dir(dir) == "." && dir != "." && dir != "") {
					fis = append(fis, newDirInfo(dir))
				}
			}
			for fn, b := range mfs.m {
				if slashdir(fn) == "/"+p {
					fis = append(fis, newFileInfo(fn, b))
				}
			}
			return fis, nil
		}
	}
	return fis, err
}

func fileInfoNames(fis []os.FileInfo) []string {
	names := make([]string, len(fis))
	for i, fi := range fis {
		names[i] = fi.Name()
	}
	return names
}

func (mfs mapFS) Mkdir(name string) error {
	name = filename(name)
	if _, err := mfs.Stat(slashdir(name)); err != nil {
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

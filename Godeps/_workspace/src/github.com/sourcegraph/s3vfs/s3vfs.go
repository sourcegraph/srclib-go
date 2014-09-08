package s3vfs

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net/url"
	"os"
	pathpkg "path"
	"path/filepath"
	"time"

	"code.google.com/p/go.tools/godoc/vfs"

	"github.com/sourcegraph/rwvfs"
	"github.com/sqs/s3"
	"github.com/sqs/s3/s3util"
)

var DefaultS3Config = s3util.Config{
	Keys: &s3.Keys{
		AccessKey: os.Getenv("AWS_ACCESS_KEY_ID"),
		SecretKey: os.Getenv("AWS_SECRET_KEY"),
	},
	Service: s3.DefaultService,
}

// S3 returns an implementation of FileSystem using the specified S3 bucket and
// config. If config is nil, DefaultS3Config is used.
//
// The bucket URL is the full URL to the bucket on Amazon S3, including the
// bucket name and AWS region (e.g.,
// https://s3-us-west-2.amazonaws.com/mybucket).
func S3(bucket *url.URL, config *s3util.Config) rwvfs.FileSystem {
	if config == nil {
		config = &DefaultS3Config
	}
	return &S3FS{bucket, config}
}

type S3FS struct {
	bucket *url.URL
	config *s3util.Config
}

func (fs *S3FS) String() string {
	return fmt.Sprintf("S3 filesystem at %s", fs.bucket)
}

func (fs *S3FS) url(path string) string {
	path = pathpkg.Join(fs.bucket.Path, path)
	return fs.bucket.ResolveReference(&url.URL{Path: path}).String()
}

func (fs *S3FS) Open(name string) (vfs.ReadSeekCloser, error) {
	rdr, err := s3util.Open(fs.url(name), fs.config)
	if err != nil {
		return nil, err
	}

	b, err := ioutil.ReadAll(rdr)
	if err != nil {
		return nil, err
	}

	return nopCloser{bytes.NewReader(b)}, nil
}

func (fs *S3FS) ReadDir(path string) ([]os.FileInfo, error) {
	dir, err := s3util.NewFile(fs.url(path), fs.config)
	if err != nil {
		return nil, err
	}

	fis, err := dir.Readdir(0)
	if err != nil {
		return nil, err
	}
	for i, fi := range fis {
		fis[i] = &fileInfo{
			name:    pathpkg.Base(fi.Name()),
			size:    fi.Size(),
			mode:    fi.Mode(),
			modTime: fi.ModTime(),
			sys:     fi.Sys(),
		}
	}
	return fis, nil
}

func (fs *S3FS) Lstat(name string) (os.FileInfo, error) {
	name = filepath.Clean(name)

	if name == "." {
		return &fileInfo{
			name:    ".",
			size:    0,
			mode:    os.ModeDir,
			modTime: time.Time{},
		}, nil
	}

	fis, err := fs.ReadDir(pathpkg.Dir(name))
	if err != nil {
		return nil, err
	}
	for _, fi := range fis {
		if fi.Name() == pathpkg.Base(name) {
			return fi, nil
		}
	}
	return nil, os.ErrNotExist
}

func (fs *S3FS) Stat(name string) (os.FileInfo, error) {
	return fs.Lstat(name)
}

// Create opens the file at path for writing, creating the file if it doesn't
// exist and truncating it otherwise.
func (fs *S3FS) Create(path string) (io.WriteCloser, error) {
	return s3util.Create(fs.url(path), nil, fs.config)
}

func (fs *S3FS) Mkdir(name string) error {
	// S3 doesn't have directories.
	return nil
}

func (fs *S3FS) Remove(name string) error {
	rdr, err := s3util.Delete(fs.url(name), fs.config)
	if rdr != nil {
		rdr.Close()
	}
	return err
}

type nopCloser struct {
	io.ReadSeeker
}

func (nc nopCloser) Close() error { return nil }

type fileInfo struct {
	name    string
	size    int64
	mode    os.FileMode
	modTime time.Time
	sys     interface{}
}

func (f *fileInfo) Name() string       { return f.name }
func (f *fileInfo) Size() int64        { return f.size }
func (f *fileInfo) Mode() os.FileMode  { return f.mode }
func (f *fileInfo) ModTime() time.Time { return f.modTime }
func (f *fileInfo) IsDir() bool        { return f.mode&os.ModeDir != 0 }
func (f *fileInfo) Sys() interface{}   { return f.sys }

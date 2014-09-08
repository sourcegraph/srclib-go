package s3vfs

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/sourcegraph/rwvfs"
)

func TestS3VFS(t *testing.T) {
	// Requires the test bucket to exist.
	//   export S3_TEST_BUCKET_URL=https://s3-us-west-2.amazonaws.com/rwvfs-test-sqs
	s3URL, _ := url.Parse(os.Getenv("S3_TEST_BUCKET_URL"))

	tests := []struct {
		fs   rwvfs.FileSystem
		path string
	}{
		{S3(s3URL, nil), "/foo"},
	}
	for _, test := range tests {
		testWrite(t, test.fs, test.path)
		testGlob(t, test.fs)
	}
}

func testGlob(t *testing.T, fs rwvfs.FileSystem) {
	label := fmt.Sprintf("%T", fs)

	files := []string{"x/y/0.txt", "x/y/1.txt", "x/2.txt"}
	for _, file := range files {
		err := rwvfs.MkdirAll(fs, filepath.Dir(file))
		if err != nil {
			t.Fatalf("%s: MkdirAll: %s", label, err)
		}
		w, err := fs.Create(file)
		if err != nil {
			t.Errorf("%s: Create(%q): %s", label, file, err)
			return
		}
		_, err = w.Write([]byte("x"))
		if err != nil {
			t.Fatal(err)
		}
		err = w.Close()
		if err != nil {
			t.Fatal(err)
		}
	}

	globTests := []struct {
		prefix  string
		pattern string
		matches []string
	}{
		{"", "x/y/*.txt", []string{"x/y/0.txt", "x/y/1.txt"}},
		{"x/y", "x/y/*.txt", []string{"x/y/0.txt", "x/y/1.txt"}},
		{"", "x/*", []string{"x/y", "x/2.txt"}},
	}
	for _, test := range globTests {
		matches, err := rwvfs.Glob(walkableFileSystem{fs}, test.prefix, test.pattern)
		if err != nil {
			t.Errorf("%s: Glob(prefix=%q, pattern=%q): %s", label, test.prefix, test.pattern, err)
			continue
		}
		sort.Strings(test.matches)
		sort.Strings(matches)
		if !reflect.DeepEqual(matches, test.matches) {
			t.Errorf("%s: Glob(prefix=%q, pattern=%q): got %v, want %v", label, test.prefix, test.pattern, matches, test.matches)
		}
	}
}

func testWrite(t *testing.T, fs rwvfs.FileSystem, path string) {
	label := fmt.Sprintf("%T", fs)

	w, err := fs.Create(path)
	if err != nil {
		t.Fatalf("%s: WriterOpen: %s", label, err)
	}

	input := []byte("qux")
	_, err = w.Write(input)
	if err != nil {
		t.Fatalf("%s: Write: %s", label, err)
	}

	err = w.Close()
	if err != nil {
		t.Fatalf("%s: w.Close: %s", label, err)
	}

	var r io.ReadCloser
	r, err = fs.Open(path)
	if err != nil {
		t.Fatalf("%s: Open: %s", label, err)
	}
	var output []byte
	output, err = ioutil.ReadAll(r)
	if err != nil {
		t.Fatalf("%s: ReadAll: %s", label, err)
	}
	if !bytes.Equal(output, input) {
		t.Errorf("%s: got output %q, want %q", label, output, input)
	}

	r, err = fs.Open(path)
	if err != nil {
		t.Fatalf("%s: Open: %s", label, err)
	}
	output, err = ioutil.ReadAll(r)
	if err != nil {
		t.Fatalf("%s: ReadAll: %s", label, err)
	}
	if !bytes.Equal(output, input) {
		t.Errorf("%s: got output %q, want %q", label, output, input)
	}

	err = fs.Remove(path)
	if err != nil {
		t.Errorf("%s: Remove(%q): %s", label, path, err)
	}
	testPathDoesNotExist(t, label, fs, path)
}

func testPathDoesNotExist(t *testing.T, label string, fs rwvfs.FileSystem, path string) {
	fi, err := fs.Stat(path)
	if err != nil && !os.IsNotExist(err) {
		t.Errorf("%s: Stat(%q): want os.IsNotExist-satisfying error, got %q", label, path, err)
	} else if err == nil {
		t.Errorf("%s: Stat(%q): want file to not exist, got existing file with FileInfo %+v", label, path, fi)
	}
}

type walkableFileSystem struct{ rwvfs.FileSystem }

func (_ walkableFileSystem) Join(elem ...string) string { return filepath.Join(elem...) }

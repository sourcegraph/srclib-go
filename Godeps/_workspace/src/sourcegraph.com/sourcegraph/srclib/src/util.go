package src

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"sourcegraph.com/sourcegraph/srclib/buildstore"
	"sourcegraph.com/sourcegraph/srclib/unit"
	"sourcegraph.com/sourcegraph/srclib/util"
)

func isDir(dir string) bool {
	di, err := os.Stat(dir)
	return err == nil && di.IsDir()
}

func isFile(file string) bool {
	fi, err := os.Stat(file)
	return err == nil && fi.Mode().IsRegular()
}

func firstLine(s string) string {
	i := strings.Index(s, "\n")
	if i == -1 {
		return s
	}
	return s[:i]
}

func cmdOutput(c ...string) string {
	cmd := exec.Command(c[0], c[1:]...)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		log.Fatalf("%v: %s", c, err)
	}
	return strings.TrimSpace(string(out))
}

func execCmd(prog string, arg ...string) error {
	cmd := exec.Command(prog, arg...)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stderr
	log.Println("Running ", cmd.Args)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("command %q failed: %s", cmd.Args, err)
	}
	return nil
}

func SourceUnitMatchesArgs(specified []string, u *unit.SourceUnit) bool {
	var match bool
	if len(specified) == 0 {
		match = true
	} else {
		for _, unitSpec := range specified {
			if string(u.ID()) == unitSpec || u.Name == unitSpec {
				match = true
				break
			}
		}
	}

	return match
}

func PrintJSON(v interface{}, prefix string) {
	data, err := json.MarshalIndent(v, prefix, "  ")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(string(data))
}

func OpenInputFiles(extraArgs []string) map[string]io.ReadCloser {
	inputs := make(map[string]io.ReadCloser)
	if len(extraArgs) == 0 {
		inputs["<stdin>"] = os.Stdin
	} else {
		for _, name := range extraArgs {
			f, err := os.Open(name)
			if err != nil {
				log.Fatal(err)
			}
			inputs[name] = f
		}
	}
	return inputs
}

func CloseAll(files map[string]io.ReadCloser) {
	for _, rc := range files {
		rc.Close()
	}
}

// updateVCSIgnore adds .srclib-cache/ to the user's .${VCS}ignore file in
// their home directory.
func updateVCSIgnore(name string) {
	homeDir := util.CurrentUserHomeDir()

	entry := buildstore.BuildDataDirName + "/"

	path := filepath.Join(homeDir, name)
	data, err := ioutil.ReadFile(path)
	if os.IsNotExist(err) {
		err = nil
	} else if bytes.Contains(data, []byte("\n"+entry+"\n")) {
		// already has entry
		return
	}

	data = append(data, []byte("\n\n# srclib build cache\n"+entry+"\n")...)
	err = ioutil.WriteFile(path, data, 0700)
	if err != nil {
		log.Fatal(err)
	}
}

func readJSONFile(file string, v interface{}) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewDecoder(f).Decode(v)
}

func bytesString(s uint64) string {
	sizes := []string{"B", "KB", "MB", "GB", "TB", "PB", "EB"}
	if s < 10 {
		return fmt.Sprintf("%dB", s)
	}
	logn := func(n, b float64) float64 {
		return math.Log(n) / math.Log(b)
	}
	e := math.Floor(logn(float64(s), 1000))
	suffix := sizes[int(e)]
	val := math.Floor(float64(s)/math.Pow(1000, math.Floor(e))*10+0.5) / 10
	f := "%.0f"
	if val < 10 {
		f = "%.1f"
	}
	return fmt.Sprintf(f+"%s", val, suffix)
}

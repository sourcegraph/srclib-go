package main

import (
	"fmt"
	"path/filepath"
	"strings"
)

type config struct {
	// GoBaseImportPath corresponds to the GoBaseImportPath Srcfile config
	// property. The keys are the DIRs and the values are the
	// IMPORT-PATH-PREFIXes.
	GoBaseImportPath map[string]string
}

// parseConfig is called on the Config []string field of the Srcfile.
func parseConfig(propStrs []string) (*config, error) {
	c := config{}
	for _, propStr := range propStrs {
		if directive := "GoBaseImportPath:"; strings.HasPrefix(propStr, directive) {
			dir, ipp, err := splitConfigProperty(propStr[len(directive):])
			if err != nil {
				return nil, err
			}

			dir = filepath.Clean(dir)
			ipp = filepath.Clean(ipp)
			if c.GoBaseImportPath == nil {
				c.GoBaseImportPath = map[string]string{}
			}
			c.GoBaseImportPath[dir] = ipp
		}
	}

	return &c, nil
}

func splitConfigProperty(s string) (string, string, error) {
	if i := strings.Index(s, "="); i != -1 {
		return s[:i], s[i+1:], nil
	}
	return "", "", fmt.Errorf("expected property in the form KEY=VAL, got %q", s)
}

func pathHasPrefix(path, prefix string) bool {
	return prefix == "." || path == prefix || strings.HasPrefix(path, prefix+"/")
}

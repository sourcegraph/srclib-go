package depresolve_test

import (
	"reflect"
	"testing"

	"sourcegraph.com/sourcegraph/srclib-go/depresolve"
	"sourcegraph.com/sourcegraph/srclib/dep"
)

// TestResolveImportPath tests the behavior of ResolveImportPath
// when called on some common Go package import paths.
func TestResolveImportPath(t *testing.T) {
	if testing.Short() {
		t.Skip("short specified, skipping since accesses network")
	}

	tests := []struct {
		ImportPath string
		Result     *dep.ResolvedTarget
	}{
		{"k8s.io/kubernetes/pkg/api", &dep.ResolvedTarget{"https://github.com/kubernetes/kubernetes", "github.com/kubernetes/kubernetes/pkg/api", "GoPackage", "", ""}},
		{"gopkg.in/inconshreveable/log15.v2", &dep.ResolvedTarget{"https://gopkg.in/inconshreveable/log15.v2", "gopkg.in/inconshreveable/log15.v2", "GoPackage", "", ""}},
		{"azul3d.org/semver.v2", &dep.ResolvedTarget{"https://azul3d.org/semver.v2", "azul3d.org/semver.v2", "GoPackage", "", ""}},
		{"sourcegraph.com/sourcegraph/srclib/graph", &dep.ResolvedTarget{"https://github.com/sourcegraph/srclib", "github.com/sourcegraph/srclib/graph", "GoPackage", "", ""}},
		{"sourcegraph.com/sourcegraph/sourcegraph/app", &dep.ResolvedTarget{"https://github.com/sourcegraph/sourcegraph", "github.com/sourcegraph/sourcegraph/app", "GoPackage", "", ""}},
		{"google.golang.org/grpc", &dep.ResolvedTarget{"https://github.com/grpc/grpc-go", "github.com/grpc/grpc-go", "GoPackage", "", ""}},
		{"google.golang.org/grpc/codes", &dep.ResolvedTarget{"https://github.com/grpc/grpc-go", "github.com/grpc/grpc-go/codes", "GoPackage", "", ""}},
		{"google.golang.org/appengine", &dep.ResolvedTarget{"https://github.com/golang/appengine", "github.com/golang/appengine", "GoPackage", "", ""}},
		{"google.golang.org/appengine/channel", &dep.ResolvedTarget{"https://github.com/golang/appengine", "github.com/golang/appengine/channel", "GoPackage", "", ""}},
		{"google.golang.org/cloud", &dep.ResolvedTarget{"https://github.com/GoogleCloudPlatform/gcloud-golang", "github.com/GoogleCloudPlatform/gcloud-golang", "GoPackage", "", ""}},
		{"google.golang.org/cloud/bigtable", &dep.ResolvedTarget{"https://github.com/GoogleCloudPlatform/gcloud-golang", "github.com/GoogleCloudPlatform/gcloud-golang/bigtable", "GoPackage", "", ""}},
		{"google.golang.org/api", &dep.ResolvedTarget{"https://github.com/google/google-api-go-client", "github.com/google/google-api-go-client", "GoPackage", "", ""}},
		{"google.golang.org/api/analytics/v3", &dep.ResolvedTarget{"https://github.com/google/google-api-go-client", "github.com/google/google-api-go-client/analytics/v3", "GoPackage", "", ""}},
		{"golang.org/x/net/context", &dep.ResolvedTarget{"https://github.com/golang/net", "github.com/golang/net/context", "GoPackage", "", ""}},
		{"golang.org/x/crypto/ssh", &dep.ResolvedTarget{"https://github.com/golang/crypto", "github.com/golang/crypto/ssh", "GoPackage", "", ""}},
		{"gopkg.in/redis.v3/internal/hashtag", &dep.ResolvedTarget{"https://gopkg.in/redis.v3", "gopkg.in/redis.v3/internal/hashtag", "GoPackage", "", ""}},
		{"github.com/gorilla/mux", &dep.ResolvedTarget{"https://github.com/gorilla/mux", "github.com/gorilla/mux", "GoPackage", "", ""}},
		{"github.com/aws/aws-sdk-go/aws/request", &dep.ResolvedTarget{"https://github.com/aws/aws-sdk-go", "github.com/aws/aws-sdk-go/aws/request", "GoPackage", "", ""}},
	}
	for _, test := range tests {
		got, err := depresolve.ResolveImportPath(test.ImportPath)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, test.Result) {
			t.Errorf("failed:\ngot : %#v\nwant: %#v", got, test.Result)
		}
	}
}

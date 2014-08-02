all: .srclib-cache/eacc42ecac5450536cc30e8a7a2b047e16c1ffc9/sourcegraph.com/sourcegraph/srclib-go/testdata/case/go-sample-0/mypkg/GoPackage.graph.json .srclib-cache/eacc42ecac5450536cc30e8a7a2b047e16c1ffc9/sourcegraph.com/sourcegraph/srclib-go/testdata/case/go-sample-0/mypkg/subpkg/GoPackage.graph.json .srclib-cache/eacc42ecac5450536cc30e8a7a2b047e16c1ffc9/sourcegraph.com/sourcegraph/srclib-go/testdata/case/go-sample-0/mypkg/GoPackage.depresolve.json .srclib-cache/eacc42ecac5450536cc30e8a7a2b047e16c1ffc9/sourcegraph.com/sourcegraph/srclib-go/testdata/case/go-sample-0/mypkg/subpkg/GoPackage.depresolve.json

.srclib-cache/eacc42ecac5450536cc30e8a7a2b047e16c1ffc9/sourcegraph.com/sourcegraph/srclib-go/testdata/case/go-sample-0/mypkg/GoPackage.graph.json: .srclib-cache/eacc42ecac5450536cc30e8a7a2b047e16c1ffc9/sourcegraph.com/sourcegraph/srclib-go/testdata/case/go-sample-0/mypkg/GoPackage.unit.json mypkg/bar.go mypkg/foo.go
	src tool --methods program "sourcegraph.com/sourcegraph/srclib-go" "graph" < $^ | src internal normalize-graph-data 1> $@

.srclib-cache/eacc42ecac5450536cc30e8a7a2b047e16c1ffc9/sourcegraph.com/sourcegraph/srclib-go/testdata/case/go-sample-0/mypkg/subpkg/GoPackage.graph.json: .srclib-cache/eacc42ecac5450536cc30e8a7a2b047e16c1ffc9/sourcegraph.com/sourcegraph/srclib-go/testdata/case/go-sample-0/mypkg/subpkg/GoPackage.unit.json mypkg/subpkg/subfoo.go
	src tool --methods program "sourcegraph.com/sourcegraph/srclib-go" "graph" < $^ | src internal normalize-graph-data 1> $@

.srclib-cache/eacc42ecac5450536cc30e8a7a2b047e16c1ffc9/sourcegraph.com/sourcegraph/srclib-go/testdata/case/go-sample-0/mypkg/GoPackage.depresolve.json: .srclib-cache/eacc42ecac5450536cc30e8a7a2b047e16c1ffc9/sourcegraph.com/sourcegraph/srclib-go/testdata/case/go-sample-0/mypkg/GoPackage.unit.json
	src tool --methods program "sourcegraph.com/sourcegraph/srclib-go" "depresolve" < $^ 1> $@

.srclib-cache/eacc42ecac5450536cc30e8a7a2b047e16c1ffc9/sourcegraph.com/sourcegraph/srclib-go/testdata/case/go-sample-0/mypkg/subpkg/GoPackage.depresolve.json: .srclib-cache/eacc42ecac5450536cc30e8a7a2b047e16c1ffc9/sourcegraph.com/sourcegraph/srclib-go/testdata/case/go-sample-0/mypkg/subpkg/GoPackage.unit.json
	src tool --methods program "sourcegraph.com/sourcegraph/srclib-go" "depresolve" < $^ 1> $@

.DELETE_ON_ERROR:

.srclib-cache/eacc42ecac5450536cc30e8a7a2b047e16c1ffc9/github.com/sgtest/go-sample-0/mypkg/GoPackage.depresolve.json: .srclib-cache/eacc42ecac5450536cc30e8a7a2b047e16c1ffc9/github.com/sgtest/go-sample-0/mypkg/GoPackage.unit.v0.json
	src tool -m docker "github.com/sourcegraph/srclib-go" "depresolve" < $^  1> $@

.srclib-cache/eacc42ecac5450536cc30e8a7a2b047e16c1ffc9/github.com/sgtest/go-sample-0/mypkg/GoPackage.graph.json: .srclib-cache/eacc42ecac5450536cc30e8a7a2b047e16c1ffc9/github.com/sgtest/go-sample-0/mypkg/GoPackage.unit.v0.json
	src tool -m docker "github.com/sourcegraph/srclib-go" "graph" < $^ | src internal normalize-graph-data 1> $@

.srclib-cache/eacc42ecac5450536cc30e8a7a2b047e16c1ffc9/github.com/sgtest/go-sample-0/mypkg/subpkg/GoPackage.depresolve.json: .srclib-cache/eacc42ecac5450536cc30e8a7a2b047e16c1ffc9/github.com/sgtest/go-sample-0/mypkg/subpkg/GoPackage.unit.v0.json
	src tool -m docker "github.com/sourcegraph/srclib-go" "depresolve" < $^  1> $@

.srclib-cache/eacc42ecac5450536cc30e8a7a2b047e16c1ffc9/github.com/sgtest/go-sample-0/mypkg/subpkg/GoPackage.graph.json: .srclib-cache/eacc42ecac5450536cc30e8a7a2b047e16c1ffc9/github.com/sgtest/go-sample-0/mypkg/subpkg/GoPackage.unit.v0.json
	src tool -m docker "github.com/sourcegraph/srclib-go" "graph" < $^ | src internal normalize-graph-data 1> $@

all: .srclib-cache/eacc42ecac5450536cc30e8a7a2b047e16c1ffc9/github.com/sgtest/go-sample-0/mypkg/GoPackage.depresolve.json .srclib-cache/eacc42ecac5450536cc30e8a7a2b047e16c1ffc9/github.com/sgtest/go-sample-0/mypkg/GoPackage.graph.json .srclib-cache/eacc42ecac5450536cc30e8a7a2b047e16c1ffc9/github.com/sgtest/go-sample-0/mypkg/subpkg/GoPackage.depresolve.json .srclib-cache/eacc42ecac5450536cc30e8a7a2b047e16c1ffc9/github.com/sgtest/go-sample-0/mypkg/subpkg/GoPackage.graph.json

.DELETE_ON_ERROR:

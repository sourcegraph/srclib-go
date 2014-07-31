# srclib-go [![Build Status](https://travis-ci.org/sourcegraph/srclib-go.png?branch=master)](https://travis-ci.org/sourcegraph/srclib-go)

## Tests

Testing this toolchain requires that you have installed `src` from
[srclib](https://sourcegraph.com/sourcegraph/srclib) and that you have this
toolchain set up. See srclib documentation for more information.

To test this toolchain's output against the expected output, run:

```
src test
```

By default, that command runs tests in an isolated Docker container. To run the
tests on your local machine, run `src test -m program`. See the srclib
documentation for more information about the differences between these two
execution methods.

NOTE: The test expectation files are based on the output of Go 1.3 `go list
-json`. This is different from Go 1.2, so the tests won't pass on Go 1.2 (but
the functionality seems fine).

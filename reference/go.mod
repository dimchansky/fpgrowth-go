// This nested module holds development-only tooling: the pinned upstream
// C sources, the scripts that build them into a reference executable, and
// the differential test suite that compares the Go port against it.
// Being a separate module keeps all of it out of the published
// github.com/dimchansky/fpgrowth-go module and out of `go test ./...`
// run from the repository root. The differential tests build the root
// module's CLI from ../cmd/fpgrowth and do not import it as a package.
module github.com/dimchansky/fpgrowth-go/reference

go 1.26.0

toolchain go1.27.2


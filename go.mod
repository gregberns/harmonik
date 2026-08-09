module github.com/gregberns/harmonik

go 1.25

// The stdlib `net` package in go1.26.1 and earlier trips govulncheck
// (GO-2026-4971), which fails `make module-hygiene` and therefore blocks every
// merge. go1.26.3 carries the fix. This line pins the floor so the gate result
// does not depend on which Go each box happens to have installed.
toolchain go1.26.3

require github.com/google/uuid v1.6.0

require (
	github.com/expr-lang/expr v1.17.8
	github.com/stretchr/testify v1.11.1
	go.uber.org/goleak v1.3.0
	gopkg.in/yaml.v3 v3.0.1
	pgregory.net/rapid v1.3.0
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
)

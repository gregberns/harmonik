module github.com/gregberns/harmonik/kernel

go 1.25

require (
	github.com/gregberns/harmonik/contract v0.0.0-00010101000000-000000000000
	google.golang.org/protobuf v1.36.6
)

require (
	golang.org/x/net v0.38.0 // indirect
	golang.org/x/sys v0.31.0 // indirect
	golang.org/x/text v0.23.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250324211829-b45e905df463 // indirect
	google.golang.org/grpc v1.73.0 // indirect
)

// A workspace member's own require still needs a resolvable version; go.work
// stitches the actual directory in, but MVS parses this string first, so it
// must be a real replace target, not just a placeholder.
replace github.com/gregberns/harmonik/contract => ../contract

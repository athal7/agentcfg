package cli

// version is overridden at build time via
// -ldflags "-X github.com/athal7/agentcfg/internal/cli.version=vX.Y.Z"
// (goreleaser's default Go build hook does this automatically from the
// git tag). Local `go build`/`go run` invocations get "dev".
var version = "dev"

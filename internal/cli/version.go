package cli

// Version is the current version of agent-notify.
// This value is set to "dev" by default and is overridden at build time via ldflags:
// go build -ldflags="-X github.com/flashzzw/agent-notify/internal/cli.Version=v0.2.7"
var Version = "dev"

package packagingworkflow

// Pins for the localhost proof. The Go SDK is the module in go.mod. The CLI
// archive is fetched by scripts/fetch-temporal-cli.sh and checked against
// CLIArchiveSHA256 before any demo or recovery test runs it. The server and UI
// versions are the ones printed by `temporal version 1.10.0`.
const (
	SDKModule        = "go.temporal.io/sdk"
	SDKVersion       = "v1.51.0"
	CLIVersion       = "1.10.0"
	ServerVersion    = "1.32.2"
	UIVersion        = "2.56.1"
	CLIArchiveURL    = "https://github.com/temporalio/cli/releases/download/v1.10.0/temporal_cli_1.10.0_linux_amd64.tar.gz"
	CLIArchiveSHA256 = "521f461f3d108902763bb71e85cd0fd382dc9357b59604ae03da6ae734d16890"
)

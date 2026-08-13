module github.com/jonathanagustin/conduit

go 1.26.0

// Pins the compiler CI and local builds use to the same fixed release the
// digest-pinned release images already build with. Without it, the `go`
// directive alone selects 1.26.0, which carries reachable crypto/tls,
// crypto/x509, net/http, net/textproto, and html/template findings.
toolchain go1.26.5

require (
	github.com/coreos/go-oidc/v3 v3.20.0
	github.com/go-jose/go-jose/v4 v4.1.4
	github.com/robfig/cron/v3 v3.0.1
	github.com/spf13/cobra v1.10.2
	github.com/spiffe/go-spiffe/v2 v2.8.1
	go.yaml.in/yaml/v3 v3.0.4
)

require (
	github.com/Microsoft/go-winio v0.6.2 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/spf13/pflag v1.0.9 // indirect
	golang.org/x/net v0.56.0 // indirect
	golang.org/x/oauth2 v0.36.0 // indirect
	golang.org/x/sys v0.46.0 // indirect
	golang.org/x/text v0.39.0 // indirect
	google.golang.org/grpc v1.82.1 // indirect
)

require (
	cel.dev/expr v0.25.1 // indirect
	github.com/antlr4-go/antlr/v4 v4.13.1 // indirect
	github.com/google/cel-go v0.30.0
	golang.org/x/exp v0.0.0-20240823005443-9b4947da3948 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260414002931-afd174a4e478 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260414002931-afd174a4e478 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

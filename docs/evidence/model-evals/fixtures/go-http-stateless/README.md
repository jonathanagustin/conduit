# greeter

A stateless HTTP service written in Go. It listens on port 8080, serves `/`
and `/healthz`, stores nothing and calls no other service.

Build with `go build .` and run the binary. There is no configuration.

// Package devserver starts the pinned Temporal CLI development server on
// loopback with a persistent sqlite file. It is the localhost demonstration
// harness, not a production service.
package devserver

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/testsuite"
)

// Options is the loopback server shape required by the packaging proof.
type Options struct {
	CLIPath string
	DBPath  string
	Stdout  io.Writer
	Stderr  io.Writer
}

// Running is one development server process.
type Running struct {
	Server   *testsuite.DevServer
	Frontend string
	UI       string
}

// Start binds the frontend and UI to 127.0.0.1, disables the UI news feed,
// and keeps workflow state in DBPath so a restarted process can recover it.
func Start(ctx context.Context, options Options) (*Running, error) {
	if options.CLIPath == "" || options.DBPath == "" {
		return nil, fmt.Errorf("temporal CLI path and database file are required")
	}
	frontend, err := freePort()
	if err != nil {
		return nil, err
	}
	ui, err := freePort()
	if err != nil {
		return nil, err
	}
	server, err := testsuite.StartDevServer(ctx, testsuite.DevServerOptions{
		ExistingPath: options.CLIPath,
		DBFilename:   options.DBPath,
		EnableUI:     true,
		UIPort:       ui,
		LogLevel:     "warn",
		Stdout:       options.Stdout,
		Stderr:       options.Stderr,
		ClientOptions: &client.Options{
			HostPort:  net.JoinHostPort("127.0.0.1", frontend),
			Namespace: "default",
		},
		ExtraArgs: []string{"--ui-ip", "127.0.0.1", "--ui-disable-news-fetch"},
	})
	if err != nil {
		return nil, err
	}
	return &Running{Server: server, Frontend: server.FrontendHostPort(), UI: net.JoinHostPort("127.0.0.1", ui)}, nil
}

func freePort() (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer listener.Close()
	return strconv.Itoa(listener.Addr().(*net.TCPAddr).Port), nil
}

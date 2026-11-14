// Command packaging-mcp serves packaging tools over standard input and output.
package main

import (
	"context"
	"log"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters/packagingmcp"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters/packagingtools"
)

func main() {
	server := packagingmcp.NewServer(packagingtools.New())
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"repo-lens/internal/tools"
)

var version = "0.1.0"

func main() {
	workspace := flag.String("workspace", os.Getenv("REPO_LENS_WORKSPACE"), "workspace root directory (or set REPO_LENS_WORKSPACE)")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("repo-lens", version)
		os.Exit(0)
	}

	if *workspace == "" {
		log.Fatal("workspace is required: use --workspace or set REPO_LENS_WORKSPACE")
	}

	if info, err := os.Stat(*workspace); err != nil || !info.IsDir() {
		log.Fatalf("workspace %q does not exist or is not a directory", *workspace)
	}

	s := mcp.NewServer(&mcp.Implementation{
		Name:    "repo-lens",
		Version: version,
	}, nil)

	tools.Register(s, *workspace)

	if err := s.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}

//go:build wireinject

//go:generate go tool wire

package main

import (
	"github.com/google/wire"

	"github.com/dphbfs/fast-resume-scoring/internal/adapter/mcpserver"
	"github.com/dphbfs/fast-resume-scoring/internal/wiring"
)

// initServer builds the MCP server with every dependency wired from the
// environment.
func initServer() (*mcpserver.Server, error) {
	wire.Build(wiring.PlatformSet, wiring.AIClientSet, wiring.ExtractorSet, wiring.CheckerSet, wiring.HolisticSet, mcpserver.New)
	return nil, nil
}

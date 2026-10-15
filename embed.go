// Package cchpcli embeds the documents cchp ships inside its binary: the agent
// skill and the task guides.
package cchpcli

import "embed"

// Docs holds skills/ (the agent skill and the AGENTS.md snippet) and guides/.
//
//go:embed skills guides
var Docs embed.FS

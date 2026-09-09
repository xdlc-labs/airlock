package discovery

// Options controls discovery behavior that reaches outside the repository.
//
// The zero value is the safe default: discovery only reads files and, for MCP
// servers with an http(s) url, asks them for their tool list.
type Options struct {
	// ProbeStdioMCP spawns stdio MCP servers to read their live tool list.
	//
	// This executes the command named in the repository's MCP config with the
	// caller's environment, so it is off by default and every caller has to opt
	// in. Do not enable it on a workflow that runs pull requests from forks: the
	// command comes from the branch under test.
	ProbeStdioMCP bool
}

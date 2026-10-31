package agentplugin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/popoffvg/background/launch"
)

// ExecPluginPrefix starts the file name of every exec plugin: agent "lint" is `reviewrules-agent-lint`.
const ExecPluginPrefix = "reviewrules-agent-"

var _ AgentPlugin = (*ExecAgentPlugin)(nil)

// ExecAgentPlugin runs an external plugin executable once per operation.
type ExecAgentPlugin struct {
	path string
}

// NewExecAgentPlugin wraps the executable at path.
func NewExecAgentPlugin(path string) *ExecAgentPlugin {
	return &ExecAgentPlugin{path: path}
}

// FindExecAgentPlugin returns the plugin for agent in pluginDir. The file MUST be executable. The path is
// absolute because the plugin runs in the repo root.
func FindExecAgentPlugin(pluginDir string, agent string) (*ExecAgentPlugin, error) {
	if agent == "" || strings.ContainsAny(agent, `/\`) {
		return nil, fmt.Errorf("agent plugin name %q is not a plain name", agent)
	}
	path, err := filepath.Abs(filepath.Join(pluginDir, ExecPluginPrefix+agent))
	if err != nil {
		return nil, fmt.Errorf("agent plugin folder %s is invalid: %w", pluginDir, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("agent %q is not built in and has no plugin: %w", agent, err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return nil, fmt.Errorf("agent plugin %s is not an executable file", path)
	}
	return NewExecAgentPlugin(path), nil
}

// RunOperation runs `<plugin> <operation>` in the repo root with the request on stdin.
func (p *ExecAgentPlugin) RunOperation(
	ctx context.Context,
	operation PluginOperation,
	request PluginRequest,
) (PluginReply, error) {
	input, err := json.Marshal(request)
	if err != nil {
		return PluginReply{}, fmt.Errorf("agent plugin request encode failed: %w", err)
	}
	var stdout, stderr bytes.Buffer
	result, err := launch.Run(ctx, p.path, []string{string(operation)}, launch.Options{
		Dir:      request.RepoRoot,
		Stdin:    bytes.NewReader(input),
		Stdout:   &stdout,
		Stderr:   &stderr,
		SourceID: "reviewrules",
	})
	if err != nil {
		return PluginReply{}, fmt.Errorf("agent plugin %s %s failed: %w", p.path, operation, err)
	}
	if result.ExitCode != 0 {
		return PluginReply{}, fmt.Errorf(
			"agent plugin %s %s exited with %d: %s",
			p.path, operation, result.ExitCode, strings.TrimSpace(stderr.String()),
		)
	}
	var reply PluginReply
	err = json.Unmarshal(stdout.Bytes(), &reply)
	if err != nil {
		return PluginReply{}, fmt.Errorf("agent plugin %s %s reply is not JSON: %w", p.path, operation, err)
	}
	if reply.SessionID == "" {
		return reply, fmt.Errorf("agent plugin %s %s reply has no session_id", p.path, operation)
	}
	return reply, nil
}

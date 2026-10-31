package review

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"charm.land/fantasy"
)

const (
	readFileMaxLines = 400
	grepMaxLines     = 200
)

type readFileInput struct {
	Path      string `json:"path" description:"Repo-relative file path."`
	StartLine int    `json:"start_line,omitempty" description:"First line, 1-based. Default 1."`
	EndLine   int    `json:"end_line,omitempty" description:"Last line, inclusive. At most 400 lines are returned."`
}

type grepInput struct {
	Pattern  string `json:"pattern" description:"Extended regular expression, as git grep -E takes it."`
	PathGlob string `json:"path_glob,omitempty" description:"Optional pathspec glob, for example '*.go' or 'platform/**'."`
}

type listDirInput struct {
	Path string `json:"path" description:"Repo-relative directory. Use '.' for the root."`
}

// NewRepoReadTools returns the read-only tools every review agent gets.
// The tool list MUST stay identical across the parent session and its forks, or no fork reads the cached prefix.
func NewRepoReadTools(repoRoot string) []fantasy.AgentTool {
	return []fantasy.AgentTool{
		fantasy.NewParallelAgentTool(
			"read_file",
			"Read lines of a repo file as it is in the working tree. Lines are prefixed with their number.",
			func(ctx context.Context, input readFileInput, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
				return readRepoFile(repoRoot, input), nil
			},
		),
		fantasy.NewParallelAgentTool(
			"grep",
			"Search tracked repo files with git grep. Returns file:line:text, at most 200 lines.",
			func(ctx context.Context, input grepInput, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
				return grepRepo(ctx, repoRoot, input), nil
			},
		),
		fantasy.NewParallelAgentTool(
			"list_dir",
			"List the entries of a repo directory. Directories end with '/'.",
			func(ctx context.Context, input listDirInput, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
				return listRepoDir(repoRoot, input), nil
			},
		),
	}
}

// resolveRepoPath keeps every tool inside the repo: a path that escapes the root is refused.
func resolveRepoPath(repoRoot string, relative string) (string, error) {
	absolute := filepath.Join(repoRoot, filepath.Clean("/"+relative))
	if absolute != repoRoot && !strings.HasPrefix(absolute, repoRoot+string(filepath.Separator)) {
		return "", fmt.Errorf("repo path %q is outside the repo", relative)
	}
	return absolute, nil
}

func readRepoFile(repoRoot string, input readFileInput) fantasy.ToolResponse {
	absolute, err := resolveRepoPath(repoRoot, input.Path)
	if err != nil {
		return fantasy.NewTextErrorResponse(err.Error())
	}
	raw, err := os.ReadFile(absolute) //nolint:gosec // resolveRepoPath keeps absolute inside the repo
	if err != nil {
		return fantasy.NewTextErrorResponse("read_file failed: " + err.Error())
	}
	lines := strings.Split(string(raw), "\n")
	start := max(input.StartLine, 1)
	end := input.EndLine
	if end <= 0 || end > len(lines) {
		end = len(lines)
	}
	end = min(end, start+readFileMaxLines-1)
	if start > end {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("read_file: start_line %d is past the end (%d lines)", start, len(lines)))
	}
	var out strings.Builder
	for number := start; number <= end; number++ {
		fmt.Fprintf(&out, "%d\t%s\n", number, lines[number-1])
	}
	if end < len(lines) {
		fmt.Fprintf(&out, "... %d more lines; call again with start_line=%d\n", len(lines)-end, end+1)
	}
	return fantasy.NewTextResponse(out.String())
}

func grepRepo(ctx context.Context, repoRoot string, input grepInput) fantasy.ToolResponse {
	args := []string{"-C", repoRoot, "grep", "-n", "-I", "-E", "-e", input.Pattern}
	if input.PathGlob != "" {
		args = append(args, "--", ":(glob)"+input.PathGlob)
	}
	output, err := exec.CommandContext(ctx, "git", args...).Output()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return fantasy.NewTextResponse("no match")
	}
	if err != nil {
		return fantasy.NewTextErrorResponse("grep failed: " + err.Error())
	}
	lines := strings.Split(strings.TrimRight(string(output), "\n"), "\n")
	if len(lines) > grepMaxLines {
		lines = append(lines[:grepMaxLines], fmt.Sprintf("... %d more matches; narrow the pattern or path_glob", len(lines)-grepMaxLines))
	}
	return fantasy.NewTextResponse(strings.Join(lines, "\n"))
}

func listRepoDir(repoRoot string, input listDirInput) fantasy.ToolResponse {
	absolute, err := resolveRepoPath(repoRoot, input.Path)
	if err != nil {
		return fantasy.NewTextErrorResponse(err.Error())
	}
	entries, err := os.ReadDir(absolute)
	if err != nil {
		return fantasy.NewTextErrorResponse("list_dir failed: " + err.Error())
	}
	var out strings.Builder
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			name += "/"
		}
		out.WriteString(name + "\n")
	}
	return fantasy.NewTextResponse(out.String())
}

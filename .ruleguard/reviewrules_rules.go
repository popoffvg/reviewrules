//go:build ignore

// Package gorules holds this repo's own code rules; golangci-lint runs them through gocritic's ruleguard.
package gorules

import "github.com/quasilyte/go-ruleguard/dsl"

// errorAssignedInIf flags `if err := f(); err != nil`: assign the error on its own line.
func errorAssignedInIf(m dsl.Matcher) {
	m.Match(`if $err := $call; $err != nil { $*_ }`).
		Where(m["err"].Text == "err").
		Report(`error assigned inside the if; call on its own line, then check err`)
}

// subtestTitleWithSpaces flags a t.Run title with a space: titles use '-' between words.
func subtestTitleWithSpaces(m dsl.Matcher) {
	m.Match(`$t.Run($name, $*_)`).
		Where(m["name"].Text.Matches(`^"[^"]* [^"]*"$`)).
		Report(`t.Run title has a space; use '-' between words`)
}

// backgroundContextInTest flags context.Background() in a test: use t.Context().
func backgroundContextInTest(m dsl.Matcher) {
	m.Match(`context.Background()`).
		Where(m.File().Name.Matches(`_test\.go$`)).
		Report(`test uses context.Background(); use t.Context()`)
}

// osTempDirInTest flags os.MkdirTemp in a test: t.TempDir removes the folder itself.
func osTempDirInTest(m dsl.Matcher) {
	m.Match(`os.MkdirTemp($*_)`).
		Where(m.File().Name.Matches(`_test\.go$`)).
		Report(`test creates a temp dir with os.MkdirTemp; use t.TempDir()`)
}

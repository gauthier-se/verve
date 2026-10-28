// Package webui embeds the web app built by Vite. `task build` syncs it into dist, where only
// a .gitignore is committed, so that the binary also compiles without it.
package webui

import (
	"embed"
	"io/fs"
)

// dist is the dist directory, with its dotfiles.
//
//go:embed all:dist
var dist embed.FS

// FS returns the content of the dist directory.
func FS() fs.FS {
	// fs.Sub fails only on an invalid directory name.
	sub, _ := fs.Sub(dist, "dist")

	return sub
}

// Built reports whether the binary embeds the web app, rather than only the placeholder.
func Built() bool {
	return built(FS())
}

func built(fsys fs.FS) bool {
	_, err := fs.Stat(fsys, "index.html")

	return err == nil
}

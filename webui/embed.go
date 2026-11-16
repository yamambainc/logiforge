// Package webui は、ビルドした WebUI（dist/）を、logiforge のバイナリに入れる（design/webui/stack.md の「配り方」）。
// dist/ は webui/ で npm run build して作る。ビルドしていなければ、dist/ には .gitkeep だけがあり、serve は WebUI の代わりに案内を返す。
package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Dist は、ビルドした WebUI のファイルを返す。根は dist/ である。
func Dist() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}

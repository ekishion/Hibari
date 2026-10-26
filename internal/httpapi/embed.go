// Package httpapi 内嵌前端构建产物。vite 的 outDir 直接指向 assets/，构建即打包。
package httpapi

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed all:assets
var embedded embed.FS

// EmbeddedAssets 返回编译期嵌入的前端产物。
func EmbeddedAssets() (http.FileSystem, error) {
	sub, err := fs.Sub(embedded, "assets")
	if err != nil {
		return nil, err
	}
	return http.FS(sub), nil
}

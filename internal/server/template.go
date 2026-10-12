package server

import (
	"bytes"
	"fmt"
	"html/template"
	"io/fs"
	"path"
)

// renderIndexTemplate 使用 Go 标准库 html/template 将 static/index.html 与 static/partials/*.html
// 预解析并执行渲染为整包 HTML 内存切片，实现零运行时网络往返与零 FOUC。
func renderIndexTemplate(staticFS fs.FS) ([]byte, error) {
	rootPath := "index.html"
	partialsPattern := "partials/*.html"
	if _, err := fs.Stat(staticFS, rootPath); err != nil {
		rootPath = "static/index.html"
		partialsPattern = "static/partials/*.html"
	}

	// 检查根骨架模板是否存在
	if _, err := fs.Stat(staticFS, rootPath); err != nil {
		return nil, fmt.Errorf("未找到根模板文件: %s", rootPath)
	}

	partials, _ := fs.Glob(staticFS, partialsPattern)
	patterns := append([]string{rootPath}, partials...)

	tmpl, err := template.ParseFS(staticFS, patterns...)
	if err != nil {
		// 若解析失败，兜底直接返回文件原始内容
		return fs.ReadFile(staticFS, rootPath)
	}

	var buf bytes.Buffer
	templateName := path.Base(rootPath)
	if err := tmpl.ExecuteTemplate(&buf, templateName, nil); err != nil {
		return fs.ReadFile(staticFS, rootPath)
	}
	return buf.Bytes(), nil
}

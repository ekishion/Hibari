package server

import (
	"bytes"
	"fmt"
	"html/template"
	"io/fs"
	"path"
)

// renderIndexTemplate 使用 Go 标准库 html/template 将前端构建产物（web/dist）的
// index.html 预解析并执行渲染为整包 HTML 内存切片，实现零运行时网络往返与零 FOUC。
// Nuxt 产物没有 partials 目录，glob 结果为空，仅解析根模板。
func renderIndexTemplate(staticFS fs.FS) ([]byte, error) {
	rootPath := "index.html"

	// 检查根骨架模板是否存在
	if _, err := fs.Stat(staticFS, rootPath); err != nil {
		return nil, fmt.Errorf("未找到根模板文件: %s", rootPath)
	}

	partials, _ := fs.Glob(staticFS, "partials/*.html")
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

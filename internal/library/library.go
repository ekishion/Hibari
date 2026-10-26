// Package library 定义存储后端的契约与文件浏览用例。
//
// 它是所有适配器的消费方：Backend、Writer、Linker 三个接口都在本包声明，
// 适配器实现它们，而本包不 import 任何适配器。
package library

import (
	"context"
	"errors"
	"io"
	"path"
	"strings"
	"time"
)

// 领域错误。适配器返回这些值，HTTP 层映射为状态码。
var (
	ErrNotFound  = errors.New("路径不存在")
	ErrNotDir    = errors.New("不是目录")
	ErrNotFile   = errors.New("不是文件")
	ErrExists    = errors.New("同名项已存在")
	ErrInvalid   = errors.New("路径非法")
	ErrReadOnly  = errors.New("后端不支持写操作")
	ErrQuotaFull = errors.New("存储空间不足")
	ErrUpstream  = errors.New("上游服务异常")
)

// Entry 是列表与详情共用的文件条目。
type Entry struct {
	Name    string    `json:"name"`
	Path    string    `json:"path"`
	IsDir   bool      `json:"is_dir"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
	// Meta 是后端私有数据，HTTP 层不得输出。
	Meta Meta `json:"-"`
}

// Meta 承载后端特定的标识，例如百度签名需要的文件 ID 与 MD5。
type Meta struct {
	ID   string
	Hash string
}

// Quota 是存储容量信息。未知时字段为 0。
type Quota struct {
	Total int64 `json:"total"`
	Used  int64 `json:"used"`
}

// Free 返回剩余字节数；总量未知时返回 0。
func (q Quota) Free() int64 {
	if q.Total <= 0 {
		return 0
	}
	if q.Used >= q.Total {
		return 0
	}
	return q.Total - q.Used
}

// Caps 声明后端的可选能力。HTTP 层据此决定是否暴露对应接口。
type Caps struct {
	// Write 表示支持 Mkdir/Rename/Remove/Move/Copy。
	Write bool
	// Upload 表示支持 Put。
	Upload bool
	// Quota 表示容量信息可信。
	Quota bool
}

// Backend 是任何存储后端必须实现的接口。
type Backend interface {
	// List 返回目录下的直接子项。dir 为以 / 开头的绝对路径。
	List(ctx context.Context, dir string) ([]Entry, error)
	// Stat 返回单个路径的元信息。
	Stat(ctx context.Context, p string) (Entry, error)
	// Open 打开文件读取流，调用方负责关闭。
	Open(ctx context.Context, p string) (io.ReadCloser, error)
	// Quota 返回容量信息。
	Quota(ctx context.Context) (Quota, error)
	// Caps 返回能力声明。
	Caps() Caps
}

// Writer 是可选接口，由支持写操作的后端实现。
type Writer interface {
	Mkdir(ctx context.Context, dir, name string) (string, error)
	Rename(ctx context.Context, p, newName string) (string, error)
	Remove(ctx context.Context, paths []string) error
	Put(ctx context.Context, dir, name string, r io.Reader, size int64) (string, error)
}

// Linker 是可选接口。实现它的后端能让浏览器直连上游，
// 省去服务端中转带宽。
type Linker interface {
	// DirectURL 返回可直接 302 的地址。ua 参与签名，调用方须原样传给客户端。
	DirectURL(ctx context.Context, p, ua string) (string, error)
}

// CleanPath 规范化用户传入的路径：强制以 / 开头，拒绝 . 与 .. 段。
func CleanPath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "/", nil
	}
	p = strings.ReplaceAll(p, "\\", "/")
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	if strings.ContainsAny(p, "\x00\n\r\t") {
		return "", ErrInvalid
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "." || seg == ".." {
			return "", ErrInvalid
		}
	}
	clean := path.Clean(p)
	if clean == "." {
		return "/", nil
	}
	return clean, nil
}

// Join 拼接目录与文件名，用于新建与重命名。
func Join(dir, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return "", ErrInvalid
	}
	if strings.ContainsAny(name, "/\\\x00\n\r\t") {
		return "", ErrInvalid
	}
	return path.Join(dir, name), nil
}

// IsUnder 判断 p 是否等于 root 或位于 root 之下。用于沙盒圈定。
func IsUnder(p, root string) bool {
	if root == "/" {
		return strings.HasPrefix(p, "/")
	}
	return p == root || strings.HasPrefix(p, root+"/")
}

// Package local 实现基于本地磁盘目录的存储后端。
package local

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/ekishion/hibari/internal/library"
)

// randHex 生成 n 字节随机数的十六进制串，用于临时文件名去重。
func randHex(n int) string {
	buf := make([]byte, n)
	rand.Read(buf)
	return hex.EncodeToString(buf)
}

// Backend 把本地磁盘目录暴露为 library.Backend。
// 所有对外路径都以 / 开头，映射到 root 之下。
type Backend struct {
	// root 是内核保证的目录边界：任何访问都无法解析到它之外，
	// 包括符号链接与 Windows 目录联接。用它而不是手写路径比较，
	// 是因为 EvalSymlinks 在 Windows 上并不解析 junction。
	root *os.Root
	// dir 仅用于配额统计与日志，不参与路径解析。
	dir string
}

// New 创建本地后端。root 不存在时会被创建。
func New(root string) (*Backend, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("解析根目录: %w", err)
	}
	if err := os.MkdirAll(abs, 0o750); err != nil {
		return nil, fmt.Errorf("创建根目录: %w", err)
	}
	r, err := os.OpenRoot(abs)
	if err != nil {
		return nil, fmt.Errorf("打开根目录: %w", err)
	}
	return &Backend{root: r, dir: abs}, nil
}

// Close 释放根目录句柄。
func (b *Backend) Close() error {
	return b.root.Close()
}

// rel 把虚拟绝对路径转为 os.Root 需要的相对路径。
// 根目录对应 "."。
func rel(p string) (string, error) {
	clean, err := library.CleanPath(p)
	if err != nil {
		return "", err
	}
	if clean == "/" {
		return ".", nil
	}
	return strings.TrimPrefix(clean, "/"), nil
}

// stat 是 Root.Stat 的包装，把越界错误映射为领域错误。
func (b *Backend) stat(p string) (fs.FileInfo, error) {
	rp, err := rel(p)
	if err != nil {
		return nil, err
	}
	info, err := b.root.Stat(rp)
	if err != nil {
		return nil, translate(err)
	}
	return info, nil
}

func (b *Backend) Caps() library.Caps {
	return library.Caps{Write: true, Upload: true, Quota: true}
}

func (b *Backend) List(ctx context.Context, dir string) ([]library.Entry, error) {
	info, err := b.stat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, library.ErrNotDir
	}

	rp, err := rel(dir)
	if err != nil {
		return nil, err
	}
	items, err := fs.ReadDir(b.root.FS(), rp)
	if err != nil {
		return nil, translate(err)
	}

	entries := make([]library.Entry, 0, len(items))
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		info, err := item.Info()
		if err != nil {
			// 单个条目读取失败（例如并发删除）不应让整个列表失败。
			continue
		}
		// 符号链接与目录联接可能指向 root 之外，一律不展示。
		// Root 本身会拦住访问，这里额外过滤是为了不让用户看到点不动的条目。
		if info.Mode()&fs.ModeSymlink != 0 {
			continue
		}
		if target, err := b.root.Stat(path.Join(rp, item.Name())); err != nil || target.IsDir() != item.IsDir() {
			continue
		}
		entries = append(entries, library.Entry{
			Name:    item.Name(),
			Path:    path.Join(dir, item.Name()),
			IsDir:   item.IsDir(),
			Size:    info.Size(),
			ModTime: info.ModTime(),
		})
	}
	return entries, nil
}

func (b *Backend) Stat(ctx context.Context, p string) (library.Entry, error) {
	info, err := b.stat(p)
	if err != nil {
		return library.Entry{}, err
	}
	return library.Entry{
		Name:    path.Base(p),
		Path:    p,
		IsDir:   info.IsDir(),
		Size:    info.Size(),
		ModTime: info.ModTime(),
	}, nil
}

func (b *Backend) Open(ctx context.Context, p string) (io.ReadCloser, error) {
	info, err := b.stat(p)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, library.ErrNotFile
	}
	rp, err := rel(p)
	if err != nil {
		return nil, err
	}
	f, err := b.root.Open(rp)
	if err != nil {
		return nil, translate(err)
	}
	return f, nil
}

func (b *Backend) Quota(ctx context.Context) (library.Quota, error) {
	used, err := dirSize(ctx, b.dir)
	if err != nil {
		return library.Quota{}, err
	}
	// 本地后端没有配额上限，只报告已用量。
	return library.Quota{Total: 0, Used: used}, nil
}

func (b *Backend) Mkdir(ctx context.Context, dir, name string) (string, error) {
	target, err := library.Join(dir, name)
	if err != nil {
		return "", err
	}
	rp, err := rel(target)
	if err != nil {
		return "", err
	}
	if err := b.root.Mkdir(rp, 0o750); err != nil {
		return "", translate(err)
	}
	return target, nil
}

func (b *Backend) Rename(ctx context.Context, p, newName string) (string, error) {
	target, err := library.Join(path.Dir(p), newName)
	if err != nil {
		return "", err
	}
	from, err := rel(p)
	if err != nil {
		return "", err
	}
	to, err := rel(target)
	if err != nil {
		return "", err
	}
	if err := b.root.Rename(from, to); err != nil {
		return "", translate(err)
	}
	return target, nil
}

func (b *Backend) Remove(ctx context.Context, paths []string) error {
	for _, p := range paths {
		rp, err := rel(p)
		if err != nil {
			return err
		}
		if rp == "." {
			return library.ErrInvalid
		}
		// RemoveAll 在 Root 内递归删除，且不会跟随符号链接逃出边界。
		if err := b.root.RemoveAll(rp); err != nil {
			return translate(err)
		}
	}
	return nil
}

func (b *Backend) Put(ctx context.Context, dir, name string, r io.Reader, size int64) (string, error) {
	target, err := library.Join(dir, name)
	if err != nil {
		return "", err
	}
	rp, err := rel(target)
	if err != nil {
		return "", err
	}

	// 先写临时文件再原子重命名，避免上传中断留下半个文件。
	tmpName := path.Join(path.Dir(rp), fmt.Sprintf(".upload-%d-%s", time.Now().UnixNano(), randHex(4)))
	tmp, err := b.root.OpenFile(tmpName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", fmt.Errorf("创建临时文件: %w", translate(err))
	}
	defer b.root.Remove(tmpName)

	written, err := io.Copy(tmp, r)
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return "", fmt.Errorf("写入文件: %w", err)
	}
	if size >= 0 && written != size {
		return "", fmt.Errorf("写入 %d 字节，声明 %d 字节", written, size)
	}
	if err := b.root.Rename(tmpName, rp); err != nil {
		return "", translate(err)
	}
	return target, nil
}

// dirSize 统计整棵目录树的字节数。ctx 取消时立即返回。
// shortcut: 每次配额查询都全树遍历，十万级文件时改为增量统计。
func dirSize(ctx context.Context, root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // 跳过不可读项
		}
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		total += info.Size()
		return nil
	})
	return total, err
}

// translate 把 os 错误映射为领域错误。
func translate(err error) error {
	switch {
	case err == nil:
		return nil
	// os.Root 越界返回的是包内未导出的 errPathEscapes，没有可用的哨兵值，
	// 只能按错误文本识别。TestTranslateEscape 会在 Go 改动这段文案时失败报警。
	case strings.Contains(err.Error(), "path escapes from parent"):
		return library.ErrInvalid
	case errors.Is(err, fs.ErrNotExist):
		return library.ErrNotFound
	case errors.Is(err, fs.ErrExist):
		return library.ErrExists
	case errors.Is(err, fs.ErrPermission):
		return library.ErrInvalid
	default:
		return err
	}
}

var _ library.Backend = (*Backend)(nil)
var _ library.Writer = (*Backend)(nil)

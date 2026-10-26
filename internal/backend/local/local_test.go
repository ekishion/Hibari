package local_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ekishion/hibari/internal/backend/local"
	"github.com/ekishion/hibari/internal/library"
	"github.com/ekishion/hibari/internal/library/librarytest"
)

func TestContract(t *testing.T) {
	librarytest.Run(t, func(t *testing.T) library.Backend {
		b, err := local.New(t.TempDir())
		if err != nil {
			t.Fatalf("创建本地后端: %v", err)
		}
		// Windows 上未关闭的根句柄会挡住 t.TempDir 的清理。
		t.Cleanup(func() { b.Close() })
		return b
	})
}

// TestSymlinkEscape 确认 root 内指向外部的符号链接不能成为逃逸通道。
func TestSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatalf("准备外部文件: %v", err)
	}

	b, err := local.New(root)
	if err != nil {
		t.Fatalf("创建本地后端: %v", err)
	}
	t.Cleanup(func() { b.Close() })
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skipf("当前环境不支持符号链接: %v", err)
	}

	ctx := context.Background()

	if _, err := b.Stat(ctx, "/link/secret.txt"); !errors.Is(err, library.ErrInvalid) {
		t.Errorf("Stat 穿过链接应返回 ErrInvalid，实际 %v", err)
	}
	if _, err := b.Open(ctx, "/link/secret.txt"); !errors.Is(err, library.ErrInvalid) {
		t.Errorf("Open 穿过链接应返回 ErrInvalid，实际 %v", err)
	}
	if _, err := b.Put(ctx, "/link", "planted.txt", strings.NewReader("x"), 1); !errors.Is(err, library.ErrInvalid) {
		t.Errorf("Put 穿过链接应返回 ErrInvalid，实际 %v", err)
	}
	if _, err := b.List(ctx, "/link"); !errors.Is(err, library.ErrInvalid) {
		t.Errorf("List 穿过链接应返回 ErrInvalid，实际 %v", err)
	}

	// 删除链接本身是允许的，但必须只删链接，不能动到目标目录。
	if err := b.Remove(ctx, []string{"/link"}); err != nil {
		t.Fatalf("删除链接本身应成功: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "secret.txt")); err != nil {
		t.Errorf("链接目标被误删: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "link")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("链接应已被删除，实际 %v", err)
	}
}

// TestSymlinkInsideAllowed 确认 root 内部的相对链接仍可正常使用。
// os.Root 明确拒绝绝对符号链接，所以这里用相对链接。
func TestSymlinkInsideAllowed(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "real"), 0o750); err != nil {
		t.Fatalf("准备目录: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "real", "a.txt"), []byte("hi"), 0o600); err != nil {
		t.Fatalf("准备文件: %v", err)
	}

	b, err := local.New(root)
	if err != nil {
		t.Fatalf("创建本地后端: %v", err)
	}
	t.Cleanup(func() { b.Close() })
	if err := os.Symlink("real", filepath.Join(root, "alias")); err != nil {
		t.Skipf("当前环境不支持符号链接: %v", err)
	}

	entry, err := b.Stat(context.Background(), "/alias/a.txt")
	if err != nil {
		t.Fatalf("内部链接应可用: %v", err)
	}
	if entry.Size != 2 {
		t.Errorf("Size = %d，期望 2", entry.Size)
	}
}

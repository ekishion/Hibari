package local

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/ekishion/hibari/internal/library"
)

// TestTranslateEscape 守住对 os.Root 越界错误的文本匹配。
// os 包没有导出 errPathEscapes，Go 一旦改动文案，这里会立刻失败。
func TestTranslateEscape(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	outside := filepath.Join(base, "outside")
	for _, d := range []string{root, outside} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			t.Fatalf("准备目录: %v", err)
		}
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatalf("准备文件: %v", err)
	}
	// 绝对链接一定指向 root 之外（root 内部不允许绝对链接），用它触发越界。
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skipf("当前环境不支持符号链接: %v", err)
	}

	b, err := New(root)
	if err != nil {
		t.Fatalf("创建本地后端: %v", err)
	}
	t.Cleanup(func() { b.Close() })

	// 先确认错误确实来自 Root，再确认映射结果。
	_, rawErr := b.root.Stat("link/secret.txt")
	if rawErr == nil {
		t.Fatal("越界访问应当失败")
	}
	if !errors.Is(translate(rawErr), library.ErrInvalid) {
		t.Fatalf("translate(%v) 应返回 ErrInvalid", rawErr)
	}
	// 普通错误不受影响。
	if !errors.Is(translate(fs.ErrNotExist), library.ErrNotFound) {
		t.Fatal("fs.ErrNotExist 应映射为 ErrNotFound")
	}
}

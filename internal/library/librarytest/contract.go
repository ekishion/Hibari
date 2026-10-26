// Package librarytest 提供所有存储适配器共用的契约测试。
//
// 每个适配器在自己的测试包里调用 Run，保证不同后端的行为一致。
// 这是「可插拔」不流于口号的唯一手段。
package librarytest

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/ekishion/hibari/internal/library"
)

// Factory 构造一个干净的、空的后端实例。每次调用返回互不影响的新实例。
type Factory func(t *testing.T) library.Backend

// Run 执行完整契约测试。会覆盖 ReadOnly 场景下的写接口行为。
func Run(t *testing.T, newBackend Factory) {
	t.Run("ListRoot", func(t *testing.T) { testListRoot(t, newBackend) })
	t.Run("StatMissing", func(t *testing.T) { testStatMissing(t, newBackend) })
	t.Run("OpenMissing", func(t *testing.T) { testOpenMissing(t, newBackend) })
	t.Run("RejectTraversal", func(t *testing.T) { testRejectTraversal(t, newBackend) })
	t.Run("Quota", func(t *testing.T) { testQuota(t, newBackend) })
	t.Run("WriteRoundTrip", func(t *testing.T) { testWriteRoundTrip(t, newBackend) })
}

func testListRoot(t *testing.T, newBackend Factory) {
	ctx := context.Background()
	b := newBackend(t)

	entries, err := b.List(ctx, "/")
	if err != nil {
		t.Fatalf("列出根目录: %v", err)
	}
	if entries == nil {
		t.Fatal("根目录应返回非 nil 切片，避免 JSON 输出 null")
	}
}

func testStatMissing(t *testing.T, newBackend Factory) {
	ctx := context.Background()
	b := newBackend(t)

	_, err := b.Stat(ctx, "/definitely-not-here")
	if !errors.Is(err, library.ErrNotFound) {
		t.Fatalf("期望 ErrNotFound，得到 %v", err)
	}
}

func testOpenMissing(t *testing.T, newBackend Factory) {
	ctx := context.Background()
	b := newBackend(t)

	_, err := b.Open(ctx, "/definitely-not-here")
	if !errors.Is(err, library.ErrNotFound) {
		t.Fatalf("期望 ErrNotFound，得到 %v", err)
	}
}

func testRejectTraversal(t *testing.T, newBackend Factory) {
	ctx := context.Background()
	b := newBackend(t)

	for _, p := range []string{"/../etc/passwd", "/a/../../b", "..", "/./x"} {
		if _, err := b.List(ctx, p); err == nil {
			t.Errorf("路径 %q 应被拒绝", p)
		}
	}
}

func testQuota(t *testing.T, newBackend Factory) {
	ctx := context.Background()
	b := newBackend(t)

	if !b.Caps().Quota {
		t.Skip("后端不声明容量能力")
	}
	if _, err := b.Quota(ctx); err != nil {
		t.Fatalf("读取容量: %v", err)
	}
}

func testWriteRoundTrip(t *testing.T, newBackend Factory) {
	ctx := context.Background()
	b := newBackend(t)

	if !b.Caps().Write {
		t.Skip("后端为只读，跳过写测试")
	}
	w, ok := b.(library.Writer)
	if !ok {
		t.Fatal("Caps().Write 为真但未实现 library.Writer")
	}

	// 建目录
	dirPath, err := w.Mkdir(ctx, "/", "contract-dir")
	if err != nil {
		t.Fatalf("创建目录: %v", err)
	}
	if dirPath != "/contract-dir" {
		t.Fatalf("目录路径为 %q，期望 /contract-dir", dirPath)
	}

	// 重复创建应冲突
	if _, err := w.Mkdir(ctx, "/", "contract-dir"); !errors.Is(err, library.ErrExists) {
		t.Fatalf("重复创建目录期望 ErrExists，得到 %v", err)
	}

	// 上传文件
	content := []byte("hello contract")
	filePath, err := w.Put(ctx, dirPath, "note.txt", bytes.NewReader(content), int64(len(content)))
	if err != nil {
		t.Fatalf("上传文件: %v", err)
	}
	if filePath != "/contract-dir/note.txt" {
		t.Fatalf("文件路径为 %q", filePath)
	}

	// 读回内容
	rc, err := b.Open(ctx, filePath)
	if err != nil {
		t.Fatalf("打开文件: %v", err)
	}
	got, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		t.Fatalf("读取文件: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("内容为 %q，期望 %q", got, content)
	}

	// Stat 能看到它
	entry, err := b.Stat(ctx, filePath)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if entry.IsDir {
		t.Fatal("文件不应标记为目录")
	}
	if entry.Size != int64(len(content)) {
		t.Fatalf("大小为 %d，期望 %d", entry.Size, len(content))
	}

	// 重命名
	renamed, err := w.Rename(ctx, filePath, "renamed.txt")
	if err != nil {
		t.Fatalf("重命名: %v", err)
	}
	if renamed != "/contract-dir/renamed.txt" {
		t.Fatalf("重命名后路径为 %q", renamed)
	}
	if _, err := b.Stat(ctx, filePath); !errors.Is(err, library.ErrNotFound) {
		t.Fatalf("旧路径应已不存在，得到 %v", err)
	}

	// 列表包含它
	entries, err := b.List(ctx, dirPath)
	if err != nil {
		t.Fatalf("列出目录: %v", err)
	}
	var found bool
	for _, e := range entries {
		if e.Name == "renamed.txt" {
			found = true
		}
	}
	if !found {
		t.Fatalf("列表中缺少 renamed.txt，实际为 %+v", entries)
	}

	// 删除
	if err := w.Remove(ctx, []string{dirPath}); err != nil {
		t.Fatalf("删除目录: %v", err)
	}
	if _, err := b.Stat(ctx, dirPath); !errors.Is(err, library.ErrNotFound) {
		t.Fatalf("删除后应不存在，得到 %v", err)
	}
}

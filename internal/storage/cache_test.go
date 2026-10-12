package storage

import (
	"strings"
	"testing"
	"time"
)

func TestFileListCache_DirAndReadme(t *testing.T) {
	cache := NewFileListCacheWithLimits(100, 100*time.Millisecond, 50*time.Millisecond)

	// 1. Directory caching
	dirJSON := []byte(`{"errno":0,"list":[{"fs_id":12345,"path":"/test/file1.txt","server_filename":"file1.txt","size":100,"isdir":0,"md5":"dummy_md5"}]}`)
	cache.Update(dirJSON)
	cache.SetDirList("/test", dirJSON)

	got, ok := cache.GetDirList("/test")
	if !ok {
		t.Fatalf("expected GetDirList to return true")
	}
	if string(got) != string(dirJSON) {
		t.Fatalf("GetDirList content mismatch, got: %s", string(got))
	}

	// Check that FileMeta was populated
	meta, found := cache.GetFileMeta("/test/file1.txt")
	if !found || meta.FsID != 12345 {
		t.Fatalf("expected FileMeta with FsID=12345, got %+v", meta)
	}

	// 2. README caching
	cache.SetReadme("/test/README.md", "README.md", "# Hello Test")
	name, content, ok := cache.GetReadme("/test/README.md")
	if !ok || name != "README.md" || content != "# Hello Test" {
		t.Fatalf("GetReadme mismatch: name=%s content=%s ok=%v", name, content, ok)
	}

	// 3. Invalidate dir
	cache.InvalidateDir("/test")
	if _, ok := cache.GetDirList("/test"); ok {
		t.Fatalf("expected dir cache to be invalidated")
	}
	if _, _, ok := cache.GetReadme("/test/README.md"); ok {
		t.Fatalf("expected readme cache to be invalidated after InvalidateDir")
	}

	// 4. TTL expiration
	cache.SetDirList("/test2", dirJSON)
	cache.SetReadme("/test2/README.md", "README.md", "# Hello 2")
	time.Sleep(120 * time.Millisecond)

	if _, ok := cache.GetDirList("/test2"); ok {
		t.Fatalf("expected dir cache to expire after TTL")
	}
	if _, _, ok := cache.GetReadme("/test2/README.md"); ok {
		t.Fatalf("expected readme cache to expire after TTL")
	}
}

func TestFileListCache_ExportAndRestore(t *testing.T) {
	cache1 := NewFileListCacheWithLimits(100, 10*time.Minute, 5*time.Minute)
	dirJSON := []byte(`{"errno":0,"list":[{"fs_id":9999,"path":"/folder/doc.pdf","server_filename":"doc.pdf","size":200,"isdir":0,"md5":"abc"}]}`)
	cache1.SetDirList("/folder", dirJSON)

	exported := cache1.ExportDirCache()
	if len(exported) != 1 {
		t.Fatalf("expected 1 exported dir, got %d", len(exported))
	}
	if exported[0].Dir != "/folder" {
		t.Fatalf("expected dir /folder, got %s", exported[0].Dir)
	}

	cache2 := NewFileListCacheWithLimits(100, 10*time.Minute, 5*time.Minute)
	cache2.RestoreDirCache(exported)

	got, ok := cache2.GetDirList("/folder")
	if !ok || !strings.Contains(string(got), "doc.pdf") {
		t.Fatalf("expected restored dir list, got ok=%v body=%s", ok, string(got))
	}

	// 检查恢复后的元数据也能查询到
	meta, found := cache2.GetFileMeta("/folder/doc.pdf")
	if !found || meta.FsID != 9999 {
		t.Fatalf("expected restored FileMeta with FsID=9999, got %+v", meta)
	}
}

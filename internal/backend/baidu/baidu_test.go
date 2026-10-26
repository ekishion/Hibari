package baidu_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ekishion/hibari/internal/backend/baidu"
	"github.com/ekishion/hibari/internal/library"
	"github.com/ekishion/hibari/internal/library/librarytest"
)

// fakeUpstream 模拟 protocol.md 记录的接口，用于在无真实 Cookie 时验证适配器。
type fakeUpstream struct {
	// 路径 -> 该目录下的条目
	files map[string][]map[string]any
}

func newFake() *fakeUpstream {
	// 上游总有根目录，只是可能为空。
	return &fakeUpstream{files: map[string][]map[string]any{"/": {}}}
}

func (f *fakeUpstream) handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/youth/api/user/getinfo", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") == "" {
			http.Error(w, "no cookie", http.StatusUnauthorized)
			return
		}
		writeJSON(w, map[string]any{
			"errno": 0,
			"records": []map[string]any{
				{"uk": 10086, "sk": "test-session-key"},
			},
		})
	})

	mux.HandleFunc("/youth/api/list", func(w http.ResponseWriter, r *http.Request) {
		dir := r.URL.Query().Get("dir")
		page := r.URL.Query().Get("page")
		items, ok := f.files[dir]
		if !ok {
			// 上游对不存在目录返回 errno -9。
			writeJSON(w, map[string]any{"errno": -9})
			return
		}
		if page != "1" {
			items = nil
		}
		// 故意带上 dlink，验证适配器不会透传。
		out := make([]map[string]any, 0, len(items))
		for _, it := range items {
			cp := map[string]any{"dlink": "https://pan.baidu.com/secret?token=leak"}
			for k, v := range it {
				cp[k] = v
			}
			out = append(out, cp)
		}
		writeJSON(w, map[string]any{"errno": 0, "list": out})
	})

	mux.HandleFunc("/youth/api/quota", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"errno": 0, "total": 10 << 30, "used": 1 << 30})
	})

	mux.HandleFunc("/youth/api/locatedownload", func(w http.ResponseWriter, r *http.Request) {
		// 校验签名参数齐备，缺失即按上游行为返回 -7。
		q := r.URL.Query()
		if q.Get("rand") == "" || q.Get("sign") == "" || q.Get("time") == "" {
			writeJSON(w, map[string]any{"errno": -7})
			return
		}
		writeJSON(w, map[string]any{
			"errno": 0,
			"url":   "https://d.pcs.baidu.com/file/abc?fid=1",
		})
	})

	return mux
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// newBackend 启动假上游并返回指向它的适配器。
func newBackend(t *testing.T, fake *fakeUpstream) *baidu.Backend {
	t.Helper()
	srv := httptest.NewServer(fake.handler())
	t.Cleanup(srv.Close)

	b, err := baidu.New(baidu.Config{
		Cookie:  "BDUSS=test",
		BaseURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("构造适配器: %v", err)
	}
	return b
}

func TestContract(t *testing.T) {
	librarytest.Run(t, func(t *testing.T) library.Backend {
		return newBackend(t, newFake())
	})
}

func TestListStripsDlink(t *testing.T) {
	fake := newFake()
	fake.files["/"] = []map[string]any{
		{
			"server_filename": "a.txt",
			"path":            "/a.txt",
			"isdir":           0,
			"size":            11,
			"server_mtime":    1730000000,
			"fs_id":           123456,
			"md5":             "abc123",
		},
		{
			"server_filename": "docs",
			"path":            "/docs",
			"isdir":           1,
			"size":            0,
			"server_mtime":    1730000001,
			"fs_id":           789,
			"md5":             "",
		},
	}
	b := newBackend(t, fake)

	entries, err := b.List(context.Background(), "/")
	if err != nil {
		t.Fatalf("列出根目录: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("期望 2 项，实际 %d", len(entries))
	}

	file := entries[0]
	if file.Name != "a.txt" || file.IsDir || file.Size != 11 {
		t.Fatalf("文件字段不符: %+v", file)
	}
	if file.Meta.ID != "123456" || file.Meta.Hash != "abc123" {
		t.Fatalf("Meta 应保留签名所需字段: %+v", file.Meta)
	}
	if !entries[1].IsDir {
		t.Fatal("docs 应标记为目录")
	}

	// 序列化后不得出现 dlink。
	data, _ := json.Marshal(entries)
	if strings.Contains(string(data), "dlink") || strings.Contains(string(data), "leak") {
		t.Fatalf("响应泄露了 dlink: %s", data)
	}
}

func TestListMissingDirMapsToNotFound(t *testing.T) {
	b := newBackend(t, newFake())
	_, err := b.List(context.Background(), "/nope")
	if err == nil {
		t.Fatal("不存在的目录应报错")
	}
	if !strings.Contains(err.Error(), library.ErrNotFound.Error()) {
		t.Fatalf("期望 ErrNotFound，得到 %v", err)
	}
}

func TestDirectURLSignsRequest(t *testing.T) {
	fake := newFake()
	fake.files["/"] = []map[string]any{
		{
			"server_filename": "a.txt",
			"path":            "/a.txt",
			"isdir":           0,
			"size":            3,
			"server_mtime":    1730000000,
			"fs_id":           42,
			"md5":             "deadbeef",
		},
	}
	b := newBackend(t, fake)

	link, err := b.DirectURL(context.Background(), "/a.txt", "Mozilla/5.0")
	if err != nil {
		t.Fatalf("取直链: %v", err)
	}
	if !strings.HasPrefix(link, "https://d.pcs.baidu.com/") {
		t.Fatalf("直链不符: %s", link)
	}
}

func TestDirectURLRejectsDir(t *testing.T) {
	fake := newFake()
	fake.files["/"] = []map[string]any{
		{
			"server_filename": "docs",
			"path":            "/docs",
			"isdir":           1,
			"size":            0,
			"server_mtime":    1730000000,
			"fs_id":           1,
			"md5":             "",
		},
	}
	b := newBackend(t, fake)

	if _, err := b.DirectURL(context.Background(), "/docs", "ua"); err == nil {
		t.Fatal("目录不应有直链")
	}
}

func TestCapsDeclaresReadOnly(t *testing.T) {
	b := newBackend(t, newFake())
	caps := b.Caps()
	if caps.Write || caps.Upload {
		t.Fatalf("按 protocol.md §6，百度后端不应声明写能力: %+v", caps)
	}
	if !caps.Quota {
		t.Fatal("百度后端应声明容量能力")
	}
	if _, ok := any(b).(library.Writer); ok {
		t.Fatal("百度后端不应实现 library.Writer")
	}
}

func TestRequiresHTTPSBaseURL(t *testing.T) {
	if _, err := baidu.New(baidu.Config{BaseURL: "http://pan.baidu.com"}); err == nil {
		t.Fatal("非 https 的 base url 应被拒绝")
	}
}

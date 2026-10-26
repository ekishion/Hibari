package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ekishion/hibari/internal/backend/local"
	"github.com/ekishion/hibari/internal/config"
	"github.com/ekishion/hibari/internal/httpapi"
	"github.com/ekishion/hibari/internal/identity"
	"github.com/ekishion/hibari/internal/library"
	"github.com/ekishion/hibari/internal/sharing"
	"github.com/ekishion/hibari/internal/sqlite"
)

// harness 是一套跑在内存上的完整服务，用于端到端验证。
type harness struct {
	t      *testing.T
	server *httptest.Server
	client *http.Client
	root   string
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	root := t.TempDir()
	be, err := local.New(root)
	if err != nil {
		t.Fatalf("创建本地后端: %v", err)
	}
	// 后注册先执行：根句柄必须先关，TempDir 才能清理干净。
	t.Cleanup(func() { be.Close() })

	h := newHarnessWithBackend(t, be)
	h.root = root
	return h
}

// newHarnessWithBackend 用指定后端装配服务，用于验证适配器错误如何映射为 HTTP 状态。
func newHarnessWithBackend(t *testing.T, be library.Backend) *harness {
	t.Helper()

	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatalf("打开数据库: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	cfg := &config.Config{Backend: config.BackendLocal}
	if err := cfg.Normalize(); err != nil {
		t.Fatalf("规范化配置: %v", err)
	}

	srv := httpapi.New(httpapi.Deps{
		Config:   cfg,
		Backend:  be,
		Identity: identity.New(identity.NewSQLStore(db), cfg.SessionTTLDuration()),
		Sharing:  sharing.New(sharing.NewSQLStore(db)),
		Auditor:  httpapi.NewSQLAuditor(db),
		Assets:   http.FS(fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html>spa</html>")}}),
		Version:  "test",
	})

	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("创建 cookie jar: %v", err)
	}

	return &harness{
		t:      t,
		server: ts,
		client: &http.Client{Jar: jar},
	}
}

// do 发送请求并返回状态码与响应体。body 为 nil 表示无请求体。
func (h *harness) do(method, path string, body any) (int, []byte) {
	h.t.Helper()

	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			h.t.Fatalf("序列化请求体: %v", err)
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, h.server.URL+path, reader)
	if err != nil {
		h.t.Fatalf("构造请求: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return h.send(req)
}

func (h *harness) send(req *http.Request) (int, []byte) {
	h.t.Helper()
	resp, err := h.client.Do(req)
	if err != nil {
		h.t.Fatalf("%s %s: %v", req.Method, req.URL.Path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		h.t.Fatalf("读取响应: %v", err)
	}
	return resp.StatusCode, data
}

func (h *harness) mustStatus(want int, method, path string, body any) []byte {
	h.t.Helper()
	got, data := h.do(method, path, body)
	if got != want {
		h.t.Fatalf("%s %s: 期望 %d，实际 %d，响应 %s", method, path, want, got, data)
	}
	return data
}

// listNames 返回目录下所有条目的文件名集合。
func (h *harness) listNames(dir string) map[string]bool {
	h.t.Helper()
	var listing struct {
		Entries []struct {
			Name string `json:"name"`
		} `json:"entries"`
	}
	data := h.mustStatus(http.StatusOK, http.MethodGet, "/api/files?dir="+dir, nil)
	if err := json.Unmarshal(data, &listing); err != nil {
		h.t.Fatalf("解析列表: %v", err)
	}
	out := make(map[string]bool, len(listing.Entries))
	for _, e := range listing.Entries {
		out[e.Name] = true
	}
	return out
}

func (h *harness) setupAndLogin() {
	h.t.Helper()
	h.mustStatus(http.StatusCreated, http.MethodPost, "/api/setup",
		map[string]string{"username": "admin", "password": "correct-horse-battery"})
}

func TestSetupOnlyOnce(t *testing.T) {
	h := newHarness(t)

	var status map[string]any
	json.Unmarshal(h.mustStatus(http.StatusOK, http.MethodGet, "/api/status", nil), &status)
	if status["needs_setup"] != true {
		t.Fatalf("初始状态应为待初始化，实际 %v", status["needs_setup"])
	}

	h.setupAndLogin()

	json.Unmarshal(h.mustStatus(http.StatusOK, http.MethodGet, "/api/status", nil), &status)
	if status["needs_setup"] != false {
		t.Fatal("初始化后不应再提示 setup")
	}
	if status["authenticated"] != true {
		t.Fatal("初始化后应自动登录")
	}

	// 再次初始化必须被拒绝。
	h.mustStatus(http.StatusConflict, http.MethodPost, "/api/setup",
		map[string]string{"username": "hacker", "password": "another-password"})
}

func TestRejectsWeakSetupPassword(t *testing.T) {
	h := newHarness(t)
	h.mustStatus(http.StatusBadRequest, http.MethodPost, "/api/setup",
		map[string]string{"username": "admin", "password": "short"})
}

func TestLoginLogoutFlow(t *testing.T) {
	h := newHarness(t)
	h.setupAndLogin()

	h.mustStatus(http.StatusOK, http.MethodPost, "/api/logout", nil)

	var status map[string]any
	json.Unmarshal(h.mustStatus(http.StatusOK, http.MethodGet, "/api/status", nil), &status)
	if status["authenticated"] != false {
		t.Fatal("登出后不应仍处于登录状态")
	}

	// 未登录访问受保护接口。
	h.mustStatus(http.StatusUnauthorized, http.MethodGet, "/api/files", nil)

	h.mustStatus(http.StatusOK, http.MethodPost, "/api/login",
		map[string]string{"username": "admin", "password": "correct-horse-battery"})
	h.mustStatus(http.StatusOK, http.MethodGet, "/api/files", nil)

	// 错误密码不能登录。
	h.mustStatus(http.StatusOK, http.MethodPost, "/api/logout", nil)
	h.mustStatus(http.StatusUnauthorized, http.MethodPost, "/api/login",
		map[string]string{"username": "admin", "password": "wrong-password"})
}

func TestFileLifecycle(t *testing.T) {
	h := newHarness(t)
	h.setupAndLogin()

	// 建目录。
	h.mustStatus(http.StatusCreated, http.MethodPost, "/api/files/mkdir",
		map[string]string{"dir": "/", "name": "docs"})

	// 重名必须冲突。
	h.mustStatus(http.StatusConflict, http.MethodPost, "/api/files/mkdir",
		map[string]string{"dir": "/", "name": "docs"})

	// 上传文件。
	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	mw.WriteField("dir", "/docs")
	mw.WriteField("name", "hello.txt")
	fw, err := mw.CreateFormFile("file", "hello.txt")
	if err != nil {
		t.Fatalf("构造表单: %v", err)
	}
	fw.Write([]byte("hello world"))
	mw.Close()

	req, _ := http.NewRequest(http.MethodPost, h.server.URL+"/api/upload", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := h.client.Do(req)
	if err != nil {
		t.Fatalf("上传: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("上传应返回 201，实际 %d", resp.StatusCode)
	}

	// 列表应包含上传的文件。
	var listing struct {
		Entries []struct {
			Name  string `json:"name"`
			Path  string `json:"path"`
			IsDir bool   `json:"is_dir"`
			Size  int64  `json:"size"`
		} `json:"entries"`
	}
	json.Unmarshal(h.mustStatus(http.StatusOK, http.MethodGet, "/api/files?dir=/docs", nil), &listing)
	if len(listing.Entries) != 1 || listing.Entries[0].Name != "hello.txt" {
		t.Fatalf("目录内容不符: %+v", listing.Entries)
	}
	if listing.Entries[0].Size != int64(len("hello world")) {
		t.Fatalf("文件大小不符: %d", listing.Entries[0].Size)
	}

	// 下载内容一致。
	status, data := h.do(http.MethodGet, "/api/files/raw?path=/docs/hello.txt", nil)
	if status != http.StatusOK {
		t.Fatalf("下载失败: %d %s", status, data)
	}
	if string(data) != "hello world" {
		t.Fatalf("下载内容不符: %q", data)
	}

	// 重命名。
	h.mustStatus(http.StatusOK, http.MethodPost, "/api/files/rename",
		map[string]string{"path": "/docs/hello.txt", "new_name": "renamed.txt"})
	names := h.listNames("/docs")
	if !names["renamed.txt"] || names["hello.txt"] {
		t.Fatalf("重命名后目录内容不符: %v", names)
	}

	// 删除。
	h.mustStatus(http.StatusOK, http.MethodPost, "/api/files/remove",
		map[string]any{"paths": []string{"/docs"}})
	if status, _ := h.do(http.MethodGet, "/api/files?dir=/docs", nil); status == http.StatusOK {
		t.Fatal("目录已删除，列表不应成功")
	}
}

// linkerBackend 的直链永远失败，用于验证错误映射不会退化成 500。
type linkerBackend struct {
	library.Backend
	err error
}

func (b linkerBackend) DirectURL(context.Context, string, string) (string, error) {
	return "", b.err
}

// 直链失败必须直接返回上游错误，不能再回落 backend.Open 重试一次。
func TestRawDirectLinkFailureIsUpstreamError(t *testing.T) {
	be, err := local.New(t.TempDir())
	if err != nil {
		t.Fatalf("创建本地后端: %v", err)
	}
	t.Cleanup(func() { be.Close() })

	h := newHarnessWithBackend(t, linkerBackend{
		Backend: be,
		err:     fmt.Errorf("%w: 身份校验失败，请更新 Cookie", library.ErrUpstream),
	})
	h.setupAndLogin()

	data := h.mustStatus(http.StatusBadGateway, http.MethodGet, "/api/files/raw?path=/a.txt", nil)

	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatalf("解析错误体: %v", err)
	}
	if body.Error.Code != "upstream_failed" {
		t.Fatalf("期望 upstream_failed，实际 %q", body.Error.Code)
	}
	if !strings.Contains(body.Error.Message, "Cookie") {
		t.Fatalf("错误消息应保留可操作的原因，实际 %q", body.Error.Message)
	}
}

func TestRawSupportsRange(t *testing.T) {
	h := newHarness(t)
	h.setupAndLogin()

	h.mustStatus(http.StatusCreated, http.MethodPost, "/api/files/mkdir",
		map[string]string{"dir": "/", "name": "docs"})

	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	mw.WriteField("dir", "/docs")
	mw.WriteField("name", "range.txt")
	fw, _ := mw.CreateFormFile("file", "range.txt")
	fw.Write([]byte("0123456789"))
	mw.Close()

	req, _ := http.NewRequest(http.MethodPost, h.server.URL+"/api/upload", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := h.client.Do(req)
	if err != nil {
		t.Fatalf("上传: %v", err)
	}
	resp.Body.Close()

	// 浏览器拖动视频进度依赖 Range 请求。
	req, _ = http.NewRequest(http.MethodGet, h.server.URL+"/api/files/raw?path=/docs/range.txt", nil)
	req.Header.Set("Range", "bytes=2-5")
	resp, err = h.client.Do(req)
	if err != nil {
		t.Fatalf("分段请求: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("期望 206，实际 %d", resp.StatusCode)
	}
	data, _ := io.ReadAll(resp.Body)
	if string(data) != "2345" {
		t.Fatalf("分段内容不符: %q", data)
	}
}

func TestRejectsTrailingJSON(t *testing.T) {
	h := newHarness(t)
	h.setupAndLogin()

	req, _ := http.NewRequest(http.MethodPost, h.server.URL+"/api/files/mkdir",
		strings.NewReader(`{"dir":"/","name":"a"}{"dir":"/","name":"b"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := h.client.Do(req)
	if err != nil {
		t.Fatalf("请求: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("尾随 JSON 期望 400，实际 %d", resp.StatusCode)
	}
}

func TestRejectsPathTraversal(t *testing.T) {
	h := newHarness(t)
	h.setupAndLogin()

	for _, p := range []string{"/../etc/passwd", "/a/../../b", "..", "/./x"} {
		status, data := h.do(http.MethodGet, "/api/files?dir="+url.QueryEscape(p), nil)
		if status != http.StatusBadRequest {
			t.Fatalf("路径 %q 应被拒绝，实际 %d %s", p, status, data)
		}
	}
}

func TestPathAllowlist(t *testing.T) {
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatalf("打开数据库: %v", err)
	}
	defer db.Close()

	root := t.TempDir()
	be, err := local.New(root)
	if err != nil {
		t.Fatalf("创建本地后端: %v", err)
	}
	t.Cleanup(func() { be.Close() })
	// 目录结构：/public 允许，/secret 禁止。
	be.Mkdir(t.Context(), "/", "public")
	be.Mkdir(t.Context(), "/", "secret")

	cfg := &config.Config{
		Backend:      config.BackendLocal,
		AllowedPaths: []string{"/public"},
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatalf("规范化配置: %v", err)
	}

	srv := httpapi.New(httpapi.Deps{
		Config:   cfg,
		Backend:  be,
		Identity: identity.New(identity.NewSQLStore(db), cfg.SessionTTLDuration()),
		Sharing:  sharing.New(sharing.NewSQLStore(db)),
		Auditor:  httpapi.NewSQLAuditor(db),
	})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	jar, _ := cookiejar.New(nil)
	h := &harness{t: t, server: ts, client: &http.Client{Jar: jar}}

	h.setupAndLogin()

	// 根目录列表只应出现白名单内的条目。
	var listing struct {
		Entries []struct {
			Name string `json:"name"`
		} `json:"entries"`
	}
	json.Unmarshal(h.mustStatus(http.StatusOK, http.MethodGet, "/api/files?dir=/", nil), &listing)
	for _, e := range listing.Entries {
		if e.Name == "secret" {
			t.Fatal("白名单外的目录不应出现在列表中")
		}
	}

	h.mustStatus(http.StatusForbidden, http.MethodGet, "/api/files?dir=/secret", nil)
	h.mustStatus(http.StatusOK, http.MethodGet, "/api/files?dir=/public", nil)
}

func TestShareFlow(t *testing.T) {
	h := newHarness(t)
	h.setupAndLogin()

	// 准备一个可分享的文件。
	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	mw.WriteField("dir", "/")
	mw.WriteField("name", "secret.txt")
	fw, _ := mw.CreateFormFile("file", "secret.txt")
	fw.Write([]byte("top secret"))
	mw.Close()

	req, _ := http.NewRequest(http.MethodPost, h.server.URL+"/api/upload", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := h.client.Do(req)
	if err != nil {
		t.Fatalf("上传: %v", err)
	}
	resp.Body.Close()

	// 创建带提取码的分享。
	var created struct {
		Token string `json:"token"`
	}
	json.Unmarshal(h.mustStatus(http.StatusCreated, http.MethodPost, "/api/shares",
		map[string]any{"path": "/secret.txt", "password": "1234"}), &created)
	if created.Token == "" {
		t.Fatal("分享令牌为空")
	}

	// 匿名客户端验证提取码流程。
	anon, _ := cookiejar.New(nil)
	anonClient := &http.Client{
		Jar: anon,
		// 分享下载不应自动跟随跳转，便于断言状态码。
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	postJSON := func(path string, body any) (int, []byte) {
		data, _ := json.Marshal(body)
		req, _ := http.NewRequest(http.MethodPost, ts0(h)+path, bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		resp, err := anonClient.Do(req)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		defer resp.Body.Close()
		out, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, out
	}

	// 无提取码：只告知需要提取码，不泄露文件名。
	var info map[string]any
	code, data := postJSON("/api/share/info", map[string]string{"token": created.Token})
	if code != http.StatusOK {
		t.Fatalf("查询分享信息: %d %s", code, data)
	}
	json.Unmarshal(data, &info)
	if info["unlocked"] != false || info["name"] != nil {
		t.Fatalf("未解锁时不应泄露信息: %v", info)
	}
	// 错误提取码。
	code, _ = postJSON("/api/share/info", map[string]string{
		"token": created.Token, "password": "9999"})
	if code != http.StatusOK {
		t.Fatalf("错误提取码应返回 200 且标记未解锁，实际 %d", code)
	}

	// 正确提取码。
	json.Unmarshal(postJSONOK(t, postJSON, "/api/share/info", map[string]string{
		"token": created.Token, "password": "1234"}), &info)
	if info["unlocked"] != true || info["name"] != "secret.txt" {
		t.Fatalf("正确提取码应返回文件信息: %v", info)
	}

	// 下载。
	get := func(path string) (int, []byte, string) {
		req, _ := http.NewRequest(http.MethodGet, ts0(h)+path, nil)
		resp, err := anonClient.Do(req)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		defer resp.Body.Close()
		out, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, out, resp.Header.Get("Location")
	}

	// 不带 download 参数应跳转到落地页。
	code, _, loc := get("/api/share/" + created.Token)
	if code != http.StatusFound || loc != "/s/"+created.Token {
		t.Fatalf("应跳转到落地页，实际 %d %q", code, loc)
	}

	// 错误提取码下载必须失败。
	code, _, _ = get("/api/share/" + created.Token + "?download=1&pwd=0000")
	if code != http.StatusUnauthorized {
		t.Fatalf("错误提取码下载应 401，实际 %d", code)
	}

	// 正确提取码下载。
	code, data, _ = get("/api/share/" + created.Token + "?download=1&pwd=1234")
	if code != http.StatusOK || string(data) != "top secret" {
		t.Fatalf("分享下载失败: %d %q", code, data)
	}

	// 撤销后失效。
	h.mustStatus(http.StatusOK, http.MethodDelete, "/api/shares/"+created.Token, nil)
	code, _, _ = get("/api/share/" + created.Token + "?download=1&pwd=1234")
	if code != http.StatusNotFound {
		t.Fatalf("撤销后应 404，实际 %d", code)
	}
}

func ts0(h *harness) string { return h.server.URL }

func postJSONOK(t *testing.T, post func(string, any) (int, []byte), path string, body any) []byte {
	t.Helper()
	code, data := post(path, body)
	if code != http.StatusOK {
		t.Fatalf("%s: 期望 200，实际 %d %s", path, code, data)
	}
	return data
}

func TestShareUseCountOnlyOnDownload(t *testing.T) {
	h := newHarness(t)
	h.setupAndLogin()

	h.mustStatus(http.StatusCreated, http.MethodPost, "/api/files/mkdir",
		map[string]string{"dir": "/", "name": "album"})
	upload := func(dir, name, content string) {
		body := &bytes.Buffer{}
		mw := multipart.NewWriter(body)
		mw.WriteField("dir", dir)
		mw.WriteField("name", name)
		fw, _ := mw.CreateFormFile("file", name)
		fw.Write([]byte(content))
		mw.Close()
		req, _ := http.NewRequest(http.MethodPost, h.server.URL+"/api/upload", body)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		resp, err := h.client.Do(req)
		if err != nil {
			t.Fatalf("上传 %s: %v", name, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("上传 %s 应 201，实际 %d", name, resp.StatusCode)
		}
	}
	upload("/album", "a.txt", "aaa")
	upload("/album", "b.txt", "bbb")

	var created struct {
		Token string `json:"token"`
	}
	json.Unmarshal(h.mustStatus(http.StatusCreated, http.MethodPost, "/api/shares",
		map[string]any{"path": "/album", "max_uses": 1}), &created)

	anonClient := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	get := func(path string) int {
		req, _ := http.NewRequest(http.MethodGet, h.server.URL+path, nil)
		resp, err := anonClient.Do(req)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		defer resp.Body.Close()
		io.Copy(io.Discard, resp.Body)
		return resp.StatusCode
	}
	browse := func() int {
		req, _ := http.NewRequest(http.MethodPost, h.server.URL+"/api/share/browse",
			strings.NewReader(`{"token":"`+created.Token+`","dir":"/"}`))
		req.Header.Set("Content-Type", "application/json")
		resp, err := anonClient.Do(req)
		if err != nil {
			t.Fatalf("浏览: %v", err)
		}
		defer resp.Body.Close()
		io.Copy(io.Discard, resp.Body)
		return resp.StatusCode
	}

	// 浏览与内联预览都不计次，反复调用不应耗尽配额。
	for i := 0; i < 3; i++ {
		if code := browse(); code != http.StatusOK {
			t.Fatalf("第 %d 次浏览应 200，实际 %d", i+1, code)
		}
		if code := get("/api/share/" + created.Token + "/raw?path=/a.txt"); code != http.StatusOK {
			t.Fatalf("第 %d 次内联预览应 200，实际 %d", i+1, code)
		}
	}

	// 目录分享走单文件下载入口应被拒，且不能烧掉次数。
	if code := get("/api/share/" + created.Token + "?download=1"); code != http.StatusBadRequest {
		t.Fatalf("目录分享下载应 400，实际 %d", code)
	}
	if code := browse(); code != http.StatusOK {
		t.Fatalf("被拒的请求不应消耗次数，浏览返回 %d", code)
	}

	// 真实下载消耗唯一一次配额。
	if code := get("/api/share/" + created.Token + "/raw?path=/b.txt&download=1"); code != http.StatusOK {
		t.Fatalf("首次下载应 200，实际 %d", code)
	}
	if code := get("/api/share/" + created.Token + "/raw?path=/b.txt&download=1"); code != http.StatusGone {
		t.Fatalf("配额耗尽后下载应 410，实际 %d", code)
	}
	// 配额耗尽即整个分享失效，浏览与预览一并关闭。
	if code := browse(); code != http.StatusGone {
		t.Fatalf("配额耗尽后浏览应 410，实际 %d", code)
	}
}

func TestOriginCheckBlocksCrossSitePost(t *testing.T) {
	h := newHarness(t)
	h.setupAndLogin()

	req, _ := http.NewRequest(http.MethodPost, h.server.URL+"/api/files/mkdir",
		strings.NewReader(`{"dir":"/","name":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil.example")
	status, data := h.send(req)
	if status != http.StatusForbidden {
		t.Fatalf("跨站 POST 应被拒绝，实际 %d %s", status, data)
	}

	// 同源 Origin 放行。
	req, _ = http.NewRequest(http.MethodPost, h.server.URL+"/api/files/mkdir",
		strings.NewReader(`{"dir":"/","name":"ok"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", h.server.URL)
	status, data = h.send(req)
	if status != http.StatusCreated {
		t.Fatalf("同源 POST 应放行，实际 %d %s", status, data)
	}
}

func TestUnknownJSONFieldRejected(t *testing.T) {
	h := newHarness(t)
	h.setupAndLogin()

	req, _ := http.NewRequest(http.MethodPost, h.server.URL+"/api/files/mkdir",
		strings.NewReader(`{"dir":"/","name":"x","evil":"y"}`))
	req.Header.Set("Content-Type", "application/json")
	status, data := h.send(req)
	if status != http.StatusBadRequest {
		t.Fatalf("未知字段应被拒绝，实际 %d %s", status, data)
	}
}

func TestAuditRecordsLogin(t *testing.T) {
	h := newHarness(t)
	h.setupAndLogin()

	// 显式再登录一次，产生 login 事件（初始化内置的登录只记 setup）。
	h.mustStatus(http.StatusOK, http.MethodPost, "/api/logout", nil)
	h.mustStatus(http.StatusOK, http.MethodPost, "/api/login",
		map[string]string{"username": "admin", "password": "correct-horse-battery"})

	var resp struct {
		Events []struct {
			Event string `json:"event"`
		} `json:"events"`
	}
	json.Unmarshal(h.mustStatus(http.StatusOK, http.MethodGet, "/api/audit?limit=20", nil), &resp)

	var sawSetup, sawLogin bool
	for _, e := range resp.Events {
		switch e.Event {
		case "setup":
			sawSetup = true
		case "login":
			sawLogin = true
		}
	}
	if !sawSetup || !sawLogin {
		t.Fatalf("审计应包含 setup 与 login，实际 %+v", resp.Events)
	}
}

func TestPasswordChangeRevokesSessions(t *testing.T) {
	h := newHarness(t)
	h.setupAndLogin()

	h.mustStatus(http.StatusOK, http.MethodPost, "/api/password", map[string]string{
		"current_password": "correct-horse-battery",
		"new_password":     "brand-new-password",
	})

	// 改密后当前会话失效。
	h.mustStatus(http.StatusUnauthorized, http.MethodGet, "/api/files", nil)

	// 新密码可登录，旧密码不行。
	h.mustStatus(http.StatusUnauthorized, http.MethodPost, "/api/login",
		map[string]string{"username": "admin", "password": "correct-horse-battery"})
	h.mustStatus(http.StatusOK, http.MethodPost, "/api/login",
		map[string]string{"username": "admin", "password": "brand-new-password"})
}

// 未注册的 /api 路径必须回 JSON 404。若回退成 index.html，
// 前端会拿 HTML 去 JSON.parse，报出与真实原因无关的解析错误。
func TestUnknownAPIPathReturnsJSON(t *testing.T) {
	h := newHarness(t)

	req, _ := http.NewRequest(http.MethodGet, h.server.URL+"/api/does-not-exist", nil)
	status, data := h.send(req)
	if status != http.StatusNotFound {
		t.Fatalf("期望 404，实际 %d", status)
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatalf("响应不是 JSON: %s", data)
	}
	if body.Error.Code != "not_found" {
		t.Fatalf("期望 not_found，实际 %q", body.Error.Code)
	}

	// 非 /api 路径仍应回退到 SPA。
	if status, _ := h.do(http.MethodGet, "/overview", nil); status != http.StatusOK {
		t.Fatalf("SPA 路由应返回 200，实际 %d", status)
	}
}

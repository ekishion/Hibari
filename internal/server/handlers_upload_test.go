package server

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"testing/fstest"

	"github.com/ekishion/jikeqingpan/internal/config"
)

func newUploadServer(t *testing.T, mutate func(*config.Config)) *Server {
	t.Helper()
	cfg := &config.Config{StateBackend: "none", UploadEnabled: true}
	if mutate != nil {
		mutate(cfg)
	}
	memFS := fstest.MapFS{"static/index.html": &fstest.MapFile{Data: []byte("<html></html>")}}
	s := NewServer(cfg, memFS, "test")
	t.Cleanup(s.Close)
	return s
}

func newUploadMultipart(t *testing.T, dir, size, fileName, content string) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if dir != "" {
		_ = writer.WriteField("dir", dir)
	}
	if size != "" {
		_ = writer.WriteField("size", size)
	}
	if fileName != "" {
		part, err := writer.CreateFormFile("file", fileName)
		if err != nil {
			t.Fatalf("create form file: %v", err)
		}
		if _, err := part.Write([]byte(content)); err != nil {
			t.Fatalf("write form file: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}
	return body, writer.FormDataContentType()
}

func TestHandleUpload_Disabled(t *testing.T) {
	// 上传关闭时路由不注册：请求直接 404（而非 handler 内 403），代码路径不可达
	s := newUploadServer(t, func(c *config.Config) { c.UploadEnabled = false })
	body, contentType := newUploadMultipart(t, "/", "5", "a.txt", "hello")
	req := httptest.NewRequest(http.MethodPost, "/api/upload", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()

	s.Handler().ServeHTTP(rec, req)

	// POST 落到静态兜底路由（仅 GET/HEAD）返回 405，证明上传路由未注册、代码路径不可达
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 for POST with static fallback, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleUpload_AuthAndMethodGuards(t *testing.T) {
	s := newUploadServer(t, func(c *config.Config) {
		c.UploadEnabled = true
		c.AccessToken = "top-secret-token"
	})
	handler := s.Handler()

	// GET 被方法白名单拒绝
	getReq := httptest.NewRequest(http.MethodGet, "/api/upload", nil)
	getRec := httptest.NewRecorder()
	handler.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 for GET, got %d", getRec.Code)
	}

	// 未登录 POST：CSRF 校验先于鉴权
	body, contentType := newUploadMultipart(t, "/", "5", "a.txt", "hello")
	postReq := httptest.NewRequest(http.MethodPost, "/api/upload", body)
	postReq.Header.Set("Content-Type", contentType)
	postRec := httptest.NewRecorder()
	handler.ServeHTTP(postRec, postReq)
	if postRec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for missing csrf, got %d", postRec.Code)
	}
}

func TestHandleUpload_QuotaRoute(t *testing.T) {
	s := newUploadServer(t, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/quota", nil)
	rec := httptest.NewRecorder()

	s.handleQuota(rec, req)

	// 本地测试无真实百度 Cookie：返回 502 quota_failed 即为路由与处理器接线正确
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 without baidu cookie backend, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Error.Code != "quota_failed" {
		t.Fatalf("unexpected error code: %q", resp.Error.Code)
	}
}

func TestHandleUpload_MissingFileField(t *testing.T) {
	s := newUploadServer(t, nil)
	body, contentType := newUploadMultipart(t, "/", "", "", "")
	req := httptest.NewRequest(http.MethodPost, "/api/upload", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()

	s.handleUpload(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleUpload_FileBeforeFields(t *testing.T) {
	s := newUploadServer(t, nil)
	// file 字段在 dir/size 之前：multipart 按序消费，直接拒绝
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", "a.txt")
	_, _ = part.Write([]byte("hello"))
	_ = writer.WriteField("dir", "/")
	_ = writer.WriteField("size", "5")
	_ = writer.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()

	s.handleUpload(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleUpload_OversizeRejected(t *testing.T) {
	s := newUploadServer(t, func(c *config.Config) { c.UploadMaxMB = 1 })
	body, contentType := newUploadMultipart(t, "/", "2097152", "big.bin", "hello")
	req := httptest.NewRequest(http.MethodPost, "/api/upload", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()

	s.handleUpload(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Error.Code != "upload_too_large" {
		t.Fatalf("unexpected error code: %q", resp.Error.Code)
	}
}

func TestHandleUpload_InvalidPath(t *testing.T) {
	s := newUploadServer(t, nil)
	body, contentType := newUploadMultipart(t, "/../etc", "5", "a.txt", "hello")
	req := httptest.NewRequest(http.MethodPost, "/api/upload", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()

	s.handleUpload(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for traversal path, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleUpload_OutsideAllowedPaths(t *testing.T) {
	s := newUploadServer(t, func(c *config.Config) {
		c.AllowedPaths = []string{"/shared"}
	})
	body, contentType := newUploadMultipart(t, "/forbidden", "5", "a.txt", "hello")
	req := httptest.NewRequest(http.MethodPost, "/api/upload", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()

	s.handleUpload(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Error.Code != "path_not_allowed" {
		t.Fatalf("unexpected error code: %q", resp.Error.Code)
	}
}

func TestHandleUpload_EndToEnd(t *testing.T) {
	var (
		precreateQueries []url.Values
		chunks           []byte
		chunkQueries     []url.Values
		createQuery      url.Values
	)
	baiduMux := http.NewServeMux()
	baiduMux.HandleFunc("/youth/api/quota", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"errno":0,"total":1000000,"used":100,"free":999900}`))
	})
	baiduMux.HandleFunc("/youth/api/list", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"errno":0,"list":[]}`))
	})
	baiduMux.HandleFunc("/youth/api/precreate", func(w http.ResponseWriter, r *http.Request) {
		precreateQueries = append(precreateQueries, r.URL.Query())
		_, _ = w.Write([]byte(`{"errno":0,"uploadid":"up-e2e","return_type":1}`))
	})
	baiduMux.HandleFunc("/youth/api/create", func(w http.ResponseWriter, r *http.Request) {
		createQuery = r.URL.Query()
		_, _ = w.Write([]byte(`{"errno":0}`))
	})
	baiduSrv := httptest.NewServer(baiduMux)
	defer baiduSrv.Close()

	pcsMux := http.NewServeMux()
	pcsMux.HandleFunc("/rest/2.0/pcs/superfile2", func(w http.ResponseWriter, r *http.Request) {
		chunkQueries = append(chunkQueries, r.URL.Query())
		file, _, err := r.FormFile("file")
		if err != nil {
			t.Errorf("pcs form file: %v", err)
			return
		}
		defer file.Close()
		data, err := io.ReadAll(file)
		if err != nil {
			t.Errorf("pcs read chunk: %v", err)
		}
		chunks = append(chunks, data...)
		_, _ = w.Write([]byte(`{"errno":0}`))
	})
	pcsSrv := httptest.NewServer(pcsMux)
	defer pcsSrv.Close()

	s := newUploadServer(t, func(c *config.Config) {
		c.UploadChunkMB = 1
		c.BaiduBaseURL = baiduSrv.URL
		c.BaiduPCSBaseURL = pcsSrv.URL
	})

	content := "hello end-to-end upload"
	body, contentType := newUploadMultipart(t, "/", strconv.Itoa(len(content)), "hello.txt", content)
	req := httptest.NewRequest(http.MethodPost, "/api/upload", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()

	s.handleUpload(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		OK   bool   `json:"ok"`
		Path string `json:"path"`
		Size int64  `json:"size"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !resp.OK || resp.Path != "/hello.txt" || resp.Size != int64(len(content)) {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if string(chunks) != content {
		t.Fatalf("chunk content mismatch: %q", chunks)
	}
	if len(precreateQueries) != 1 || precreateQueries[0].Get("path") != "/hello.txt" {
		t.Fatalf("unexpected precreate queries: %v", precreateQueries)
	}
	if createQuery.Get("uploadid") != "up-e2e" {
		t.Fatalf("unexpected create uploadid: %q", createQuery.Get("uploadid"))
	}
	// 上传成功后目录缓存应失效（前端 refresh:true 会重新拉列表）
	if _, ok := s.cache.GetDirList("/"); ok {
		t.Fatal("expected dir cache invalidated after upload")
	}
}

func TestHandleUpload_FileExistsPrecheck(t *testing.T) {
	baiduMux := http.NewServeMux()
	baiduMux.HandleFunc("/youth/api/list", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"errno":0,"list":[{"fs_id":1,"path":"/dup.txt","server_filename":"dup.txt","isdir":0}]}`))
	})
	baiduSrv := httptest.NewServer(baiduMux)
	defer baiduSrv.Close()

	s := newUploadServer(t, func(c *config.Config) { c.BaiduBaseURL = baiduSrv.URL })
	body, contentType := newUploadMultipart(t, "/", "5", "dup.txt", "hello")
	req := httptest.NewRequest(http.MethodPost, "/api/upload", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()

	s.handleUpload(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 for existing file, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Error.Code != "file_exists" {
		t.Fatalf("unexpected error code: %q", resp.Error.Code)
	}
}

func TestWriteTimeoutFor_UploadExtension(t *testing.T) {
	cfg := &config.Config{}
	base := WriteTimeoutFor(cfg)
	cfgUpload := &config.Config{UploadEnabled: true, UploadMaxMB: 2048}
	extended := WriteTimeoutFor(cfgUpload)
	if extended <= base {
		t.Fatalf("expected upload-enabled timeout > base: %v vs %v", extended, base)
	}
}

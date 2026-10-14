package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ekishion/jikeqingpan/internal/config"
)

func TestHandleFiles_CacheHit(t *testing.T) {
	cfg := &config.Config{
		StateBackend: "none",
	}
	memFS := fstest.MapFS{
		"static/index.html": &fstest.MapFile{Data: []byte("<html></html>")},
	}
	s := NewServer(cfg, memFS, "test")
	defer s.Close()

	// Pre-populate dir cache
	sampleDir := []byte(`{"errno":0,"list":[{"fs_id":111,"path":"/cached/file.txt","server_filename":"file.txt","isdir":0,"dlink":"https://fake/dlink"}]}`)
	s.cache.Update(sampleDir)
	s.cache.SetDirList("/cached", sampleDir)

	// Make request without refresh
	reqBody := []byte(`{"dir":"/cached"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/files", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	s.handleFiles(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-Cache") != "HIT" {
		t.Fatalf("expected X-Cache: HIT, got %s", rec.Header().Get("X-Cache"))
	}

	var resp struct {
		List []struct {
			FsID int64  `json:"fs_id"`
			Path string `json:"path"`
		} `json:"list"`
		ResolvedDir string `json:"resolved_dir"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if len(resp.List) != 1 || resp.List[0].FsID != 111 {
		t.Fatalf("unexpected list: %+v", resp.List)
	}
	if resp.ResolvedDir != "/cached" {
		t.Fatalf("unexpected resolved_dir: %s", resp.ResolvedDir)
	}
}

func TestHandleReadme_CacheHit(t *testing.T) {
	cfg := &config.Config{
		StateBackend: "none",
	}
	memFS := fstest.MapFS{
		"static/index.html": &fstest.MapFile{Data: []byte("<html></html>")},
	}
	s := NewServer(cfg, memFS, "test")
	defer s.Close()

	// Pre-populate readme cache
	s.cache.SetReadme("/docs/README.md", "README.md", "# Preloaded Content")

	reqBody := []byte(`{"path":"/docs/README.md"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/readme", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	s.handleReadme(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-Cache") != "HIT" {
		t.Fatalf("expected X-Cache: HIT, got %s", rec.Header().Get("X-Cache"))
	}

	var resp struct {
		Found   bool   `json:"found"`
		Name    string `json:"name"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse readme response: %v", err)
	}
	if !resp.Found || resp.Content != "# Preloaded Content" {
		t.Fatalf("unexpected readme resp: %+v", resp)
	}
}

func TestHandleFiles_SandboxConfinement(t *testing.T) {
	cfg := &config.Config{StateBackend: "none"}
	memFS := fstest.MapFS{"static/index.html": &fstest.MapFile{Data: []byte("<html></html>")}}
	s := NewServer(cfg, memFS, "test")
	defer s.Close()

	// 缓存子目录与外部目录数据
	sampleInner := []byte(`{"errno":0,"list":[{"fs_id":201,"path":"/shared/folder/sub/a.txt","server_filename":"a.txt","isdir":0}]}`)
	sampleRoot := []byte(`{"errno":0,"list":[{"fs_id":202,"path":"/forbidden/secret.txt","server_filename":"secret.txt","isdir":0}]}`)
	s.cache.Update(sampleInner)
	s.cache.SetDirList("/shared/folder", sampleInner)
	s.cache.SetDirList("/shared/folder/sub", sampleInner)
	s.cache.SetDirList("/forbidden", sampleRoot)

	token, err := s.dirLinks.Create("/shared/folder")
	if err != nil {
		t.Fatalf("create dir link failed: %v", err)
	}

	// 1. 尝试越权访问外部目录 /forbidden
	escapeReq := httptest.NewRequest(http.MethodPost, "/api/files", bytes.NewReader([]byte(`{"token":"`+token+`","dir":"/forbidden"}`)))
	escapeReq.Header.Set("Content-Type", "application/json")
	escapeRec := httptest.NewRecorder()
	s.handleFiles(escapeRec, escapeReq)
	if escapeRec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden on directory jailbreak, got %d: %s", escapeRec.Code, escapeRec.Body.String())
	}

	// 2. 正常请求允许目录，响应中必须包含 sandbox_root 与 resolved_dir
	validReq := httptest.NewRequest(http.MethodPost, "/api/files", bytes.NewReader([]byte(`{"token":"`+token+`","dir":"/shared/folder/sub"}`)))
	validReq.Header.Set("Content-Type", "application/json")
	validRec := httptest.NewRecorder()
	s.handleFiles(validRec, validReq)
	if validRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK within sandbox, got %d: %s", validRec.Code, validRec.Body.String())
	}

	var resp struct {
		ResolvedDir string `json:"resolved_dir"`
		SandboxRoot string `json:"sandbox_root"`
	}
	if err := json.Unmarshal(validRec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.SandboxRoot != "/shared/folder" {
		t.Fatalf("expected sandbox_root='/shared/folder', got %q", resp.SandboxRoot)
	}
	if resp.ResolvedDir != "/shared/folder/sub" {
		t.Fatalf("expected resolved_dir='/shared/folder/sub', got %q", resp.ResolvedDir)
	}
}

func TestHandleShareInfo_PasswordAndMeta(t *testing.T) {
	cfg := &config.Config{StateBackend: "none"}
	memFS := fstest.MapFS{"static/index.html": &fstest.MapFile{Data: []byte("<html></html>")}}
	s := NewServer(cfg, memFS, "test")
	defer s.Close()

	// 预置文件元数据缓存
	sampleDir := []byte(`{"errno":0,"list":[{"fs_id":301,"path":"/docs/guide.pdf","server_filename":"guide.pdf","size":123456,"isdir":0}]}`)
	s.cache.Update(sampleDir)

	token, err := s.shortLinks.CreateWithOptions("/docs/guide.pdf", "Ab12Cd", 0, -1)
	if err != nil {
		t.Fatalf("create shortlink failed: %v", err)
	}

	// 1. 未提供提取码：返回元数据与 password_valid=false
	req1 := httptest.NewRequest(http.MethodPost, "/api/share/info", bytes.NewReader([]byte(`{"token":"`+token+`"}`)))
	req1.Header.Set("Content-Type", "application/json")
	rec1 := httptest.NewRecorder()
	s.handleShareInfo(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rec1.Code, rec1.Body.String())
	}
	var res1 struct {
		FileName      string `json:"file_name"`
		HasPassword   bool   `json:"has_password"`
		PasswordValid bool   `json:"password_valid"`
	}
	_ = json.Unmarshal(rec1.Body.Bytes(), &res1)
	if res1.FileName != "guide.pdf" || !res1.HasPassword || res1.PasswordValid {
		t.Fatalf("unexpected res1: %+v", res1)
	}

	// 2. 提供错误提取码：返回 401
	req2 := httptest.NewRequest(http.MethodPost, "/api/share/info", bytes.NewReader([]byte(`{"token":"`+token+`","password":"wrong"}`)))
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	s.handleShareInfo(rec2, req2)
	if rec2.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized, got %d", rec2.Code)
	}

	// 3. 提供正确提取码（大小写不敏感）：返回 200 与 download_url
	req3 := httptest.NewRequest(http.MethodPost, "/api/share/info", bytes.NewReader([]byte(`{"token":"`+token+`","password":"ab12cd"}`)))
	req3.Header.Set("Content-Type", "application/json")
	rec3 := httptest.NewRecorder()
	s.handleShareInfo(rec3, req3)
	if rec3.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rec3.Code, rec3.Body.String())
	}
	var res3 struct {
		PasswordValid bool   `json:"password_valid"`
		Size          int64  `json:"size"`
		DownloadURL   string `json:"download_url"`
	}
	_ = json.Unmarshal(rec3.Body.Bytes(), &res3)
	if !res3.PasswordValid || res3.Size != 123456 || res3.DownloadURL == "" {
		t.Fatalf("unexpected res3: %+v", res3)
	}
}

func TestHandleShortDownload_RedirectToLandingPage(t *testing.T) {
	cfg := &config.Config{StateBackend: "none"}
	memFS := fstest.MapFS{"static/index.html": &fstest.MapFile{Data: []byte("<html></html>")}}
	s := NewServer(cfg, memFS, "test")
	defer s.Close()

	token, _ := s.shortLinks.CreateWithOptions("/test.txt", "123456", 0, -1)

	// 浏览器打开 /d/{token} 且未显式指定 download=1，应 302 重定向到单文件落地页
	req := httptest.NewRequest(http.MethodGet, "/d/"+token, nil)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	rec := httptest.NewRecorder()
	s.handleShortDownload(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected 302 redirect for browser, got %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if loc != "/?s="+token {
		t.Fatalf("expected Location to be '/?s=%s', got %q", token, loc)
	}
}

// TestHandleStream_ProbeMintsNoShareLink 核心回归：预览/下载走无状态中转，
// 不得铸造任何对外分享链接（既不进 shortLinks，也不进 dirLinks）。
func TestHandleStream_ProbeMintsNoShareLink(t *testing.T) {
	cfg := &config.Config{StateBackend: "none"}
	memFS := fstest.MapFS{"static/index.html": &fstest.MapFile{Data: []byte("<html></html>")}}
	s := NewServer(cfg, memFS, "test")
	defer s.Close()

	// 预置文件元数据缓存，避免探测请求打到百度
	sampleDir := []byte(`{"errno":0,"list":[{"fs_id":401,"path":"/media/clip.mp4","server_filename":"clip.mp4","size":2048,"isdir":0}]}`)
	s.cache.Update(sampleDir)

	req := httptest.NewRequest(http.MethodGet, "/api/stream?path=%2Fmedia%2Fclip.mp4", nil)
	req.Header.Set("X-Stream-Probe", "1")
	rec := httptest.NewRecorder()
	s.handleStream(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || !resp.OK {
		t.Fatalf("unexpected probe response: %s", rec.Body.String())
	}
	if links := s.shortLinks.List(); len(links) != 0 {
		t.Fatalf("expected no shortlinks minted, got %d: %+v", len(links), links)
	}
	if links := s.dirLinks.List(); len(links) != 0 {
		t.Fatalf("expected no dirlinks minted, got %d", len(links))
	}
}

func TestHandleStream_RejectsInvalidRequests(t *testing.T) {
	cfg := &config.Config{StateBackend: "none", AllowedPaths: []string{"/shared"}}
	memFS := fstest.MapFS{"static/index.html": &fstest.MapFile{Data: []byte("<html></html>")}}
	s := NewServer(cfg, memFS, "test")
	defer s.Close()

	// 预置目录项，用于验证目录被拒绝（isdir=1 走缓存，不打百度）
	dirSample := []byte(`{"errno":0,"list":[{"fs_id":501,"path":"/shared/movies","server_filename":"movies","isdir":1}]}`)
	s.cache.Update(dirSample)

	cases := []struct {
		name     string
		target   string
		wantCode int
	}{
		{"非法路径", "/api/stream?path=..%2Fetc%2Fpasswd", http.StatusBadRequest},
		{"越权路径", "/api/stream?path=%2Fother%2Fsecret.mp4", http.StatusForbidden},
		{"目录非文件", "/api/stream?path=%2Fshared%2Fmovies", http.StatusNotFound},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, tc.target, nil)
		req.Header.Set("X-Stream-Probe", "1")
		rec := httptest.NewRecorder()
		s.handleStream(rec, req)
		if rec.Code != tc.wantCode {
			t.Fatalf("%s: expected %d, got %d: %s", tc.name, tc.wantCode, rec.Code, rec.Body.String())
		}
	}
}

func TestHandleStream_AuthAndMethodGuards(t *testing.T) {
	cfg := &config.Config{StateBackend: "none", AccessToken: "top-secret-token"}
	memFS := fstest.MapFS{"static/index.html": &fstest.MapFile{Data: []byte("<html></html>")}}
	s := NewServer(cfg, memFS, "test")
	defer s.Close()
	handler := s.Handler()

	// 未携带会话：401（与其它预览接口同等鉴权）
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/stream?path=/a.mp4", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without session, got %d: %s", rec.Code, rec.Body.String())
	}

	// 仅允许 GET/HEAD：POST 返回 405（方法校验先于鉴权）
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, httptest.NewRequest(http.MethodPost, "/api/stream?path=/a.mp4", nil))
	if rec2.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 for POST, got %d", rec2.Code)
	}
}

func TestHandleSharesListAndRevoke(t *testing.T) {
	cfg := &config.Config{StateBackend: "none"}
	memFS := fstest.MapFS{"static/index.html": &fstest.MapFile{Data: []byte("<html></html>")}}
	s := NewServer(cfg, memFS, "test")
	defer s.Close()

	// 1. 创建一个文件短链和一个目录短链
	fileToken, err := s.shortLinks.CreateWithOptions("/test/report.pdf", "123456", time.Hour, 5)
	if err != nil {
		t.Fatalf("create shortlink failed: %v", err)
	}
	dirToken, err := s.dirLinks.CreateWithOptions("/docs", "", time.Hour)
	if err != nil {
		t.Fatalf("create dirlink failed: %v", err)
	}

	// 2. 测试 handleSharesList
	reqList := httptest.NewRequest(http.MethodGet, "/api/shares", nil)
	recList := httptest.NewRecorder()
	s.handleSharesList(recList, reqList)
	if recList.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", recList.Code, recList.Body.String())
	}
	var listResp struct {
		Files []struct {
			Token    string `json:"token"`
			FilePath string `json:"file_path"`
			URL      string `json:"url"`
		} `json:"files"`
		Dirs []struct {
			Token   string `json:"token"`
			Dir     string `json:"dir"`
			DirName string `json:"dir_name"`
			URL     string `json:"url"`
		} `json:"dirs"`
		Total int `json:"total"`
	}
	if err := json.Unmarshal(recList.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("unmarshal list response failed: %v", err)
	}
	if listResp.Total != 2 || len(listResp.Files) != 1 || len(listResp.Dirs) != 1 {
		t.Fatalf("unexpected list count: %+v", listResp)
	}
	if listResp.Files[0].Token != fileToken || listResp.Dirs[0].Token != dirToken {
		t.Fatalf("tokens do not match: %+v", listResp)
	}

	// 3. 测试 handleShareRevoke 撤销文件短链
	reqRevokeFile := httptest.NewRequest(http.MethodPost, "/api/shares/revoke", bytes.NewReader([]byte(`{"token":"`+fileToken+`"}`)))
	reqRevokeFile.Header.Set("Content-Type", "application/json")
	recRevokeFile := httptest.NewRecorder()
	s.handleShareRevoke(recRevokeFile, reqRevokeFile)
	if recRevokeFile.Code != http.StatusOK {
		t.Fatalf("revoke file share failed: %d: %s", recRevokeFile.Code, recRevokeFile.Body.String())
	}

	// 重复撤销同一文件短链应返回 404
	recRevokeFile2 := httptest.NewRecorder()
	reqRevokeFile2 := httptest.NewRequest(http.MethodPost, "/api/shares/revoke", bytes.NewReader([]byte(`{"token":"`+fileToken+`"}`)))
	reqRevokeFile2.Header.Set("Content-Type", "application/json")
	s.handleShareRevoke(recRevokeFile2, reqRevokeFile2)
	if recRevokeFile2.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for repeated revoke, got %d", recRevokeFile2.Code)
	}

	// 4. 测试 handleShareRevoke 撤销目录短链
	reqRevokeDir := httptest.NewRequest(http.MethodPost, "/api/shares/revoke", bytes.NewReader([]byte(`{"token":"`+dirToken+`"}`)))
	reqRevokeDir.Header.Set("Content-Type", "application/json")
	recRevokeDir := httptest.NewRecorder()
	s.handleShareRevoke(recRevokeDir, reqRevokeDir)
	if recRevokeDir.Code != http.StatusOK {
		t.Fatalf("revoke dir share failed: %d: %s", recRevokeDir.Code, recRevokeDir.Body.String())
	}

	// 5. 再次查询 handleSharesList，总数应为 0
	recList2 := httptest.NewRecorder()
	reqList2 := httptest.NewRequest(http.MethodGet, "/api/shares", nil)
	s.handleSharesList(recList2, reqList2)
	var listResp2 struct {
		Total int `json:"total"`
	}
	_ = json.Unmarshal(recList2.Body.Bytes(), &listResp2)
	if listResp2.Total != 0 {
		t.Fatalf("expected 0 total shares, got %d", listResp2.Total)
	}
}

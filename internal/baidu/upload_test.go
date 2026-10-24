package baidu

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ekishion/jikeqingpan/internal/config"
)

func newUploadTestConfig(apiURL, pcsURL string) *config.Config {
	return &config.Config{
		BaiduCookie:     "BDUSS=test-cookie",
		BaiduAppID:      "250528",
		BaiduBaseURL:    apiURL,
		BaiduPCSBaseURL: pcsURL,
	}
}

func TestGetQuota(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/youth/api/quota" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Cookie"); got != "BDUSS=test-cookie" {
			t.Errorf("cookie not injected: %q", got)
		}
		_, _ = w.Write([]byte(`{"errno":0,"total":1000,"used":400}`))
	}))
	defer srv.Close()

	client := NewClient(newUploadTestConfig(srv.URL, srv.URL), nil, srv.Client(), srv.URL)
	quota, err := client.GetQuota()
	if err != nil {
		t.Fatalf("GetQuota failed: %v", err)
	}
	if quota.Total != 1000 || quota.Used != 400 || quota.Free != 600 {
		t.Fatalf("unexpected quota: %+v", quota)
	}
}

func TestGetQuota_ErrnoMapped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"errno":-6}`))
	}))
	defer srv.Close()

	client := NewClient(newUploadTestConfig(srv.URL, srv.URL), nil, srv.Client(), srv.URL)
	if _, err := client.GetQuota(); err == nil {
		t.Fatal("expected error for non-zero errno")
	}
}

// fakeBaiduUpload 承载 /youth/api/precreate 与 /youth/api/create 的假服务器。
type fakeBaiduUpload struct {
	srv           *httptest.Server
	precreateHits atomic.Int32
	createHits    atomic.Int32
	precreateURI  string
	createURI     string
	precreateResp func() string
}

func startFakeBaiduUpload(t *testing.T) *fakeBaiduUpload {
	t.Helper()
	fake := &fakeBaiduUpload{}
	mux := http.NewServeMux()
	mux.HandleFunc("/youth/api/precreate", func(w http.ResponseWriter, r *http.Request) {
		fake.precreateURI = r.URL.RawQuery
		fake.precreateHits.Add(1)
		resp := `{"errno":0,"uploadid":"up-1","return_type":1}`
		if fake.precreateResp != nil {
			resp = fake.precreateResp()
		}
		_, _ = w.Write([]byte(resp))
	})
	mux.HandleFunc("/youth/api/create", func(w http.ResponseWriter, r *http.Request) {
		fake.createURI = r.URL.RawQuery
		fake.createHits.Add(1)
		_, _ = w.Write([]byte(`{"errno":0}`))
	})
	fake.srv = httptest.NewServer(mux)
	t.Cleanup(fake.srv.Close)
	return fake
}

func TestUploadFlow_PrecreateChunkCreate(t *testing.T) {
	fakeBaidu := startFakeBaiduUpload(t)
	content := []byte("hello 上传 content")
	chunkMD5 := md5.Sum(content) // #nosec G501 -- 百度契约要求

	var pcsChunk []byte
	var pcsQuery url.Values
	pcsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/rest/2.0/pcs/superfile2") {
			t.Errorf("unexpected pcs path: %s", r.URL.Path)
		}
		pcsQuery = r.URL.Query()
		if got := r.URL.Query().Get("method"); got != "upload" {
			t.Errorf("unexpected method param: %q", got)
		}
		if got := r.Header.Get("Cookie"); got != "BDUSS=test-cookie" {
			t.Errorf("pcs cookie not injected: %q", got)
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			t.Errorf("multipart field file: %v", err)
			return
		}
		defer file.Close()
		pcsChunk, err = io.ReadAll(file)
		if err != nil {
			t.Errorf("read chunk: %v", err)
		}
		_, _ = w.Write([]byte(`{"errno":0}`))
	}))
	defer pcsSrv.Close()

	cfg := newUploadTestConfig(fakeBaidu.srv.URL, pcsSrv.URL)
	client := NewClient(cfg, nil, fakeBaidu.srv.Client(), fakeBaidu.srv.URL)

	pre, err := client.Precreate("/dir/hello.txt", int64(len(content)), []string{hex.EncodeToString(chunkMD5[:])})
	if err != nil {
		t.Fatalf("Precreate failed: %v", err)
	}
	if pre.UploadID != "up-1" || pre.ReturnType != 1 {
		t.Fatalf("unexpected precreate result: %+v", pre)
	}
	if err := client.UploadChunk("/dir/hello.txt", pre.UploadID, 0, content); err != nil {
		t.Fatalf("UploadChunk failed: %v", err)
	}
	if err := client.CreateFile("/dir/hello.txt", int64(len(content)), pre.UploadID, []string{hex.EncodeToString(chunkMD5[:])}); err != nil {
		t.Fatalf("CreateFile failed: %v", err)
	}

	// 青春版接口的参数全部位于 query string
	preQuery, err := url.ParseQuery(fakeBaidu.precreateURI)
	if err != nil {
		t.Fatalf("precreate query parse: %v", err)
	}
	if got := preQuery.Get("path"); got != "/dir/hello.txt" {
		t.Errorf("precreate path: %q", got)
	}
	if got := preQuery.Get("size"); got != strconv.Itoa(len(content)) {
		t.Errorf("precreate size: %q", got)
	}
	if got := preQuery.Get("autoinit"); got != "1" {
		t.Errorf("precreate autoinit: %q", got)
	}
	var blocks []string
	if err := json.Unmarshal([]byte(preQuery.Get("block_list")), &blocks); err != nil {
		t.Fatalf("precreate block_list: %v", err)
	}
	if len(blocks) != 1 || blocks[0] != hex.EncodeToString(chunkMD5[:]) {
		t.Errorf("precreate block_list: %v", blocks)
	}
	if !bytes.Equal(pcsChunk, content) {
		t.Errorf("chunk content mismatch: %q vs %q", pcsChunk, content)
	}
	if got := pcsQuery.Get("uploadid"); got != "up-1" {
		t.Errorf("pcs uploadid: %q", got)
	}
	if got := pcsQuery.Get("partseq"); got != "0" {
		t.Errorf("pcs partseq: %q", got)
	}
	createQuery, err := url.ParseQuery(fakeBaidu.createURI)
	if err != nil {
		t.Fatalf("create query parse: %v", err)
	}
	if got := createQuery.Get("uploadid"); got != "up-1" {
		t.Errorf("create uploadid: %q", got)
	}
	if got := createQuery.Get("path"); got != "/dir/hello.txt" {
		t.Errorf("create path: %q", got)
	}
}

func TestPrecreate_FileExistsSentinel(t *testing.T) {
	fakeBaidu := startFakeBaiduUpload(t)
	fakeBaidu.precreateResp = func() string { return `{"errno":-8,"show_err":"文件已存在"}` }

	client := NewClient(newUploadTestConfig(fakeBaidu.srv.URL, fakeBaidu.srv.URL), nil, fakeBaidu.srv.Client(), fakeBaidu.srv.URL)
	_, err := client.Precreate("/dir/a.txt", 10, []string{"x"})
	if !errors.Is(err, ErrFileExists) {
		t.Fatalf("expected ErrFileExists, got: %v", err)
	}
}

func TestPrecreate_InvalidParamSentinel(t *testing.T) {
	fakeBaidu := startFakeBaiduUpload(t)
	fakeBaidu.precreateResp = func() string { return `{"errno":-7,"show_err":"param path is invalid"}` }

	client := NewClient(newUploadTestConfig(fakeBaidu.srv.URL, fakeBaidu.srv.URL), nil, fakeBaidu.srv.Client(), fakeBaidu.srv.URL)
	_, err := client.Precreate("/dir/a.txt", 10, []string{"x"})
	if !errors.Is(err, ErrInvalidParam) {
		t.Fatalf("expected ErrInvalidParam, got: %v", err)
	}
}

func TestPrecreate_IdentityRetry(t *testing.T) {
	fakeBaidu := startFakeBaiduUpload(t)
	fakeBaidu.precreateResp = func() string {
		if fakeBaidu.precreateHits.Load() == 1 {
			return `{"errno":-6}`
		}
		return `{"errno":0,"uploadid":"up-2","return_type":1}`
	}

	client := NewClient(newUploadTestConfig(fakeBaidu.srv.URL, fakeBaidu.srv.URL), nil, fakeBaidu.srv.Client(), fakeBaidu.srv.URL)
	pre, err := client.Precreate("/dir/a.txt", 10, []string{"x"})
	if err != nil {
		t.Fatalf("Precreate should retry once on errno -6: %v", err)
	}
	if pre.UploadID != "up-2" {
		t.Fatalf("unexpected uploadid: %+v", pre)
	}
	if got := fakeBaidu.precreateHits.Load(); got != 2 {
		t.Fatalf("expected 2 precreate calls, got %d", got)
	}
}

func TestClearSession_ResetsSessionState(t *testing.T) {
	client := NewClient(newUploadTestConfig("https://example.invalid", ""), nil, nil, "https://example.invalid")
	client.sessionMu.Lock()
	client.uk = 42
	client.sk = "sk"
	client.sessionMu.Unlock()
	client.ClearSession()
	client.sessionMu.Lock()
	defer client.sessionMu.Unlock()
	if client.uk != 0 || client.sk != "" {
		t.Fatal("ClearSession should reset uk/sk")
	}
}

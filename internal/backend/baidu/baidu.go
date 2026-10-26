// Package baidu 实现百度网盘青春版后端。
//
// 它只依赖 docs/protocol.md 记录的接口形态。这些接口不是官方开放 API，
// 没有稳定性承诺，随时可能变化。
package baidu

import (
	"context"
	"crypto/md5"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ekishion/hibari/internal/library"
)

// Config 是适配器的构造参数。它不 import internal/config，
// 由装配点做字段搬运。
type Config struct {
	Cookie  string
	AppID   string
	BaseURL string
	// Client 为 nil 时使用带超时的默认客户端。
	Client *http.Client
}

// Backend 实现 library.Backend 与 library.Linker。
type Backend struct {
	cookie string
	appID  string
	base   string
	// client 用于 JSON 接口，有总超时。
	client *http.Client
	// dl 用于文件流，没有总超时：Timeout 覆盖响应体读取，
	// 设了会把超过该时长的大文件下载拦腰切断。
	dl *http.Client

	// 会话凭据缓存。签名失败时整体清空重取。
	mu      sync.Mutex
	cred    credentials
	credExp time.Time

	// 直链缓存，键为路径 + User-Agent。
	links sync.Map
}

type credentials struct {
	uk string
	sk string
}

// New 构造后端。Cookie 缺失时仍可构造，请求时才会失败。
func New(cfg Config) (*Backend, error) {
	base := strings.TrimSuffix(cfg.BaseURL, "/")
	if base == "" {
		base = "https://pan.baidu.com"
	}
	u, err := url.Parse(base)
	if err != nil {
		return nil, fmt.Errorf("base url 无法解析: %w", err)
	}
	// Cookie 随每个请求发出，明文链路会直接泄露凭据。
	// 仅回环地址（本地测试替身）允许 http。
	if u.Scheme != "https" && !isLoopback(u.Hostname()) {
		return nil, fmt.Errorf("base url 必须是 https: %s", base)
	}
	appID := cfg.AppID
	if appID == "" {
		appID = "250528"
	}
	client := cfg.Client
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	return &Backend{
		cookie: strings.TrimSpace(cfg.Cookie),
		appID:  appID,
		base:   base,
		client: client,
		// 共用 Transport（连接池），但不继承总超时。
		dl: &http.Client{Transport: client.Transport},
	}, nil
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Caps 报告本后端能力。写操作按 protocol.md §6 有意不实现。
func (b *Backend) Caps() library.Caps {
	return library.Caps{Write: false, Upload: false, Quota: true}
}

// ---- 会话凭据 ----

// credentials 取 uk/sk，带缓存与多级回退。
func (b *Backend) credentials(ctx context.Context) (credentials, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.cred.uk != "" && b.cred.sk != "" && time.Now().Before(b.credExp) {
		return b.cred, nil
	}

	var firstErr error
	for _, fetch := range []func(context.Context) (credentials, error){
		b.fetchCredentialsGetInfo,
		b.fetchCredentialsTemplate,
		b.fetchCredentialsReport,
	} {
		c, err := fetch(ctx)
		if err == nil && c.uk != "" && c.sk != "" {
			b.cred = c
			b.credExp = time.Now().Add(time.Hour)
			return c, nil
		}
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if firstErr == nil {
		firstErr = fmt.Errorf("%w: 无法取得会话凭据，请检查 Cookie 是否有效", library.ErrUpstream)
	}
	return credentials{}, firstErr
}

func (b *Backend) invalidateCredentials() {
	b.mu.Lock()
	b.cred = credentials{}
	b.credExp = time.Time{}
	b.mu.Unlock()
	// 凭据刷新意味着旧直链多半也已失效，一并丢弃。
	// shortcut: 缓存条目数随「访问路径 × UA」增长，靠读取时懒删回收；
	// 若出现大量不同 UA 的来源，再补一个定期清理。
	b.links.Range(func(k, _ any) bool {
		b.links.Delete(k)
		return true
	})
}

func (b *Backend) fetchCredentialsGetInfo(ctx context.Context) (credentials, error) {
	var resp struct {
		Records []struct {
			UK json.Number `json:"uk"`
			SK string      `json:"sk"`
		} `json:"records"`
	}
	if err := b.getJSON(ctx, "/youth/api/user/getinfo", url.Values{
		"need_selfinfo": {"1"},
	}, &resp); err != nil {
		return credentials{}, err
	}
	if len(resp.Records) == 0 {
		return credentials{}, fmt.Errorf("%w: getinfo 未返回用户记录", library.ErrUpstream)
	}
	return credentials{uk: resp.Records[0].UK.String(), sk: resp.Records[0].SK}, nil
}

func (b *Backend) fetchCredentialsTemplate(ctx context.Context) (credentials, error) {
	var resp struct {
		Result struct {
			UK json.Number `json:"uk"`
			SK string      `json:"sk"`
		} `json:"result"`
	}
	if err := b.getJSON(ctx, "/api/gettemplatevariable", url.Values{
		"fields": {`["bdstoken","uk","sk"]`},
	}, &resp); err != nil {
		return credentials{}, err
	}
	return credentials{uk: resp.Result.UK.String(), sk: resp.Result.SK}, nil
}

func (b *Backend) fetchCredentialsReport(ctx context.Context) (credentials, error) {
	var resp struct {
		UK    json.Number `json:"uk"`
		UInfo string      `json:"uinfo"`
	}
	if err := b.getJSON(ctx, "/youth/api/report/user", url.Values{
		"action":    {"sapi_auth"},
		"timestamp": {strconv.FormatInt(time.Now().UnixMilli(), 10)},
	}, &resp); err != nil {
		return credentials{}, err
	}
	return credentials{uk: resp.UK.String(), sk: resp.UInfo}, nil
}

// ---- library.Backend ----

// List 逐页拉取目录内容，直到某页不足一页为止。
func (b *Backend) List(ctx context.Context, dir string) ([]library.Entry, error) {
	dir, err := library.CleanPath(dir)
	if err != nil {
		return nil, err
	}

	const pageSize = 100
	// 上限只是防止上游分页异常导致死循环。触顶时返回错误而不是残缺列表，
	// 否则用户会以为目录里真的只有这么多文件。
	const maxPages = 1000
	out := make([]library.Entry, 0, pageSize)
	for page := 1; ; page++ {
		if page > maxPages {
			return nil, fmt.Errorf("%w: 目录条目超过 %d 项", library.ErrUpstream, maxPages*pageSize)
		}
		var resp struct {
			Errno int `json:"errno"`
			List  []struct {
				ServerFilename string      `json:"server_filename"`
				Path           string      `json:"path"`
				IsDir          int         `json:"isdir"`
				Size           json.Number `json:"size"`
				ServerMtime    json.Number `json:"server_mtime"`
				FsID           json.Number `json:"fs_id"`
				MD5            string      `json:"md5"`
			} `json:"list"`
		}
		err := b.getJSON(ctx, "/youth/api/list", url.Values{
			"dir":   {dir},
			"order": {"time"},
			"desc":  {"1"},
			"num":   {strconv.Itoa(pageSize)},
			"page":  {strconv.Itoa(page)},
		}, &resp)
		if err != nil {
			return nil, err
		}
		if resp.Errno != 0 {
			return nil, errnoToError(resp.Errno)
		}

		for _, item := range resp.List {
			// 协议要求 dlink 绝不下发，此处只取白名单字段，
			// 结构体未声明 dlink 即天然剥离。
			size, _ := item.Size.Int64()
			mtime, _ := item.ServerMtime.Int64()
			out = append(out, library.Entry{
				Name:    item.ServerFilename,
				Path:    item.Path,
				IsDir:   item.IsDir == 1,
				Size:    size,
				ModTime: time.Unix(mtime, 0).UTC(),
				Meta: library.Meta{
					ID:   item.FsID.String(),
					Hash: item.MD5,
				},
			})
		}
		if len(resp.List) < pageSize {
			break
		}
	}
	return out, nil
}

// Stat 通过列出父目录实现。上游没有单文件查询接口。
func (b *Backend) Stat(ctx context.Context, p string) (library.Entry, error) {
	p, err := library.CleanPath(p)
	if err != nil {
		return library.Entry{}, err
	}
	if p == "/" {
		return library.Entry{Name: "/", Path: "/", IsDir: true}, nil
	}

	parent := "/"
	if i := strings.LastIndex(p, "/"); i > 0 {
		parent = p[:i]
	}
	entries, err := b.List(ctx, parent)
	if err != nil {
		return library.Entry{}, err
	}
	for _, e := range entries {
		if e.Path == p {
			return e, nil
		}
	}
	return library.Entry{}, library.ErrNotFound
}

// Open 先取直链再下载。直链与 UA 绑定，这里固定使用本适配器的 UA，
// 保证签名与请求一致。目录与不存在的路径由 DirectURL 内部的 Stat 拦下。
func (b *Backend) Open(ctx context.Context, p string) (io.ReadCloser, error) {
	link, err := b.DirectURL(ctx, p, internalUA)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return nil, fmt.Errorf("构造下载请求: %w", err)
	}
	req.Header.Set("User-Agent", internalUA)

	resp, err := b.dl.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", library.ErrUpstream, err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("%w: 下载返回 %d", library.ErrUpstream, resp.StatusCode)
	}
	return resp.Body, nil
}

// Quota 返回青春版独立空间容量。
func (b *Backend) Quota(ctx context.Context) (library.Quota, error) {
	var resp struct {
		Errno int         `json:"errno"`
		Total json.Number `json:"total"`
		Used  json.Number `json:"used"`
	}
	if err := b.getJSON(ctx, "/youth/api/quota", url.Values{
		"checkfree":   {"1"},
		"checkexpire": {"1"},
	}, &resp); err != nil {
		return library.Quota{}, err
	}
	if resp.Errno != 0 {
		return library.Quota{}, errnoToError(resp.Errno)
	}
	total, _ := resp.Total.Int64()
	used, _ := resp.Used.Int64()
	return library.Quota{Total: total, Used: used}, nil
}

// ---- library.Linker ----

// internalUA 用于 Open 自身发起的下载请求，与签名绑定。
const internalUA = "hibari/baidu-adapter"

// DirectURL 计算签名并换取直链。结果按 (路径, UA) 缓存 300 秒。
func (b *Backend) DirectURL(ctx context.Context, p, ua string) (string, error) {
	p, err := library.CleanPath(p)
	if err != nil {
		return "", err
	}
	if ua == "" {
		ua = internalUA
	}

	cacheKey := p + "\x00" + ua
	if v, ok := b.links.Load(cacheKey); ok {
		if item := v.(linkEntry); time.Now().Before(item.exp) {
			return item.url, nil
		}
		b.links.Delete(cacheKey)
	}

	entry, err := b.Stat(ctx, p)
	if err != nil {
		return "", err
	}
	if entry.IsDir {
		return "", library.ErrNotFile
	}

	link, err := b.signedLink(ctx, entry)
	if err != nil {
		return "", err
	}
	b.links.Store(cacheKey, linkEntry{url: link, exp: time.Now().Add(5 * time.Minute)})
	return link, nil
}

type linkEntry struct {
	url string
	exp time.Time
}

// signedLink 执行 protocol.md §3 的签名流程。签名失败一次时清空凭据重试，
// 应对 sk 提前失效。
func (b *Backend) signedLink(ctx context.Context, entry library.Entry) (string, error) {
	link, err := b.requestLink(ctx, entry)
	if err == nil {
		return link, nil
	}
	if !errors.Is(err, errAuthFailed) {
		return "", err
	}
	b.invalidateCredentials()
	return b.requestLink(ctx, entry)
}

// errAuthFailed 既是「清空凭据重试」的信号，也是一个已映射的领域错误：
// 它同时包装 ErrUpstream，所以重试仍失败时 HTTP 层能返回 502 与可操作的
// 原因，而不是落到 default 分支变成「服务内部错误」。
var errAuthFailed = fmt.Errorf("%w: 身份校验失败，请更新 Cookie", library.ErrUpstream)

// authFailed 在保留哨兵身份的前提下附加上游给出的原因。
func authFailed(msg string) error {
	if msg == "" {
		return errAuthFailed
	}
	return fmt.Errorf("%w: %s", errAuthFailed, msg)
}

func (b *Backend) requestLink(ctx context.Context, entry library.Entry) (string, error) {
	cred, err := b.credentials(ctx)
	if err != nil {
		return "", err
	}

	now := strconv.FormatInt(time.Now().UnixMilli(), 10)
	rand := hexSHA1(cred.uk + cred.sk + now + "0")
	sign := hexMD5(entry.Meta.Hash + "_" + cred.uk + "_" + entry.Meta.ID + "_" + now)

	var resp struct {
		Errno   int    `json:"errno"`
		URL     string `json:"url"`
		ShowMsg string `json:"show_msg"`
	}
	err = b.getJSON(ctx, "/youth/api/locatedownload", url.Values{
		"devuid":   {"0"},
		"dp-logid": {strconv.FormatInt(time.Now().UnixNano(), 10)},
		"path":     {entry.Path},
		"rand":     {rand},
		"sign":     {sign},
		"time":     {now},
	}, &resp)
	if err != nil {
		return "", err
	}
	if resp.Errno == -6 {
		return "", authFailed(resp.ShowMsg)
	}
	if resp.Errno != 0 {
		return "", errnoToError(resp.Errno)
	}
	if err := checkLinkHost(resp.URL); err != nil {
		return "", err
	}
	return resp.URL, nil
}

// checkLinkHost 拒绝非百度域名的直链，避免上游把用户导向任意地址。
func checkLinkHost(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%w: 直链无法解析", library.ErrUpstream)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("%w: 直链协议必须为 https", library.ErrUpstream)
	}
	host := strings.ToLower(u.Hostname())
	for _, allowed := range []string{"baidu.com", "baidupcs.com", "bdstatic.com"} {
		if host == allowed || strings.HasSuffix(host, "."+allowed) {
			return nil
		}
	}
	return fmt.Errorf("%w: 直链域名 %s 不在允许范围", library.ErrUpstream, host)
}

func hexSHA1(s string) string {
	sum := sha1.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

func hexMD5(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

// ---- 请求辅助 ----

// getJSON 发起 GET 并解析 JSON。上游错误统一转为 ErrUpstream 或领域错误。
func (b *Backend) getJSON(ctx context.Context, endpoint string, params url.Values, dst any) error {
	if b.cookie == "" {
		return fmt.Errorf("%w: 未配置 Cookie", library.ErrUpstream)
	}

	q := url.Values{}
	q.Set("app_id", b.appID)
	q.Set("clienttype", "0")
	q.Set("web", "1")
	for k, vs := range params {
		for _, v := range vs {
			q.Set(k, v)
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		b.base+endpoint+"?"+q.Encode(), nil)
	if err != nil {
		return fmt.Errorf("构造请求: %w", err)
	}
	req.Header.Set("Cookie", b.cookie)
	req.Header.Set("Referer", "https://pan.baidu.com/")
	req.Header.Set("User-Agent", internalUA)

	resp, err := b.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", library.ErrUpstream, err)
	}
	defer resp.Body.Close()

	// 风控触发时百度会返回登录页 HTML，此时解析 JSON 会失败。
	ctype := resp.Header.Get("Content-Type")
	if !strings.Contains(ctype, "json") {
		return fmt.Errorf("%w: 响应类型 %q，可能已触发风控或 Cookie 失效",
			library.ErrUpstream, ctype)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: HTTP %d", library.ErrUpstream, resp.StatusCode)
	}

	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(dst); err != nil {
		return fmt.Errorf("%w: 解析响应失败: %v", library.ErrUpstream, err)
	}
	return nil
}

// errnoToError 把上游错误码翻译为领域错误。
func errnoToError(errno int) error {
	switch errno {
	case -6:
		return errAuthFailed
	case -7:
		return fmt.Errorf("%w: 上游拒绝该参数", library.ErrInvalid)
	case -8:
		return library.ErrExists
	case -9:
		return library.ErrNotFound
	case 4:
		return library.ErrQuotaFull
	default:
		return fmt.Errorf("%w: errno %d", library.ErrUpstream, errno)
	}
}

// 编译期断言：适配器必须满足消费方声明的接口。
var (
	_ library.Backend = (*Backend)(nil)
	_ library.Linker  = (*Backend)(nil)
)

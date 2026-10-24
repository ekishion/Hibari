package baidu

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
)

// UploadChunkTimeout 单个分片上传请求的超时：分片最大 16MB，弱网上行也需
// 留足时间，独立于普通 API 请求的 BaiduClientTimeout。
const UploadChunkTimeout = 5 * time.Minute

// defaultPCSBaseURL 分片上传的固定目标域名。
const defaultPCSBaseURL = "https://d.pcs.baidu.com"

// 上传链路的哨兵错误：handler 据此映射为可读的 API 错误码。
var (
	// ErrFileExists 目标位置已存在同名文件（百度 errno=-8）。
	ErrFileExists = errors.New("目标位置已存在同名文件")
	// ErrQuotaExceeded 网盘剩余容量不足（百度 errno=4）。
	ErrQuotaExceeded = errors.New("网盘剩余容量不足")
	// ErrInvalidParam 百度认为请求参数非法（errno=-7，常见于路径/文件名不被接受）。
	ErrInvalidParam = errors.New("百度认为请求参数非法")
	// ErrIdentityExpired 身份令牌过期（errno=-6），清除会话后可重试一次。
	ErrIdentityExpired = errors.New("百度身份校验失败")
)

// Quota 网盘容量信息（字节）。
type Quota struct {
	Total int64 `json:"total"`
	Used  int64 `json:"used"`
	Free  int64 `json:"free"`
}

// PrecreateResult precreate 应答：UploadID 供分片上传使用；
// ReturnType==2 表示秒传命中，可跳过分片直接 CreateFile（响应缺失时按需上传处理）。
type PrecreateResult struct {
	UploadID   string
	ReturnType int
}

// GetQuota 获取青春版网盘容量信息（/youth/api/quota，Cookie 鉴权）。
// 青春版空间与主网盘独立，必须走 youth 域才能拿到本应用的容量。
func (c *Client) GetQuota() (Quota, error) {
	query := url.Values{}
	query.Set("checkfree", "1")
	query.Set("checkexpire", "1")
	body, err := c.BaiduGet(c.youthAPIURL("/youth/api/quota", query), "")
	if err != nil {
		return Quota{}, err
	}
	var resp struct {
		Errno int   `json:"errno"`
		Total int64 `json:"total"`
		Used  int64 `json:"used"`
		Free  int64 `json:"free"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return Quota{}, fmt.Errorf("解析配额响应失败: %w", err)
	}
	if resp.Errno != 0 {
		return Quota{}, fmt.Errorf("百度配额接口返回错误 errno=%d", resp.Errno)
	}
	// 百度 youth quota 的 free 字段不可信（实测会返回等于 total 的值），
	// 剩余容量一律按 total-used 计算。
	free := resp.Total - resp.Used
	if free < 0 {
		free = 0
	}
	return Quota{Total: resp.Total, Used: resp.Used, Free: free}, nil
}

// Precreate 预上传：提交分片 MD5 清单获取 uploadid（/youth/api/precreate）。
// 青春版接口的所有参数都在 query string 上（form body 会得到 errno=-7）。
// 身份令牌过期（errno=-6）时清除会话重试一次，与 locatedownload 的重试策略一致。
func (c *Client) Precreate(filePath string, size int64, blockList []string) (*PrecreateResult, error) {
	result, err := c.precreate(filePath, size, blockList)
	if err == nil || !errors.Is(err, ErrIdentityExpired) {
		return result, err
	}
	c.ClearSession()
	return c.precreate(filePath, size, blockList)
}

func (c *Client) precreate(filePath string, size int64, blockList []string) (*PrecreateResult, error) {
	blocksJSON, err := json.Marshal(blockList)
	if err != nil {
		return nil, err
	}
	query := url.Values{}
	query.Set("path", filePath)
	query.Set("size", strconv.FormatInt(size, 10))
	query.Set("isdir", "0")
	query.Set("autoinit", "1")
	query.Set("block_list", string(blocksJSON))
	body, err := c.BaiduPostQuery(c.youthAPIURL("/youth/api/precreate", query), "")
	if err != nil {
		return nil, err
	}
	var resp struct {
		Errno      int    `json:"errno"`
		UploadID   string `json:"uploadid"`
		ReturnType int    `json:"return_type"`
		ShowErr    string `json:"show_err"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("解析 precreate 响应失败: %w", err)
	}
	if resp.Errno != 0 {
		return nil, uploadErrnoError("precreate", resp.Errno, resp.ShowErr)
	}
	if resp.ReturnType != 2 && resp.UploadID == "" {
		return nil, fmt.Errorf("precreate 响应缺少 uploadid")
	}
	return &PrecreateResult{UploadID: resp.UploadID, ReturnType: resp.ReturnType}, nil
}

// UploadChunk 上传单个分片到 d.pcs.baidu.com（multipart 字段名固定为 file）。
func (c *Client) UploadChunk(filePath, uploadID string, partseq int, chunk []byte) error {
	params := url.Values{}
	params.Set("method", "upload")
	params.Set("app_id", c.cfg.BaiduAppID)
	params.Set("clienttype", "0")
	params.Set("web", "1")
	params.Set("path", filePath)
	params.Set("uploadid", uploadID)
	params.Set("partseq", strconv.Itoa(partseq))
	apiURL := c.pcsUploadBaseURL() + "/rest/2.0/pcs/superfile2?" + params.Encode()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", path.Base(filePath))
	if err != nil {
		return fmt.Errorf("创建分片表单失败: %w", err)
	}
	if _, err := part.Write(chunk); err != nil {
		return fmt.Errorf("写入分片数据失败: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("结束分片表单失败: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, apiURL, body)
	if err != nil {
		return fmt.Errorf("创建分片请求失败: %w", err)
	}
	req.ContentLength = int64(body.Len())
	req.Header.Set("Content-Type", writer.FormDataContentType())
	c.setBaiduHeaders(req, "")
	respBody, err := c.readAPIResponse(req, c.uploadClient)
	if err != nil {
		return err
	}
	var resp struct {
		Errno   int    `json:"errno"`
		ShowErr string `json:"show_err"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return fmt.Errorf("解析分片上传响应失败: %w", err)
	}
	if resp.Errno != 0 {
		return uploadErrnoError("分片上传", resp.Errno, resp.ShowErr)
	}
	return nil
}

// CreateFile 提交分片清单完成文件创建（/youth/api/create，参数在 query string）。
// 身份令牌过期（errno=-6）时清除会话重试一次。
func (c *Client) CreateFile(filePath string, size int64, uploadID string, blockList []string) error {
	err := c.createFile(filePath, size, uploadID, blockList)
	if err == nil || !errors.Is(err, ErrIdentityExpired) {
		return err
	}
	c.ClearSession()
	return c.createFile(filePath, size, uploadID, blockList)
}

func (c *Client) createFile(filePath string, size int64, uploadID string, blockList []string) error {
	blocksJSON, err := json.Marshal(blockList)
	if err != nil {
		return err
	}
	query := url.Values{}
	query.Set("path", filePath)
	query.Set("size", strconv.FormatInt(size, 10))
	query.Set("isdir", "0")
	query.Set("block_list", string(blocksJSON))
	if uploadID != "" {
		query.Set("uploadid", uploadID)
	}
	body, err := c.BaiduPostQuery(c.youthAPIURL("/youth/api/create", query), "")
	if err != nil {
		return err
	}
	var resp struct {
		Errno   int    `json:"errno"`
		ShowErr string `json:"show_err"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("解析 create 响应失败: %w", err)
	}
	if resp.Errno != 0 {
		return uploadErrnoError("create", resp.Errno, resp.ShowErr)
	}
	return nil
}

// youthAPIURL 拼接青春版接口地址：公共参数在前、调用方 query 在后
// （Encode 的标准转义 Youth 接口可正确解码，实测中文/空格/&/斜杠均正常）。
func (c *Client) youthAPIURL(apiPath string, query url.Values) string {
	return fmt.Sprintf("%s%s?app_id=%s&web=1&clienttype=0&%s",
		c.baseURL, apiPath, url.QueryEscape(c.cfg.BaiduAppID), query.Encode())
}

// pcsUploadBaseURL 分片上传域名，测试可经 baidu_pcs_base_url 注入替换。
func (c *Client) pcsUploadBaseURL() string {
	if c.cfg != nil {
		if v := strings.TrimRight(strings.TrimSpace(c.cfg.BaiduPCSBaseURL), "/"); v != "" {
			return v
		}
	}
	return defaultPCSBaseURL
}

// uploadErrnoError 把百度上传链路的业务 errno 映射为可读错误。
func uploadErrnoError(stage string, errno int, showErr string) error {
	switch errno {
	case -6:
		return ErrIdentityExpired
	case -7:
		return fmt.Errorf("%w（%s 返回 errno=-7）", ErrInvalidParam, stage)
	case -8:
		return fmt.Errorf("%w（%s 返回 errno=-8）", ErrFileExists, stage)
	case 4:
		return fmt.Errorf("%w（%s 返回 errno=4）", ErrQuotaExceeded, stage)
	}
	if showErr != "" {
		return fmt.Errorf("百度 %s 返回错误 errno=%d, msg=%q", stage, errno, showErr)
	}
	return fmt.Errorf("百度 %s 返回错误 errno=%d", stage, errno)
}

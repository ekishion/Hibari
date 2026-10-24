package baidu

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"path"
	"strconv"

	"github.com/ekishion/jikeqingpan/internal/storage"
)

// ListFetchResult 目录列表拉取结果（含截断信息）
type ListFetchResult struct {
	Body      []byte
	Truncated bool
	Pages     int
	Items     int
}

// FetchFileList 拉取百度网盘文件列表（自动翻页）。下载直链只在服务端按需生成。
func (c *Client) FetchFileList(dir string) (*ListFetchResult, error) {
	if dir == "" {
		dir = "/"
	}
	maxPages := DefaultFileListMaxPages
	if c.cfg != nil && c.cfg.ListMaxPagesCount() > 0 {
		maxPages = c.cfg.ListMaxPagesCount()
	}

	var (
		mergedList []json.RawMessage
		baseResp   map[string]json.RawMessage
		truncated  bool
		pages      int
	)

	for page := 1; page <= maxPages; page++ {
		apiURL := fmt.Sprintf(
			c.baseURL+"/youth/api/list?clienttype=0&app_id=%s&web=1&order=time&desc=1&num=%d&page=%d&dlink=1&dir=%s",
			url.QueryEscape(c.cfg.BaiduAppID),
			FileListPageSize,
			page,
			url.QueryEscape(dir),
		)
		body, err := c.BaiduGet(apiURL, "")
		if err != nil {
			return nil, err
		}

		var resp map[string]json.RawMessage
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("解析文件列表失败: %w", err)
		}
		if page == 1 {
			baseResp = resp
		}
		pages = page

		// 百度业务错误（errno!=0）直接返回原始响应，由上层/缓存逻辑处理。
		if errnoRaw, ok := resp["errno"]; ok {
			var errno int
			if json.Unmarshal(errnoRaw, &errno) == nil && errno != 0 {
				if page == 1 {
					return &ListFetchResult{Body: body, Pages: 1}, nil
				}
				break
			}
		}

		listRaw, ok := resp["list"]
		if !ok {
			if page == 1 {
				return &ListFetchResult{Body: body, Pages: 1}, nil
			}
			break
		}
		var pageList []json.RawMessage
		if err := json.Unmarshal(listRaw, &pageList); err != nil {
			return nil, fmt.Errorf("解析文件列表 list 失败: %w", err)
		}
		mergedList = append(mergedList, pageList...)
		if len(pageList) < FileListPageSize {
			break
		}
		if page == maxPages {
			truncated = true
			log.Printf("[WARN] 目录 %q 达到翻页上限 %d 页（每页 %d），可能仍有未加载文件", dir, maxPages, FileListPageSize)
		}
	}

	if baseResp == nil {
		return nil, fmt.Errorf("文件列表响应为空")
	}
	cleanList, err := json.Marshal(mergedList)
	if err != nil {
		return nil, err
	}
	baseResp["list"] = cleanList
	// 附加服务端元信息，前端可用于提示截断。
	baseResp["truncated"] = json.RawMessage(strconv.FormatBool(truncated))
	baseResp["list_pages"] = json.RawMessage(strconv.Itoa(pages))
	baseResp["list_items"] = json.RawMessage(strconv.Itoa(len(mergedList)))
	baseResp["list_page_limit"] = json.RawMessage(strconv.Itoa(maxPages * FileListPageSize))

	out, err := json.Marshal(baseResp)
	if err != nil {
		return nil, err
	}
	return &ListFetchResult{
		Body:      out,
		Truncated: truncated,
		Pages:     pages,
		Items:     len(mergedList),
	}, nil
}

// StripDownloadLinks 从响应中剔除 dlink 字段，保证直链不暴露给普通列表接口。
func StripDownloadLinks(body []byte) ([]byte, error) {
	var response map[string]json.RawMessage
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}
	listRaw, ok := response["list"]
	if !ok {
		return body, nil
	}
	var list []map[string]json.RawMessage
	if err := json.Unmarshal(listRaw, &list); err != nil {
		return nil, err
	}
	for _, item := range list {
		delete(item, "dlink")
	}
	cleanList, err := json.Marshal(list)
	if err != nil {
		return nil, err
	}
	response["list"] = cleanList
	return json.Marshal(response)
}

// EnsureFileMeta 确保路径对应的文件元数据在缓存中（非目录）。
func (c *Client) EnsureFileMeta(filePath string) (storage.FileMeta, error) {
	meta, ok := c.cache.GetFileMeta(filePath)
	if !ok {
		parentDir := path.Dir(filePath)
		log.Printf("[缓存] 未命中 %q，重新拉取父目录 %q 的文件列表", filePath, parentDir)
		result, err := c.FetchFileList(parentDir)
		if err != nil {
			return storage.FileMeta{}, fmt.Errorf("重新拉取文件列表失败: %w", err)
		}
		c.cache.Update(result.Body)
		meta, ok = c.cache.GetFileMeta(filePath)
	}
	if !ok {
		return storage.FileMeta{}, fmt.Errorf("找不到文件: %s", filePath)
	}
	if meta.IsDir == 1 {
		return storage.FileMeta{}, fmt.Errorf("路径是目录，不能下载: %s", filePath)
	}
	return meta, nil
}

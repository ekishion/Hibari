package server

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/ekishion/jikeqingpan/internal/baidu"
	"github.com/ekishion/jikeqingpan/internal/config"
)

const (
	// multipartFormOverheadBytes 预留 multipart 头与表单字段的开销，
	// MaxBytesReader 的上限 = 单文件上限 + 该开销。
	multipartFormOverheadBytes = 4 * 1024 * 1024
	// unlimitedUploadCapBytes 配置为不限（upload_max_mb=0）时的硬上限，防极端滥用。
	unlimitedUploadCapBytes = int64(8) << 30
)

// handleQuota 返回网盘容量信息（上传弹窗的剩余容量展示）。
func (s *Server) handleQuota(w http.ResponseWriter, r *http.Request) {
	quota, err := s.baiduClient.GetQuota()
	if err != nil {
		log.Printf("[ERROR] 获取网盘容量失败: %v", err)
		writeJSONError(w, http.StatusBadGateway, "quota_failed", "无法获取网盘容量，请稍后重试")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"total": quota.Total,
		"used":  quota.Used,
		"free":  quota.Free,
	})
}

// handleUpload 接收 multipart 上传并转发到百度网盘。
// 网页版上传契约要求 precreate 携带全部分片的 MD5（秒传匹配与最终校验），
// 而分片 uploadid 又来自 precreate，因此必须先完整收下文件体才能发起
// 百度链路：请求体 spool 到临时文件（内存只留一个分片缓冲），随后
// precreate → 逐片 superfile2 → create，结束立即删除临时文件。
// 表单字段约定按序：dir（目标目录）→ size（文件字节数）→ file（文件内容）。
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	limit := s.cfg.UploadMaxBytes()
	maxBytes := limit + multipartFormOverheadBytes
	if limit == 0 {
		maxBytes = unlimitedUploadCapBytes
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	mr, err := r.MultipartReader()
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad_request", "请使用 multipart/form-data 上传")
		return
	}

	var (
		dir      string
		size     int64
		fileName string
		filePart *multipart.Part
	)
stream:
	for {
		part, partErr := mr.NextPart()
		if partErr == io.EOF {
			break
		}
		if partErr != nil {
			if tooLargeError(partErr) {
				writeJSONError(w, http.StatusRequestEntityTooLarge, "upload_too_large", "请求体超出上传大小上限")
				return
			}
			writeJSONError(w, http.StatusBadRequest, "bad_request", "上传内容解析失败，请重试")
			return
		}
		switch part.FormName() {
		case "dir":
			raw, _ := io.ReadAll(io.LimitReader(part, 4096))
			dir = strings.TrimSpace(string(raw))
		case "size":
			raw, _ := io.ReadAll(io.LimitReader(part, 64))
			size, _ = strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
		case "file":
			// multipart 部件必须按序消费：约定 file 在最后，读到即跳出。
			if size <= 0 || strings.TrimSpace(dir) == "" {
				writeJSONError(w, http.StatusBadRequest, "bad_request", "上传表单缺少 dir/size 字段（须位于 file 之前）")
				return
			}
			fileName = sanitizeUploadFileName(part.FileName())
			if fileName == "" {
				writeJSONError(w, http.StatusBadRequest, "bad_request", "文件名无效")
				return
			}
			filePart = part
			break stream
		default:
			_, _ = io.Copy(io.Discard, part)
		}
	}
	if filePart == nil {
		writeJSONError(w, http.StatusBadRequest, "bad_request", "缺少上传文件内容")
		return
	}
	if limit > 0 && size > limit {
		writeJSONError(w, http.StatusRequestEntityTooLarge, "upload_too_large", "文件超出单文件上传上限")
		return
	}

	if !config.IsValidBaiduPath(dir) {
		log.Printf("[WARN] 上传目录的非法路径被拒绝: %q", dir)
		writeJSONError(w, http.StatusBadRequest, "invalid_path", "目标目录无效")
		return
	}
	target := path.Join(dir, fileName)
	if !config.IsValidBaiduPath(target) {
		log.Printf("[WARN] 上传目标的非法路径被拒绝: dir=%q name=%q", dir, fileName)
		writeJSONError(w, http.StatusBadRequest, "invalid_path", "目标路径无效")
		return
	}
	if !s.pathAllowed(target) {
		writeJSONError(w, http.StatusForbidden, "path_not_allowed", "目标目录不在共享范围内")
		return
	}

	// 目录存在性与同名冲突预检：列表缓存未命中时现拉一次父目录。
	if errno := s.ensureDirListed(dir); errno != 0 {
		if errno == -1 {
			writeJSONError(w, http.StatusBadGateway, "baidu_list_failed", "无法确认目标目录，请稍后重试")
		} else {
			writeJSONError(w, http.StatusNotFound, "dir_not_found", "目标目录不存在或不可访问")
		}
		return
	}
	if meta, ok := s.cache.GetFileMeta(target); ok {
		if meta.IsDir == 1 {
			writeJSONError(w, http.StatusConflict, "file_exists", "目标位置已存在同名目录")
		} else {
			writeJSONError(w, http.StatusConflict, "file_exists", "目标位置已存在同名文件")
		}
		return
	}

	// 容量预检：拿不到配额不阻断，create 阶段百度仍会兜底校验。
	if quota, quotaErr := s.baiduClient.GetQuota(); quotaErr == nil && size > quota.Free {
		writeJSONError(w, http.StatusInsufficientStorage, "quota_exceeded", "网盘剩余容量不足")
		return
	}

	tmp, err := os.CreateTemp("", "jkqp-upload-")
	if err != nil {
		log.Printf("[ERROR] 创建上传临时文件失败: %v", err)
		writeJSONError(w, http.StatusInternalServerError, "upload_failed", "服务端接收文件失败，请重试")
		return
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	src := io.Reader(filePart)
	if limit > 0 {
		src = io.LimitReader(filePart, limit+1)
	}
	chunkSize := s.cfg.UploadChunkBytes()
	blockList, written, spoolErr := spoolAndHash(src, tmp, chunkSize)
	if spoolErr != nil {
		log.Printf("[ERROR] 接收上传文件失败: %v", spoolErr)
		writeJSONError(w, http.StatusInternalServerError, "upload_failed", "服务端接收文件失败，请重试")
		return
	}
	if limit > 0 && written > limit {
		writeJSONError(w, http.StatusRequestEntityTooLarge, "upload_too_large", "文件超出单文件上传上限")
		return
	}
	if written != size {
		writeJSONError(w, http.StatusBadRequest, "size_mismatch", "文件实际大小与声明不一致，请重试")
		return
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		log.Printf("[ERROR] 回读上传临时文件失败: %v", err)
		writeJSONError(w, http.StatusInternalServerError, "upload_failed", "服务端处理文件失败，请重试")
		return
	}

	pre, err := s.baiduClient.Precreate(target, size, blockList)
	if err != nil {
		writeUploadBaiduError(w, "precreate", err)
		return
	}
	if pre.ReturnType != 2 {
		buf := make([]byte, chunkSize)
		for partseq := 0; ; partseq++ {
			n, readErr := io.ReadFull(tmp, buf)
			if n > 0 {
				if chunkErr := s.baiduClient.UploadChunk(target, pre.UploadID, partseq, buf[:n]); chunkErr != nil {
					if errors.Is(chunkErr, baidu.ErrIdentityExpired) {
						s.baiduClient.ClearSession()
						chunkErr = s.baiduClient.UploadChunk(target, pre.UploadID, partseq, buf[:n])
					}
					if chunkErr != nil {
						writeUploadBaiduError(w, "分片上传", chunkErr)
						return
					}
				}
			}
			if readErr == io.EOF || readErr == io.ErrUnexpectedEOF {
				break
			}
			if readErr != nil {
				log.Printf("[ERROR] 读取上传临时文件分片失败: %v", readErr)
				writeJSONError(w, http.StatusInternalServerError, "upload_failed", "服务端处理文件失败，请重试")
				return
			}
		}
	}
	if err := s.baiduClient.CreateFile(target, size, pre.UploadID, blockList); err != nil {
		writeUploadBaiduError(w, "create", err)
		return
	}

	// 目录内容已变化：失效该目录缓存，前端刷新后立即可见新文件；
	// 目录列表缓存会随状态落盘，因此标记 dirty。
	s.cache.InvalidateDir(dir)
	s.markStateDirty()
	s.audit(r, "upload_completed", target)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "path": target, "size": written})
}

// ensureDirListed 确保目录列表在缓存中（用于目录存在性与同名预检）。
// 返回 0 表示目录可用；-1 表示百度请求失败；其余为百度 errno。
func (s *Server) ensureDirListed(dir string) int {
	if cached, ok := s.cache.GetDirList(dir); ok {
		return listErrno(cached)
	}
	result, err := s.baiduClient.FetchFileList(dir)
	if err != nil {
		log.Printf("[ERROR] 拉取上传目标目录失败: %v", err)
		return -1
	}
	filtered, err := s.filterFileList(result.Body)
	if err != nil {
		log.Printf("[ERROR] 过滤上传目标目录失败: %v", err)
		return -1
	}
	s.cache.Update(filtered)
	s.cache.SetDirList(dir, filtered)
	s.markStateDirty()
	return listErrno(filtered)
}

// listErrno 从列表响应体提取 errno（0 为成功）。
func listErrno(body []byte) int {
	var resp struct {
		Errno int `json:"errno"`
	}
	if json.Unmarshal(body, &resp) != nil {
		return -1
	}
	return resp.Errno
}

// spoolAndHash 把上传内容写入临时文件，同时按 chunkSize 计算每片 MD5。
func spoolAndHash(src io.Reader, dst *os.File, chunkSize int) (blockList []string, written int64, err error) {
	buf := make([]byte, chunkSize)
	blockList = make([]string, 0, 64)
	for {
		n, readErr := io.ReadFull(src, buf)
		if n > 0 {
			data := buf[:n]
			sum := md5.Sum(data) // #nosec G501 -- 百度上传契约要求的分片校验算法
			blockList = append(blockList, hex.EncodeToString(sum[:]))
			if _, werr := dst.Write(data); werr != nil {
				return nil, written, werr
			}
			written += int64(n)
		}
		if readErr == io.EOF || readErr == io.ErrUnexpectedEOF {
			return blockList, written, nil
		}
		if readErr != nil {
			return nil, written, readErr
		}
	}
}

// sanitizeUploadFileName 剥离路径分隔符与控制字符，得到纯文件名；
// 返回空串表示文件名无效。
func sanitizeUploadFileName(name string) string {
	name = strings.TrimSpace(name)
	if i := strings.LastIndexAny(name, "/\\"); i >= 0 {
		name = name[i+1:]
	}
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, name)
	if name == "" || name == "." || name == ".." {
		return ""
	}
	if utf8.RuneCountInString(name) > 255 {
		return ""
	}
	return name
}

// writeUploadBaiduError 把百度上传链路错误映射为可读的 API 错误响应。
func writeUploadBaiduError(w http.ResponseWriter, stage string, err error) {
	switch {
	case errors.Is(err, baidu.ErrFileExists):
		writeJSONError(w, http.StatusConflict, "file_exists", "目标位置已存在同名文件")
	case errors.Is(err, baidu.ErrQuotaExceeded):
		writeJSONError(w, http.StatusInsufficientStorage, "quota_exceeded", "网盘剩余容量不足")
	case errors.Is(err, baidu.ErrInvalidParam):
		writeJSONError(w, http.StatusBadRequest, "upload_rejected", "文件路径或名称未被网盘接受")
	default:
		log.Printf("[ERROR] 上传失败（%s）: %v", stage, err)
		writeJSONError(w, http.StatusBadGateway, "upload_failed", "上传到网盘失败，请稍后重试")
	}
}

// tooLargeError 识别 MaxBytesReader 超限错误。
func tooLargeError(err error) bool {
	var maxErr *http.MaxBytesError
	return errors.As(err, &maxErr)
}

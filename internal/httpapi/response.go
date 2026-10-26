package httpapi

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/ekishion/hibari/internal/identity"
	"github.com/ekishion/hibari/internal/library"
	"github.com/ekishion/hibari/internal/sharing"
)

// 错误码。收敛到有限集合，细节走 message。
const (
	codeBadRequest  = "bad_request"
	codeUnauth      = "unauthorized"
	codeForbidden   = "forbidden"
	codeNotFound    = "not_found"
	codeConflict    = "conflict"
	codeTooLarge    = "too_large"
	codeRateLimit   = "rate_limited"
	codeUnsupported = "unsupported"
	codeUpstream    = "upstream_failed"
	codeInternal    = "internal"
)

type errorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("[WARN] 写响应失败: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	var body errorBody
	body.Error.Code = code
	body.Error.Message = msg
	writeJSON(w, status, body)
}

// writeDomainError 把领域错误映射为 HTTP 响应。
// 这是全项目唯一做错误翻译的地方，handler 只需返回 error。
func writeDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, library.ErrNotFound), errors.Is(err, sharing.ErrNotFound):
		writeError(w, http.StatusNotFound, codeNotFound, err.Error())
	case errors.Is(err, library.ErrExists):
		writeError(w, http.StatusConflict, codeConflict, err.Error())
	case errors.Is(err, library.ErrInvalid), errors.Is(err, library.ErrNotDir), errors.Is(err, library.ErrNotFile):
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
	case errors.Is(err, library.ErrReadOnly):
		writeError(w, http.StatusNotImplemented, codeUnsupported, err.Error())
	case errors.Is(err, library.ErrQuotaFull):
		writeError(w, http.StatusInsufficientStorage, codeTooLarge, err.Error())
	case errors.Is(err, library.ErrUpstream):
		writeError(w, http.StatusBadGateway, codeUpstream, err.Error())
	case errors.Is(err, sharing.ErrPasswordNeeded), errors.Is(err, sharing.ErrPasswordWrong):
		writeError(w, http.StatusUnauthorized, codeUnauth, err.Error())
	case errors.Is(err, sharing.ErrExhausted):
		writeError(w, http.StatusGone, codeNotFound, err.Error())
	case errors.Is(err, identity.ErrInvalidCredentials), errors.Is(err, identity.ErrDisabled),
		errors.Is(err, identity.ErrSessionExpired):
		writeError(w, http.StatusUnauthorized, codeUnauth, err.Error())
	case errors.Is(err, identity.ErrWeakPassword):
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
	default:
		log.Printf("[ERROR] 未映射的错误: %v", err)
		writeError(w, http.StatusInternalServerError, codeInternal, "服务内部错误")
	}
}

// decodeJSON 解析请求体，限制大小并拒绝未知字段与尾随内容。
func decodeJSON(w http.ResponseWriter, r *http.Request, limit int64, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, "请求内容格式错误")
		return false
	}
	// 拒绝 `{}{}` 这类合法但多余的尾随内容，避免解析歧义。
	if dec.More() {
		writeError(w, http.StatusBadRequest, codeBadRequest, "请求内容格式错误")
		return false
	}
	return true
}

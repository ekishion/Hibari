// 即刻轻盘 API 与网络通信模块：封装 fetch、CSRF 携带、响应错误解析与凭证存储。
(function (root) {
  'use strict';

  function getCookie(name) {
    const value = "; " + document.cookie;
    const parts = value.split("; " + name + "=");
    if (parts.length === 2) return parts.pop().split(";").shift();
    return "";
  }

  function refreshCsrfCookie() {
    return fetch("/healthz", {
      method: "GET",
      cache: "no-store",
      credentials: "same-origin"
    }).catch(function () {
      // 探活静默忽略错误
    });
  }

  function apiFetch(url, options) {
    const opts = Object.assign({}, options);
    opts.headers = Object.assign({}, opts.headers);
    opts.credentials = opts.credentials || "same-origin";

    const method = (opts.method || "GET").toUpperCase();
    if (method !== "GET" && method !== "HEAD") {
      const csrf = getCookie("csrf_token");
      if (csrf) {
        opts.headers["X-CSRF-Token"] = csrf;
      }
    }

    return fetch(url, opts).then(function (resp) {
      if (resp.status === 403) {
        // 尝试自动刷新 CSRF cookie 并重试一次非幂等请求
        return resp.text().then(function (bodyText) {
          if (bodyText.indexOf("csrf_invalid") !== -1) {
            return refreshCsrfCookie().then(function () {
              const retryCsrf = getCookie("csrf_token");
              if (retryCsrf) {
                opts.headers["X-CSRF-Token"] = retryCsrf;
                return fetch(url, opts);
              }
              return resp;
            });
          }
          // 返回包含原 bodyText 的伪 response 供上层处理
          return {
            ok: false,
            status: resp.status,
            text: function () { return Promise.resolve(bodyText); },
            json: function () {
              try { return Promise.resolve(JSON.parse(bodyText)); }
              catch (e) { return Promise.reject(e); }
            }
          };
        });
      }
      return resp;
    });
  }

  function parseAPIError(resp, bodyText) {
    if (bodyText) {
      try {
        const parsed = JSON.parse(bodyText);
        if (parsed && parsed.error && parsed.error.message) {
          return parsed.error.message;
        }
      } catch (e) {
        // 非 JSON 响应
      }
    }
    if (resp.status === 401) return "访问未授权或登录已过期";
    if (resp.status === 403) return "操作被拒绝，无权访问";
    if (resp.status === 404) return "所请求的资源不存在或已失效";
    if (resp.status === 429) return "请求过于频繁，请稍候再试";
    if (resp.status >= 500) return "服务器响应异常，请稍后重试";
    return "请求失败 (" + resp.status + ")";
  }

  function triggerDownloadNavigation(downloadUrl) {
    if (!downloadUrl) return;
    const a = document.createElement("a");
    a.href = downloadUrl;
    a.download = "";
    a.rel = "noopener noreferrer";
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
  }

  function copyText(text) {
    if (navigator.clipboard && navigator.clipboard.writeText) {
      return navigator.clipboard.writeText(text);
    }
    return new Promise(function (resolve, reject) {
      const textarea = document.createElement("textarea");
      textarea.value = text;
      textarea.style.position = "fixed";
      textarea.style.left = "-9999px";
      textarea.style.top = "-9999px";
      document.body.appendChild(textarea);
      textarea.focus();
      textarea.select();
      try {
        const success = document.execCommand("copy");
        document.body.removeChild(textarea);
        if (success) resolve();
        else reject(new Error("浏览器不支持快捷复制"));
      } catch (err) {
        document.body.removeChild(textarea);
        reject(err);
      }
    });
  }

  const SHARE_STORAGE_PREFIX = "jkqp_share_pwd_";

  function getStoredSharePassword(key) {
    if (!key) return "";
    try {
      return window.sessionStorage.getItem(SHARE_STORAGE_PREFIX + key) || "";
    } catch (e) {
      return "";
    }
  }

  function setStoredSharePassword(key, pwd) {
    if (!key) return;
    try {
      if (!pwd) window.sessionStorage.removeItem(SHARE_STORAGE_PREFIX + key);
      else window.sessionStorage.setItem(SHARE_STORAGE_PREFIX + key, pwd);
    } catch (e) {
      // 隐身模式或配额满时静默忽略
    }
  }

  function dirTokenFromLocation() {
    try {
      const params = new URLSearchParams(window.location.search);
      return (params.get("d") || "").trim();
    } catch (e) {
      return "";
    }
  }

  function dirPasswordFromLocation() {
    try {
      const params = new URLSearchParams(window.location.search);
      return (params.get("pwd") || params.get("password") || "").trim();
    } catch (e) {
      return "";
    }
  }

  function shareTokenFromLocation() {
    try {
      const params = new URLSearchParams(window.location.search);
      return (params.get("s") || "").trim();
    } catch (e) {
      return "";
    }
  }

  function sharePasswordFromLocation() {
    try {
      const params = new URLSearchParams(window.location.search);
      return (params.get("pwd") || params.get("password") || "").trim();
    } catch (e) {
      return "";
    }
  }

  // 挂载至全局
  root.getCookie = getCookie;
  root.refreshCsrfCookie = refreshCsrfCookie;
  root.apiFetch = apiFetch;
  root.parseAPIError = parseAPIError;
  root.triggerDownloadNavigation = triggerDownloadNavigation;
  root.copyText = copyText;
  root.getStoredSharePassword = getStoredSharePassword;
  root.setStoredSharePassword = setStoredSharePassword;
  root.dirTokenFromLocation = dirTokenFromLocation;
  root.dirPasswordFromLocation = dirPasswordFromLocation;
  root.shareTokenFromLocation = shareTokenFromLocation;
  root.sharePasswordFromLocation = sharePasswordFromLocation;

})(typeof window !== "undefined" ? window : globalThis);

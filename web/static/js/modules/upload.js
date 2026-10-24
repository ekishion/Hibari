// 即刻轻盘上传模块：上传弹窗、拖拽预填、XHR 进度与剩余容量展示。
// 依赖（运行时按需检查）：apiFetch/parseAPIError（api.js）、showToast/setButtonLoading/
// formatSize/fileIconMeta（components.js）、makeIcon（icons.js）、loadFiles（file_browser.js）。
// 上传走无状态 POST /api/upload：multipart 字段按序 dir → size → file，
// 服务端先完整收下文件体再转发百度，浏览器进度条反映本地→服务器一段。
(function (root) {
  'use strict';

  const STATUS_PENDING = "pending";
  const STATUS_UPLOADING = "uploading";
  const STATUS_SAVING = "saving";
  const STATUS_DONE = "done";
  const STATUS_FAILED = "failed";

  const STATUS_TEXT = {
    pending: "排队中",
    saving: "保存中…",
    done: "已完成"
  };

  let overlay = null;
  let lastFocused = null;
  let uploading = false;
  let currentXHR = null;
  const queue = [];

  function $(id) {
    return document.getElementById(id);
  }

  // 上传入口是否可用：按钮可见（updateAuthUI 已按登录态/开关/沙盒判定）。
  function uploadAvailable() {
    const btn = $("btn-upload");
    return !!(root.uiConfig && root.uiConfig.uploadEnabled && btn && !btn.hidden);
  }

  function maxUploadBytes() {
    const mb = root.uiConfig && typeof root.uiConfig.uploadMaxMB === "number" ? root.uiConfig.uploadMaxMB : 0;
    return mb > 0 ? mb * 1024 * 1024 : 0;
  }

  function getCookie(name) {
    const prefix = name + "=";
    const parts = document.cookie.split(";");
    for (let i = 0; i < parts.length; i++) {
      const part = parts[i].trim();
      if (part.indexOf(prefix) === 0) {
        return decodeURIComponent(part.slice(prefix.length));
      }
    }
    return "";
  }

  function refreshCsrfCookie() {
    try {
      return fetch("/healthz", { credentials: "same-origin" });
    } catch (e) {
      return Promise.resolve();
    }
  }

  // ===== 配额 =====
  function loadQuota() {
    const box = $("upload-quota");
    const text = $("upload-quota-text");
    const fill = $("upload-quota-fill");
    if (!box || !text || !fill) return;
    apiFetch("/api/quota", { method: "GET" })
      .then(function (resp) {
        return resp.text().then(function (body) {
          if (!resp.ok) throw new Error(parseAPIError(resp, body));
          return JSON.parse(body);
        });
      })
      .then(function (data) {
        const total = Number(data.total) || 0;
        const used = Number(data.used) || 0;
        const free = Number(data.free) || 0;
        if (total <= 0) {
          text.textContent = "容量信息不可用";
          fill.style.width = "0%";
          return;
        }
        const percent = Math.min(100, Math.round((used / total) * 100));
        text.textContent = "已用 " + formatSize(used) + " / 共 " + formatSize(total) + " · 剩余 " + formatSize(free);
        fill.style.width = percent + "%";
        fill.classList.toggle("is-warning", percent >= 90);
        box.dataset.free = String(free);
      })
      .catch(function (err) {
        text.textContent = "容量获取失败：" + err.message;
        fill.style.width = "0%";
      });
  }

  // ===== 文件清单 =====
  function clearQueue() {
    queue.length = 0;
    const list = $("upload-file-list");
    if (list) list.replaceChildren();
    syncSummary();
  }

  function addFiles(fileList) {
    const list = $("upload-file-list");
    if (!list || !fileList || !fileList.length) return;
    const cap = maxUploadBytes();
    const quotaBox = $("upload-quota");
    const freeBytes = quotaBox ? Number(quotaBox.dataset.free) || 0 : 0;

    for (let i = 0; i < fileList.length; i++) {
      const file = fileList[i];
      const item = {
        file: file,
        name: file.name,
        size: file.size,
        status: STATUS_PENDING,
        message: "",
        xhr: null
      };
      if (file.size <= 0) {
        item.status = STATUS_FAILED;
        item.message = "不支持空文件";
      } else if (cap > 0 && file.size > cap) {
        item.status = STATUS_FAILED;
        item.message = "超出单文件上限";
      } else if (freeBytes > 0 && file.size > freeBytes) {
        item.status = STATUS_FAILED;
        item.message = "超出网盘剩余容量";
      }
      queue.push(item);
      list.appendChild(renderItem(item));
    }
    syncSummary();
  }

  function renderItem(item) {
    const li = document.createElement("li");
    li.className = "upload-item";
    item.el = li;

    const icon = document.createElement("span");
    icon.className = "upload-item-icon file-icon";
    const meta = typeof fileIconMeta === "function" ? fileIconMeta(item.name, false) : null;
    icon.appendChild(makeIcon(meta && meta.name ? meta.name : "file"));

    const main = document.createElement("div");
    main.className = "upload-item-main";

    const row = document.createElement("div");
    row.className = "upload-item-row";
    const name = document.createElement("span");
    name.className = "upload-item-name";
    name.textContent = item.name;
    name.title = item.name;
    const size = document.createElement("span");
    size.className = "upload-item-size";
    size.textContent = formatSize(item.size);
    row.appendChild(name);
    row.appendChild(size);

    const progress = document.createElement("div");
    progress.className = "upload-progress";
    const fill = document.createElement("div");
    fill.className = "upload-progress-fill";
    progress.appendChild(fill);
    item.fillEl = fill;

    const status = document.createElement("div");
    status.className = "upload-item-status";
    status.textContent = item.message || STATUS_TEXT[item.status] || "";
    item.statusEl = status;

    main.appendChild(row);
    main.appendChild(progress);
    main.appendChild(status);

    const remove = document.createElement("button");
    remove.type = "button";
    remove.className = "upload-item-remove";
    remove.setAttribute("aria-label", "移除 " + item.name);
    remove.appendChild(makeIcon("trash-2"));
    remove.addEventListener("click", function () {
      if (item.status === STATUS_UPLOADING || item.status === STATUS_SAVING) return;
      const idx = queue.indexOf(item);
      if (idx >= 0) queue.splice(idx, 1);
      li.remove();
      syncSummary();
    });

    li.appendChild(icon);
    li.appendChild(main);
    li.appendChild(remove);
    setItemState(item, item.status, item.message);
    return li;
  }

  function setItemState(item, status, message) {
    item.status = status;
    if (message !== undefined) item.message = message;
    if (!item.el) return;
    item.el.classList.toggle("is-done", status === STATUS_DONE);
    item.el.classList.toggle("is-failed", status === STATUS_FAILED);
    if (item.statusEl) {
      if (status === STATUS_UPLOADING) {
        item.statusEl.textContent = "上传中 " + Math.round(item.progress || 0) + "%";
      } else {
        item.statusEl.textContent = item.message || STATUS_TEXT[status] || "";
      }
    }
    if (status === STATUS_DONE && item.fillEl) item.fillEl.style.width = "100%";
    if (status === STATUS_FAILED && item.fillEl) item.fillEl.classList.add("is-failed");
  }

  function syncSummary() {
    const summary = $("upload-summary");
    const startBtn = $("btn-upload-start");
    if (summary) {
      const pending = queue.filter(function (it) { return it.status === STATUS_PENDING; }).length;
      const failed = queue.filter(function (it) { return it.status === STATUS_FAILED; }).length;
      const done = queue.filter(function (it) { return it.status === STATUS_DONE; }).length;
      if (!queue.length) {
        summary.textContent = "";
      } else {
        const bits = [];
        if (pending) bits.push("待上传 " + pending);
        if (done) bits.push("已完成 " + done);
        if (failed) bits.push("失败 " + failed);
        summary.textContent = bits.join(" · ");
      }
    }
    if (startBtn) {
      const hasPending = queue.some(function (it) { return it.status === STATUS_PENDING; });
      startBtn.disabled = uploading || !hasPending;
    }
  }

  // ===== 上传执行 =====
  function uploadOne(item) {
    return new Promise(function (resolve) {
      setItemState(item, STATUS_UPLOADING);
      const dir = root.currentDir || "/";
      const form = new FormData();
      form.append("dir", dir);
      form.append("size", String(item.file.size));
      form.append("file", item.file, item.file.name);

      const xhr = new XMLHttpRequest();
      item.xhr = xhr;
      currentXHR = xhr;
      let cancelled = false;
      let retried = false;

      const finish = function () {
        item.xhr = null;
        currentXHR = null;
        resolve();
      };

      const send = function () {
        xhr.open("POST", "/api/upload");
        xhr.setRequestHeader("X-CSRF-Token", getCookie("csrf_token"));
        xhr.send(form);
      };

      xhr.upload.onprogress = function (e) {
        if (!e.lengthComputable) return;
        const percent = (e.loaded / e.total) * 100;
        item.progress = percent;
        if (item.fillEl) item.fillEl.style.width = Math.min(100, percent) + "%";
        if (percent >= 100) {
          setItemState(item, STATUS_SAVING);
        } else {
          setItemState(item, STATUS_UPLOADING);
        }
      };

      xhr.onload = function () {
        const text = xhr.responseText || "";
        let data = null;
        try { data = text ? JSON.parse(text) : null; } catch (e) { /* 非 JSON */ }
        if (xhr.status >= 200 && xhr.status < 300) {
          setItemState(item, STATUS_DONE);
          loadQuota();
          finish();
          return;
        }
        const message = data && data.error && data.error.message ? data.error.message
          : "上传失败 (" + xhr.status + ")";
        if (!retried && xhr.status === 403 && data && data.error && data.error.code === "csrf_invalid") {
          // CSRF Cookie 过期：重新获取后重试一次（与 apiFetch 的重试策略一致）
          retried = true;
          setItemState(item, STATUS_UPLOADING);
          refreshCsrfCookie().then(function () {
            setTimeout(send, 50);
          });
          return;
        }
        setItemState(item, STATUS_FAILED, message);
        finish();
      };

      xhr.onerror = function () {
        setItemState(item, STATUS_FAILED, cancelled ? "已取消" : "网络错误，上传中断");
        finish();
      };
      xhr.onabort = function () {
        setItemState(item, STATUS_FAILED, "已取消");
        finish();
      };

      send();
    });
  }

  function startUpload() {
    if (uploading) return;
    const pending = queue.filter(function (it) { return it.status === STATUS_PENDING; });
    if (!pending.length) return;
    uploading = true;
    syncSummary();
    const startBtn = $("btn-upload-start");
    setButtonLoading(startBtn, true, "上传中…");

    let index = 0;
    let succeeded = 0;
    let failed = 0;
    const next = function () {
      if (index >= pending.length) {
        uploading = false;
        setButtonLoading(startBtn, false);
        syncSummary();
        if (succeeded > 0 && typeof loadFiles === "function") {
          loadFiles(root.currentDir || "/", { refresh: true });
        }
        if (succeeded > 0 && failed === 0) {
          showToast("上传完成，共 " + succeeded + " 个文件", "success");
        } else if (failed > 0) {
          showToast("上传结束：成功 " + succeeded + " 个，失败 " + failed + " 个", failed && succeeded ? "warning" : "error");
        }
        return;
      }
      const item = pending[index++];
      uploadOne(item).then(function () {
        if (item.status === STATUS_DONE) succeeded++;
        else failed++;
        next();
      });
    };
    next();
  }

  // ===== 弹窗生命周期 =====
  function syncDropzoneHint() {
    const hint = $("upload-dropzone-hint");
    if (!hint) return;
    const mb = root.uiConfig && typeof root.uiConfig.uploadMaxMB === "number" ? root.uiConfig.uploadMaxMB : 0;
    hint.textContent = mb > 0
      ? "单文件上限 " + (mb >= 1024 ? (mb / 1024) + " GB" : mb + " MB")
      : "不限单文件大小";
  }

  function openUploadModal() {
    if (!overlay) return;
    if (!uploadAvailable()) return;
    lastFocused = document.activeElement;
    overlay.hidden = false;
    overlay.setAttribute("aria-hidden", "false");
    const dirEl = $("upload-target-dir");
    if (dirEl) {
      const dir = root.currentDir || "/";
      dirEl.textContent = "目标目录：" + dir;
      dirEl.title = dir;
    }
    clearQueue();
    syncDropzoneHint();
    loadQuota();
    const dropzone = $("upload-dropzone");
    if (dropzone) dropzone.focus();
  }

  function closeUploadModal() {
    if (!overlay) return;
    if (uploading && currentXHR) {
      currentXHR.abort();
      showToast("已取消上传", "info");
    }
    uploading = false;
    currentXHR = null;
    overlay.hidden = true;
    overlay.setAttribute("aria-hidden", "true");
    if (lastFocused && typeof lastFocused.focus === "function") {
      lastFocused.focus();
    }
    lastFocused = null;
  }

  // ===== 全局拖拽 =====
  function bindGlobalDrop() {
    let dragDepth = 0;
    document.addEventListener("dragenter", function (e) {
      if (!uploadAvailable() || !hasFiles(e)) return;
      dragDepth++;
      const listEl = $("file-list");
      if (listEl) listEl.classList.add("is-drop-target");
    });
    document.addEventListener("dragover", function (e) {
      if (!uploadAvailable() || !hasFiles(e)) return;
      e.preventDefault();
    });
    document.addEventListener("dragleave", function (e) {
      if (!uploadAvailable() || !hasFiles(e)) return;
      dragDepth = Math.max(0, dragDepth - 1);
      if (dragDepth === 0) clearDropTarget();
    });
    document.addEventListener("drop", function (e) {
      dragDepth = 0;
      clearDropTarget();
      if (!uploadAvailable() || !hasFiles(e)) return;
      e.preventDefault();
      const files = e.dataTransfer && e.dataTransfer.files;
      if (!files || !files.length) return;
      // 单文件分享落地页不提供上传（app-main 已隐藏）
      const appMain = $("app-main");
      if (!appMain || appMain.hidden) return;
      // 弹窗已打开且事件源自弹窗内部时，交给弹窗拖放区处理，避免重复清空清单
      if (overlay && !overlay.hidden && overlay.contains(e.target)) return;
      openUploadModal();
      addFiles(files);
    });
  }

  function hasFiles(e) {
    const types = e.dataTransfer && e.dataTransfer.types;
    if (!types) return false;
    for (let i = 0; i < types.length; i++) {
      if (types[i] === "Files") return true;
    }
    return false;
  }

  function clearDropTarget() {
    const listEl = $("file-list");
    if (listEl) listEl.classList.remove("is-drop-target");
  }

  // ===== 绑定 =====
  function bindUploadModal() {
    overlay = $("upload-overlay");
    if (!overlay || overlay.dataset.bound === "1") return;
    overlay.dataset.bound = "1";

    const closeBtn = $("upload-close");
    if (closeBtn) closeBtn.addEventListener("click", closeUploadModal);
    overlay.addEventListener("click", function (e) {
      if (e.target === overlay) closeUploadModal();
    });
    document.addEventListener("keydown", function (e) {
      if (e.key === "Escape" && !overlay.hidden) closeUploadModal();
    });

    const dropzone = $("upload-dropzone");
    const input = $("upload-file-input");
    if (dropzone && input) {
      dropzone.addEventListener("click", function () { input.click(); });
      dropzone.addEventListener("keydown", function (e) {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          input.click();
        }
      });
      ["dragenter", "dragover"].forEach(function (type) {
        dropzone.addEventListener(type, function (e) {
          e.preventDefault();
          dropzone.classList.add("is-dragover");
        });
      });
      ["dragleave", "drop"].forEach(function (type) {
        dropzone.addEventListener(type, function () {
          dropzone.classList.remove("is-dragover");
        });
      });
      dropzone.addEventListener("drop", function (e) {
        e.preventDefault();
        const files = e.dataTransfer && e.dataTransfer.files;
        if (files && files.length) addFiles(files);
      });
      input.addEventListener("change", function () {
        if (input.files && input.files.length) {
          addFiles(input.files);
          input.value = "";
        }
      });
    }

    const startBtn = $("btn-upload-start");
    if (startBtn) startBtn.addEventListener("click", startUpload);

    bindGlobalDrop();
  }

  // 挂载至全局
  root.openUploadModal = openUploadModal;
  root.closeUploadModal = closeUploadModal;
  root.bindUploadModal = bindUploadModal;

})(typeof window !== "undefined" ? window : globalThis);

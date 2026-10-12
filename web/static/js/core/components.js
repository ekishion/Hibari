// 即刻轻盘 UI 组件与视觉反馈模块：Toast 提示、自定义单选下拉、焦点圈定与文件元数据格式化。
(function (root) {
  'use strict';

  // ===== 消息通知 (Toast) =====
  let toastTimer = null;

  function showToast(msg, type) {
    let toast = document.getElementById("toast");
    if (!toast) {
      toast = document.createElement("div");
      toast.id = "toast";
      toast.className = "toast";
      toast.setAttribute("role", "status");
      toast.setAttribute("aria-live", "polite");
      document.body.appendChild(toast);
    }

    if (toastTimer) {
      clearTimeout(toastTimer);
      toastTimer = null;
    }

    toast.className = "toast toast-" + (type || "info");
    const iconName = type === "error" ? "circle-alert"
      : type === "warning" ? "triangle-alert"
        : type === "success" ? "circle-check"
          : "info";

    toast.replaceChildren();
    if (typeof makeIcon === "function") {
      const iconWrap = document.createElement("span");
      iconWrap.className = "toast-icon";
      iconWrap.appendChild(makeIcon(iconName));
      toast.appendChild(iconWrap);
    }
    const textSpan = document.createElement("span");
    textSpan.className = "toast-text";
    textSpan.textContent = msg;
    toast.appendChild(textSpan);

    toast.classList.add("is-visible");
    toastTimer = setTimeout(function () {
      toast.classList.remove("is-visible");
    }, 2800);
  }

  // ===== 按钮加载态管理 =====
  function setButtonLoading(btn, loading, loadingText) {
    if (!btn) return;
    if (loading) {
      if (!btn.dataset.originalHtml) {
        btn.dataset.originalHtml = btn.innerHTML;
      }
      btn.disabled = true;
      btn.classList.add("is-loading");
      btn.replaceChildren();

      const spinner = document.createElement("span");
      spinner.className = "btn-spinner";
      if (typeof makeIcon === "function") {
        spinner.appendChild(makeIcon("loader"));
      }
      btn.appendChild(spinner);

      const label = document.createElement("span");
      label.className = "btn-label";
      label.textContent = loadingText || "正在处理…";
      btn.appendChild(label);
    } else {
      btn.disabled = false;
      btn.classList.remove("is-loading");
      if (btn.dataset.originalHtml) {
        btn.innerHTML = btn.dataset.originalHtml;
        delete btn.dataset.originalHtml;
        if (typeof mountIcons === "function") {
          mountIcons(btn);
        }
      }
    }
  }

  // ===== 无障碍 Focus Trap 圈定 =====
  function trapFocusWithin(event, container) {
    if (event.key !== "Tab" || !container) return;
    const focusable = container.querySelectorAll(
      'button:not([disabled]):not([hidden]), [href]:not([disabled]):not([hidden]), input:not([disabled]):not([hidden]), select:not([disabled]):not([hidden]), textarea:not([disabled]):not([hidden]), [tabindex]:not([tabindex="-1"]):not([hidden])'
    );
    if (!focusable.length) {
      event.preventDefault();
      return;
    }
    const first = focusable[0];
    const last = focusable[focusable.length - 1];

    if (event.shiftKey) {
      if (document.activeElement === first) {
        last.focus();
        event.preventDefault();
      }
    } else {
      if (document.activeElement === last) {
        first.focus();
        event.preventDefault();
      }
    }
  }

  // ===== 格式化工具 =====
  function formatSize(bytes) {
    const n = Number(bytes);
    if (!Number.isFinite(n) || n <= 0) return "-";
    const units = ["B", "KB", "MB", "GB", "TB"];
    let v = n;
    let i = 0;
    while (v >= 1024 && i < units.length - 1) {
      v /= 1024;
      i++;
    }
    return (i === 0 ? v : v.toFixed(1)) + " " + units[i];
  }

  function formatTime(ts) {
    const n = Number(ts);
    if (!Number.isFinite(n) || n <= 0) return "-";
    const d = new Date(n * 1000);
    const pad = function (v) { return v < 10 ? "0" + v : "" + v; };
    return d.getFullYear() + "-" +
      pad(d.getMonth() + 1) + "-" +
      pad(d.getDate()) + " " +
      pad(d.getHours()) + ":" +
      pad(d.getMinutes());
  }

  const EXT_ICONS = {
    jpg: "image", jpeg: "image", png: "image", gif: "image", webp: "image", svg: "image", bmp: "image", ico: "image",
    mp4: "film", mkv: "film", avi: "film", mov: "film", webm: "film", flv: "film", m4v: "film",
    mp3: "music", flac: "music", wav: "music", aac: "music", ogg: "music", m4a: "music",
    txt: "file-text", md: "file-text", markdown: "file-text", log: "file-text", pdf: "file-text",
    doc: "file-text", docx: "file-text",
    xls: "file-spreadsheet", xlsx: "file-spreadsheet", csv: "file-spreadsheet",
    zip: "archive", rar: "archive", "7z": "archive", tar: "archive", gz: "archive", bz2: "archive",
    js: "file-code", ts: "file-code", html: "file-code", css: "file-code", json: "file-code",
    go: "file-code", py: "file-code", java: "file-code", c: "file-code", cpp: "file-code",
    rs: "file-code", sh: "file-code", sql: "file-code", yaml: "file-code", yml: "file-code",
    apk: "smartphone", ipa: "smartphone", exe: "package", dmg: "package"
  };

  function fileIconMeta(name, isDir) {
    if (isDir) return { name: "folder", category: "folder" };
    const ext = (name || "").split(".").pop().toLowerCase();
    const icon = EXT_ICONS[ext] || "file";
    return { name: icon, category: icon };
  }

  const PREVIEW_IMAGE_EXTS = ["jpg", "jpeg", "png", "gif", "webp", "bmp", "svg", "ico"];

  function isPreviewableImage(name) {
    const ext = (name || "").split(".").pop().toLowerCase();
    return PREVIEW_IMAGE_EXTS.indexOf(ext) !== -1;
  }

  // ===== 自绘下拉组件 (Custom Select) =====
  function setCustomSelectValue(wrapEl, value) {
    if (!wrapEl) return;
    const input = wrapEl.querySelector('input[type="hidden"]');
    const labelEl = wrapEl.querySelector(".custom-select-label");
    const options = wrapEl.querySelectorAll(".custom-select-option");

    if (input) input.value = value;
    let found = false;
    for (let i = 0; i < options.length; i++) {
      const opt = options[i];
      const match = String(opt.getAttribute("data-value")) === String(value);
      opt.classList.toggle("is-active", match);
      opt.setAttribute("aria-selected", match ? "true" : "false");
      if (match) {
        if (labelEl) labelEl.textContent = opt.textContent.trim();
        found = true;
      }
    }
    if (!found && options.length > 0) {
      options[0].classList.add("is-active");
      options[0].setAttribute("aria-selected", "true");
      if (labelEl) labelEl.textContent = options[0].textContent.trim();
      if (input) input.value = options[0].getAttribute("data-value");
    }
  }

  function closeAllCustomSelects(except) {
    const all = document.querySelectorAll(".custom-select-wrap");
    for (let i = 0; i < all.length; i++) {
      if (all[i] !== except) {
        all[i].classList.remove("is-open");
        const trigger = all[i].querySelector(".custom-select-trigger");
        const menu = all[i].querySelector(".custom-select-menu");
        if (trigger) trigger.setAttribute("aria-expanded", "false");
        if (menu) menu.hidden = true;
      }
    }
  }

  function bindCustomSelect(wrapEl) {
    if (!wrapEl || wrapEl.dataset.customSelectBound === "1") return;
    wrapEl.dataset.customSelectBound = "1";

    const trigger = wrapEl.querySelector(".custom-select-trigger");
    const menu = wrapEl.querySelector(".custom-select-menu");
    const options = wrapEl.querySelectorAll(".custom-select-option");

    function openMenu() {
      closeAllCustomSelects(wrapEl);
      wrapEl.classList.add("is-open");
      if (trigger) trigger.setAttribute("aria-expanded", "true");
      if (menu) {
        menu.hidden = false;
        const active = menu.querySelector(".custom-select-option.is-active") || options[0];
        if (active) active.focus();
      }
    }

    function closeMenu() {
      wrapEl.classList.remove("is-open");
      if (trigger) {
        trigger.setAttribute("aria-expanded", "false");
        trigger.focus();
      }
      if (menu) menu.hidden = true;
    }

    if (trigger) {
      trigger.addEventListener("click", function (e) {
        e.preventDefault();
        e.stopPropagation();
        if (wrapEl.classList.contains("is-open")) closeMenu();
        else openMenu();
      });

      trigger.addEventListener("keydown", function (e) {
        if (e.key === "ArrowDown" || e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          openMenu();
        }
      });
    }

    for (let i = 0; i < options.length; i++) {
      (function (opt, idx) {
        opt.addEventListener("click", function (e) {
          e.preventDefault();
          e.stopPropagation();
          const val = opt.getAttribute("data-value");
          setCustomSelectValue(wrapEl, val);
          closeMenu();
        });

        opt.addEventListener("keydown", function (e) {
          if (e.key === "ArrowDown") {
            e.preventDefault();
            const next = options[(idx + 1) % options.length];
            if (next) next.focus();
          } else if (e.key === "ArrowUp") {
            e.preventDefault();
            const prev = options[(idx - 1 + options.length) % options.length];
            if (prev) prev.focus();
          } else if (e.key === "Enter" || e.key === " ") {
            e.preventDefault();
            const val = opt.getAttribute("data-value");
            setCustomSelectValue(wrapEl, val);
            closeMenu();
          } else if (e.key === "Escape" || e.key === "Tab") {
            closeMenu();
          }
        });
      })(options[i], i);
    }
  }

  // 点击外部收起自绘下拉
  document.addEventListener("click", function (e) {
    if (!e.target.closest(".custom-select-wrap")) {
      closeAllCustomSelects();
    }
  });

  // 挂载至全局
  root.showToast = showToast;
  root.setButtonLoading = setButtonLoading;
  root.trapFocusWithin = trapFocusWithin;
  root.formatSize = formatSize;
  root.formatTime = formatTime;
  root.fileIconMeta = fileIconMeta;
  root.isPreviewableImage = isPreviewableImage;
  root.setCustomSelectValue = setCustomSelectValue;
  root.closeAllCustomSelects = closeAllCustomSelects;
  root.bindCustomSelect = bindCustomSelect;

})(typeof window !== "undefined" ? window : globalThis);

// 即刻轻盘分享模态框模块：创建短链、提取码生成、二维码展示与提取码解锁弹窗。
(function (root) {
  'use strict';

  let currentShareTarget = null;
  let lastFocusedBeforeShare = null;
  let currentUnlockContext = null;
  let lastFocusedBeforeUnlock = null;

  // 6 位大小写不敏感（数字 + 英文字母）随机生成器
  function generateRandomSharePin(len) {
    const chars = "23456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz";
    const n = len || 6;
    let res = "";
    if (window.crypto && window.crypto.getRandomValues) {
      const buf = new Uint8Array(n);
      window.crypto.getRandomValues(buf);
      for (let i = 0; i < n; i++) {
        res += chars[buf[i] % chars.length];
      }
    } else {
      for (let i = 0; i < n; i++) {
        res += chars[Math.floor(Math.random() * chars.length)];
      }
    }
    return res;
  }

  // ===== 打开分享弹窗 =====
  function openShareModal(target) {
    currentShareTarget = target;
    const overlay = document.getElementById("share-overlay");
    if (!overlay) return;

    lastFocusedBeforeShare = document.activeElement;
    overlay.hidden = false;
    overlay.setAttribute("aria-hidden", "false");

    const isDir = target && (target.type === "dir" || target.isDir);
    const titleEl = document.getElementById("share-dialog-title");
    const nameEl = document.getElementById("share-target-name");
    const subEl = document.getElementById("share-target-sub");
    const iconEl = document.getElementById("share-target-icon");

    if (titleEl) titleEl.textContent = isDir ? "分享文件夹" : "分享文件";
    if (nameEl) nameEl.textContent = target ? (target.name || target.path) : "未命名";
    if (subEl) subEl.textContent = isDir ? "文件夹 · 目录分享" : "单个文件 · 直接外链";

    if (iconEl && typeof makeIcon === "function") {
      iconEl.replaceChildren();
      const meta = typeof fileIconMeta === "function" ? fileIconMeta(target ? target.name : "", isDir) : { name: isDir ? "folder" : "file" };
      iconEl.appendChild(makeIcon(meta.name));
    }

    // 目录分享与文件分享的选项适配：目录短链无最大下载次数限制
    const maxUsesRow = document.getElementById("share-max-uses-row");
    if (maxUsesRow) {
      maxUsesRow.hidden = !!isDir;
    }

    // 重置面板展示
    const formPanel = document.getElementById("share-form-panel");
    const resultPanel = document.getElementById("share-result-panel");
    if (formPanel) formPanel.hidden = false;
    if (resultPanel) resultPanel.hidden = true;

    // 重置提取码开关与输入框
    const pwdToggle = document.getElementById("share-pwd-toggle");
    const pwdBox = document.getElementById("share-pwd-box");
    const pwdInput = document.getElementById("share-pwd-input");
    if (pwdToggle) pwdToggle.checked = true;
    if (pwdBox) pwdBox.hidden = false;
    if (pwdInput) {
      pwdInput.value = generateRandomSharePin(6);
    }

    // 重置自绘下拉菜单为默认值
    const expSelect = document.getElementById("wrap-share-expire");
    if (expSelect && typeof setCustomSelectValue === "function") {
      setCustomSelectValue(expSelect, "604800"); // 默认 7 天
    }
    const usesSelect = document.getElementById("wrap-share-uses");
    if (usesSelect && typeof setCustomSelectValue === "function") {
      setCustomSelectValue(usesSelect, "0"); // 默认不限
    }

    const btnSubmit = document.getElementById("btn-create-share");
    if (btnSubmit && typeof setButtonLoading === "function") {
      setButtonLoading(btnSubmit, false);
    }

    if (pwdInput) pwdInput.focus();
  }

  function closeShareModal() {
    const overlay = document.getElementById("share-overlay");
    if (!overlay) return;
    overlay.hidden = true;
    overlay.setAttribute("aria-hidden", "true");
    currentShareTarget = null;
    if (lastFocusedBeforeShare && typeof lastFocusedBeforeShare.focus === "function") {
      lastFocusedBeforeShare.focus();
      lastFocusedBeforeShare = null;
    }
  }

  // ===== 创建分享逻辑 =====
  function handleCreateShare() {
    if (!currentShareTarget) return;
    const btnSubmit = document.getElementById("btn-create-share");
    if (typeof setButtonLoading === "function") {
      setButtonLoading(btnSubmit, true, "正在生成…");
    }

    const pwdToggle = document.getElementById("share-pwd-toggle");
    const pwdInput = document.getElementById("share-pwd-input");
    const expInput = document.getElementById("share-expire-select");
    const usesInput = document.getElementById("share-uses-select");

    const hasPwd = pwdToggle ? pwdToggle.checked : true;
    const password = (hasPwd && pwdInput) ? pwdInput.value.trim() : "";
    const expireSeconds = expInput ? parseInt(expInput.value, 10) : 604800;
    const maxUsesVal = usesInput ? parseInt(usesInput.value, 10) : 0;

    const isDir = currentShareTarget.type === "dir" || currentShareTarget.isDir;
    const endpoint = isDir ? "/api/dir-link" : "/api/download";

    const payload = isDir ? {
      dir: currentShareTarget.path,
      password: password,
      expire_seconds: expireSeconds
    } : {
      path: currentShareTarget.path,
      password: password,
      expire_seconds: expireSeconds,
      max_uses: maxUsesVal
    };

    apiFetch(endpoint, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload)
    })
      .then(function (resp) {
        if (!resp.ok) {
          return resp.text().then(function (text) {
            throw new Error(parseAPIError(resp, text));
          });
        }
        return resp.json();
      })
      .then(function (data) {
        if (typeof setButtonLoading === "function") {
          setButtonLoading(btnSubmit, false);
        }
        renderShareResult(data, password, isDir);
      })
      .catch(function (err) {
        if (typeof setButtonLoading === "function") {
          setButtonLoading(btnSubmit, false);
        }
        showToast("创建分享失败：" + err.message, "error");
      });
  }

  // ===== 呈现创建成功视图 (短链、文案、二维码) =====
  function renderShareResult(data, password, isDir) {
    const formPanel = document.getElementById("share-form-panel");
    const resultPanel = document.getElementById("share-result-panel");
    if (formPanel) formPanel.hidden = true;
    if (resultPanel) resultPanel.hidden = false;

    // 格式化完整外链 URL (单个文件为 /d/{token}，目录为 /?d={token})
    const relUrl = data.url || (isDir ? "/?d=" + data.token : "/d/" + data.token);
    const fullUrl = window.location.origin + relUrl;

    const urlInput = document.getElementById("share-result-url");
    if (urlInput) urlInput.value = fullUrl;

    const pwdDisplay = document.getElementById("share-result-pwd");
    const pwdWrap = document.getElementById("share-result-pwd-wrap");
    if (password) {
      if (pwdDisplay) {
        if ("value" in pwdDisplay) {
          pwdDisplay.value = password;
        } else {
          pwdDisplay.textContent = password;
        }
      }
      if (pwdWrap) pwdWrap.hidden = false;
    } else {
      if (pwdWrap) pwdWrap.hidden = true;
    }

    // 渲染矢量 SVG 二维码
    const qrContainer = document.getElementById("share-qr-container");
    if (qrContainer) {
      qrContainer.replaceChildren();
      if (root.QRCode && typeof root.QRCode.renderTo === "function") {
        root.QRCode.renderTo(qrContainer, fullUrl, {
          size: 144,
          margin: 1,
          // 二维码模块固定深色：容器底色是写死的白色，跟随主题的
          // currentColor 会在暗色模式下变成白模块白底而无法扫描
          fgColor: "#000000",
          bgColor: "transparent"
        });
      }
    }

    // 绑定“复制链接”
    const btnCopyUrl = document.getElementById("btn-copy-share-url");
    if (btnCopyUrl) {
      btnCopyUrl.onclick = function () {
        copyText(fullUrl).then(function () {
          showToast("分享链接已复制到剪贴板", "success");
        }).catch(function () {
          showToast("复制失败，请手动长按复制", "error");
        });
      };
    }

    // 绑定“复制提取码”
    const btnCopyPwd = document.getElementById("btn-copy-share-pwd");
    if (btnCopyPwd) {
      btnCopyPwd.onclick = function () {
        copyText(password).then(function () {
          showToast("提取码已复制", "success");
        }).catch(function () {
          showToast("复制失败", "error");
        });
      };
    }

    // 绑定“复制完整口令”
    const btnCopyFull = document.getElementById("btn-copy-share-full");
    if (btnCopyFull) {
      btnCopyFull.onclick = function () {
        let copyStr = "【即刻轻盘分享】\n名称：" + (currentShareTarget ? (currentShareTarget.name || currentShareTarget.path) : "网盘资源") + "\n链接：" + fullUrl;
        if (password) {
          copyStr += "\n提取码：" + password;
        }
        copyText(copyStr).then(function () {
          showToast("完整分享口令已复制，可直接发送", "success");
        }).catch(function () {
          showToast("复制失败，请手动长按复制", "error");
        });
      };
    }
  }

  // ===== 提取码受保护解锁弹窗 (Unlock Modal) =====
  function openUnlockModal(context) {
    currentUnlockContext = context;
    const overlay = document.getElementById("unlock-overlay");
    if (!overlay) return;

    lastFocusedBeforeUnlock = document.activeElement;
    overlay.hidden = false;
    overlay.setAttribute("aria-hidden", "false");

    const input = document.getElementById("unlock-pwd-input");
    const errEl = document.getElementById("unlock-error");
    if (input) {
      input.value = "";
      input.classList.remove("is-shake");
      input.focus();
    }
    if (errEl) {
      errEl.hidden = true;
      errEl.textContent = "";
    }
  }

  function closeUnlockModal() {
    const overlay = document.getElementById("unlock-overlay");
    if (!overlay) return;
    overlay.hidden = true;
    overlay.setAttribute("aria-hidden", "true");
    currentUnlockContext = null;
    if (lastFocusedBeforeUnlock && typeof lastFocusedBeforeUnlock.focus === "function") {
      lastFocusedBeforeUnlock.focus();
      lastFocusedBeforeUnlock = null;
    }
  }

  function bindUnlockModal() {
    const overlay = document.getElementById("unlock-overlay");
    if (!overlay) return;

    overlay.addEventListener("click", function (e) {
      if (e.target === overlay) closeUnlockModal();
    });

    const form = document.getElementById("unlock-form");
    if (form) {
      form.addEventListener("submit", function (e) {
        e.preventDefault();
        const input = document.getElementById("unlock-pwd-input");
        const val = input ? input.value.trim() : "";
        if (!val) {
          if (input) {
            input.classList.remove("is-shake");
            void input.offsetWidth;
            input.classList.add("is-shake");
          }
          return;
        }
        if (currentUnlockContext && typeof currentUnlockContext.onUnlock === "function") {
          currentUnlockContext.onUnlock(val);
        }
        closeUnlockModal();
      });
    }
  }

  // ===== 绑定分享弹窗整体事件 =====
  function bindShareModal() {
    const overlay = document.getElementById("share-overlay");
    if (!overlay) return;

    const closeBtn = document.getElementById("share-close");
    if (closeBtn) closeBtn.addEventListener("click", closeShareModal);

    overlay.addEventListener("click", function (e) {
      if (e.target === overlay) closeShareModal();
    });

    document.addEventListener("keydown", function (e) {
      if (e.key === "Escape") {
        if (overlay && !overlay.hidden) closeShareModal();
        const unlock = document.getElementById("unlock-overlay");
        if (unlock && !unlock.hidden) closeUnlockModal();
      }
    });

    // 提取码保护开关
    const pwdToggle = document.getElementById("share-pwd-toggle");
    const pwdBox = document.getElementById("share-pwd-box");
    const pwdInput = document.getElementById("share-pwd-input");
    if (pwdToggle && pwdBox) {
      pwdToggle.addEventListener("change", function () {
        pwdBox.hidden = !pwdToggle.checked;
        if (pwdToggle.checked && pwdInput && !pwdInput.value) {
          pwdInput.value = generateRandomSharePin(6);
          pwdInput.focus();
        }
      });
    }

    // 随机密码按钮
    const btnRandom = document.getElementById("btn-share-pwd-random");
    if (btnRandom && pwdInput) {
      btnRandom.addEventListener("click", function () {
        pwdInput.value = generateRandomSharePin(6);
        pwdInput.focus();
      });
    }

    // 提交创建分享
    const btnCreate = document.getElementById("btn-create-share");
    if (btnCreate) {
      btnCreate.addEventListener("click", function () {
        handleCreateShare();
      });
    }

    // 绑定自绘下拉菜单
    const expWrap = document.getElementById("wrap-share-expire");
    if (expWrap && typeof bindCustomSelect === "function") {
      bindCustomSelect(expWrap);
    }
    const usesWrap = document.getElementById("wrap-share-uses");
    if (usesWrap && typeof bindCustomSelect === "function") {
      bindCustomSelect(usesWrap);
    }

    // 从创建分享弹窗底部的“查看已创建的分享记录”跳转
    const openFromModal = document.getElementById("btn-open-shares-from-modal");
    if (openFromModal) {
      openFromModal.addEventListener("click", function () {
        closeShareModal();
        openSharesManageModal();
      });
    }
  }

  // ===== 活跃分享管理 (Share Management) =====
  let activeSharesData = { files: [], dirs: [], total: 0 };
  let currentSharesTab = "all";
  let sharesSearchKeyword = "";
  let lastFocusedBeforeSharesManage = null;

  function formatRemaining(expiresAtTs) {
    const diff = Number(expiresAtTs) - Math.floor(Date.now() / 1000);
    if (diff <= 0) return "已过期";
    const days = Math.floor(diff / 86400);
    if (days >= 2) return days + " 天后到期";
    const hours = Math.floor(diff / 3600);
    if (hours >= 1) return hours + " 小时后到期";
    const minutes = Math.floor(diff / 60);
    return Math.max(1, minutes) + " 分钟后到期";
  }

  function openSharesManageModal() {
    const overlay = document.getElementById("shares-manage-overlay");
    if (!overlay) return;

    lastFocusedBeforeSharesManage = document.activeElement;
    overlay.hidden = false;
    overlay.setAttribute("aria-hidden", "false");

    sharesSearchKeyword = "";
    const searchInput = document.getElementById("shares-search-input");
    if (searchInput) searchInput.value = "";

    fetchSharesList();
  }

  function closeSharesManageModal() {
    const overlay = document.getElementById("shares-manage-overlay");
    if (!overlay) return;
    overlay.hidden = true;
    overlay.setAttribute("aria-hidden", "true");

    if (lastFocusedBeforeSharesManage && typeof lastFocusedBeforeSharesManage.focus === "function") {
      lastFocusedBeforeSharesManage.focus();
      lastFocusedBeforeSharesManage = null;
    }
  }

  function fetchSharesList() {
    const listContainer = document.getElementById("shares-manage-list");
    if (listContainer) {
      listContainer.replaceChildren();
      const loading = document.createElement("div");
      loading.className = "shares-loading";
      if (typeof makeIcon === "function") {
        const iconSpan = document.createElement("span");
        iconSpan.className = "btn-spinner";
        iconSpan.appendChild(makeIcon("loader"));
        loading.appendChild(iconSpan);
      }
      const tipSpan = document.createElement("span");
      tipSpan.textContent = "正在加载活跃分享…";
      loading.appendChild(tipSpan);
      listContainer.appendChild(loading);
    }

    apiFetch("/api/shares", { method: "GET" })
      .then(function (resp) {
        if (!resp.ok) {
          return resp.text().then(function (text) {
            throw new Error(parseAPIError(resp, text));
          });
        }
        return resp.json();
      })
      .then(function (data) {
        activeSharesData = {
          files: Array.isArray(data.files) ? data.files : [],
          dirs: Array.isArray(data.dirs) ? data.dirs : [],
          total: Number(data.total) || 0
        };
        updateSharesCounts();
        renderSharesList();
      })
      .catch(function (err) {
        if (listContainer) {
          listContainer.replaceChildren();
          const errBox = document.createElement("div");
          errBox.className = "shares-empty";
          if (typeof makeIcon === "function") {
            const iconSpan = document.createElement("span");
            iconSpan.className = "shares-empty-icon";
            iconSpan.appendChild(makeIcon("circle-alert"));
            errBox.appendChild(iconSpan);
          }
          const textP = document.createElement("p");
          textP.className = "shares-empty-text";
          textP.textContent = "获取分享列表失败：" + err.message;
          errBox.appendChild(textP);
          listContainer.appendChild(errBox);
        }
        showToast("获取分享列表失败：" + err.message, "error");
      });
  }

  function updateSharesCounts() {
    const totalCount = (activeSharesData.files.length + activeSharesData.dirs.length);
    const countAll = document.getElementById("shares-count-all");
    const countFiles = document.getElementById("shares-count-files");
    const countDirs = document.getElementById("shares-count-dirs");
    if (countAll) countAll.textContent = String(totalCount);
    if (countFiles) countFiles.textContent = String(activeSharesData.files.length);
    if (countDirs) countDirs.textContent = String(activeSharesData.dirs.length);
  }

  function renderSharesList() {
    const listContainer = document.getElementById("shares-manage-list");
    if (!listContainer) return;
    listContainer.replaceChildren();

    let items = [];
    if (currentSharesTab === "all") {
      const taggedFiles = activeSharesData.files.map(function (f) { return Object.assign({}, f, { _type: "file" }); });
      const taggedDirs = activeSharesData.dirs.map(function (d) { return Object.assign({}, d, { _type: "dir" }); });
      items = taggedFiles.concat(taggedDirs);
      items.sort(function (a, b) { return (b.created_at || 0) - (a.created_at || 0); });
    } else if (currentSharesTab === "files") {
      items = activeSharesData.files.map(function (f) { return Object.assign({}, f, { _type: "file" }); });
    } else if (currentSharesTab === "dirs") {
      items = activeSharesData.dirs.map(function (d) { return Object.assign({}, d, { _type: "dir" }); });
    }

    const kw = (sharesSearchKeyword || "").toLowerCase().trim();
    if (kw) {
      items = items.filter(function (it) {
        const name = (it.file_name || it.dir_name || "").toLowerCase();
        const path = (it.file_path || it.dir || "").toLowerCase();
        return name.indexOf(kw) !== -1 || path.indexOf(kw) !== -1;
      });
    }

    if (!items.length) {
      const emptyBox = document.createElement("div");
      emptyBox.className = "shares-empty";
      if (typeof makeIcon === "function") {
        const iconSpan = document.createElement("span");
        iconSpan.className = "shares-empty-icon";
        iconSpan.appendChild(makeIcon("inbox"));
        emptyBox.appendChild(iconSpan);
      }
      const textP = document.createElement("p");
      textP.className = "shares-empty-text";
      textP.textContent = kw ? "未找到匹配的分享链接" : "暂无活跃的分享链接";
      emptyBox.appendChild(textP);

      const subP = document.createElement("p");
      subP.className = "shares-empty-sub";
      subP.textContent = kw ? "请尝试更换关键词搜索" : "在文件列表中点击“分享”即可创建新外链";
      emptyBox.appendChild(subP);

      listContainer.appendChild(emptyBox);
      return;
    }

    items.forEach(function (item) {
      const isDir = item._type === "dir";
      const row = document.createElement("div");
      row.className = "shares-item-row";
      row.dataset.token = item.token;

      // 左侧图标
      const iconWrap = document.createElement("div");
      iconWrap.className = "shares-item-icon" + (isDir ? " is-dir" : "");
      if (typeof makeIcon === "function") {
        const iconMeta = isDir ? { name: "folder" } : (typeof fileIconMeta === "function" ? fileIconMeta(item.file_name) : { name: "file" });
        iconWrap.appendChild(makeIcon(iconMeta.name));
      }
      row.appendChild(iconWrap);

      // 中间信息
      const infoWrap = document.createElement("div");
      infoWrap.className = "shares-item-info";

      const titleRow = document.createElement("div");
      titleRow.className = "shares-item-title-row";
      const titleSpan = document.createElement("span");
      titleSpan.className = "shares-item-title";
      titleSpan.textContent = isDir ? (item.dir_name || "未命名目录") : (item.file_name || "未命名文件");
      titleRow.appendChild(titleSpan);
      infoWrap.appendChild(titleRow);

      const pathRow = document.createElement("div");
      pathRow.className = "shares-item-path";
      pathRow.textContent = isDir ? item.dir : item.file_path;
      infoWrap.appendChild(pathRow);

      // 标签元数据
      const tagsRow = document.createElement("div");
      tagsRow.className = "shares-item-tags";

      const typeTag = document.createElement("span");
      typeTag.className = "shares-tag";
      typeTag.textContent = isDir ? "目录分享" : "文件外链";
      tagsRow.appendChild(typeTag);

      const pwdTag = document.createElement("span");
      pwdTag.className = "shares-tag" + (item.has_password ? " is-locked" : "");
      pwdTag.textContent = item.has_password ? "提取码保护" : "公开访问";
      tagsRow.appendChild(pwdTag);

      if (item.expires_at) {
        const expTag = document.createElement("span");
        expTag.className = "shares-tag";
        expTag.textContent = formatRemaining(item.expires_at);
        expTag.title = "到期时间：" + (typeof formatTime === "function" ? formatTime(item.expires_at) : "");
        tagsRow.appendChild(expTag);
      }

      if (!isDir) {
        const usesTag = document.createElement("span");
        usesTag.className = "shares-tag";
        if (item.max_uses > 0) {
          usesTag.textContent = "已用 " + (item.uses || 0) + " / 限 " + item.max_uses + " 次";
        } else {
          usesTag.textContent = (item.uses || 0) > 0 ? "已用 " + item.uses + " 次" : "不限次";
        }
        tagsRow.appendChild(usesTag);
      }

      infoWrap.appendChild(tagsRow);
      row.appendChild(infoWrap);

      // 右侧操作按钮组
      const actionsWrap = document.createElement("div");
      actionsWrap.className = "shares-item-actions";

      // 复制链接
      const copyBtn = document.createElement("button");
      copyBtn.type = "button";
      copyBtn.className = "btn btn-secondary btn-sm";
      copyBtn.title = "复制分享链接";
      if (typeof makeIcon === "function") {
        const copyIcon = document.createElement("span");
        copyIcon.className = "btn-icon";
        copyIcon.appendChild(makeIcon("copy"));
        copyBtn.appendChild(copyIcon);
      }
      const copyLabel = document.createElement("span");
      copyLabel.textContent = "复制";
      copyBtn.appendChild(copyLabel);
      copyBtn.onclick = function () {
        const fullUrl = window.location.origin + item.url;
        copyText(fullUrl).then(function () {
          showToast("分享链接已复制", "success");
        }).catch(function () {
          showToast("复制失败，请手动复制", "error");
        });
      };
      actionsWrap.appendChild(copyBtn);

      // 撤销按钮
      const revokeBtn = document.createElement("button");
      revokeBtn.type = "button";
      revokeBtn.className = "btn btn-ghost btn-sm btn-revoke";
      revokeBtn.title = "撤销并废除该分享链接";
      if (typeof makeIcon === "function") {
        const revokeIcon = document.createElement("span");
        revokeIcon.className = "btn-icon";
        revokeIcon.appendChild(makeIcon("trash-2"));
        revokeBtn.appendChild(revokeIcon);
      }
      const revokeLabel = document.createElement("span");
      revokeLabel.textContent = "撤销";
      revokeBtn.appendChild(revokeLabel);

      revokeBtn.onclick = function () {
        handleRevokeShare(item.token, revokeBtn, row);
      };
      actionsWrap.appendChild(revokeBtn);

      row.appendChild(actionsWrap);
      listContainer.appendChild(row);
    });
  }

  function handleRevokeShare(token, btn, rowEl) {
    if (typeof setButtonLoading === "function") {
      setButtonLoading(btn, true, "撤销中…");
    }

    apiFetch("/api/shares/revoke", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ token: token })
    })
      .then(function (resp) {
        if (!resp.ok) {
          return resp.text().then(function (text) {
            throw new Error(parseAPIError(resp, text));
          });
        }
        return resp.json();
      })
      .then(function () {
        showToast("已成功撤销该分享链接", "success");
        activeSharesData.files = activeSharesData.files.filter(function (f) { return f.token !== token; });
        activeSharesData.dirs = activeSharesData.dirs.filter(function (d) { return d.token !== token; });
        activeSharesData.total = activeSharesData.files.length + activeSharesData.dirs.length;
        updateSharesCounts();

        if (rowEl) {
          rowEl.style.transition = "opacity 0.2s, transform 0.2s";
          rowEl.style.opacity = "0";
          rowEl.style.transform = "translateX(10px)";
          setTimeout(function () {
            renderSharesList();
          }, 200);
        } else {
          renderSharesList();
        }
      })
      .catch(function (err) {
        if (typeof setButtonLoading === "function") {
          setButtonLoading(btn, false);
        }
        showToast("撤销失败：" + err.message, "error");
      });
  }

  function bindSharesManageModal() {
    const overlay = document.getElementById("shares-manage-overlay");
    if (!overlay) return;

    const closeBtn = document.getElementById("shares-manage-close");
    if (closeBtn) closeBtn.addEventListener("click", closeSharesManageModal);

    overlay.addEventListener("click", function (e) {
      if (e.target === overlay) closeSharesManageModal();
    });

    document.addEventListener("keydown", function (e) {
      if (e.key === "Escape") {
        if (overlay && !overlay.hidden) closeSharesManageModal();
      }
    });

    const tabs = overlay.querySelectorAll(".shares-tab");
    tabs.forEach(function (tab) {
      tab.addEventListener("click", function () {
        tabs.forEach(function (t) {
          t.classList.remove("is-active");
          t.setAttribute("aria-selected", "false");
        });
        tab.classList.add("is-active");
        tab.setAttribute("aria-selected", "true");
        currentSharesTab = tab.getAttribute("data-tab") || "all";
        renderSharesList();
      });
    });

    const searchInput = document.getElementById("shares-search-input");
    if (searchInput) {
      searchInput.addEventListener("input", function () {
        sharesSearchKeyword = searchInput.value;
        renderSharesList();
      });
    }

    const refreshBtn = document.getElementById("btn-refresh-shares");
    if (refreshBtn) {
      refreshBtn.addEventListener("click", function () {
        fetchSharesList();
      });
    }
  }

  // 挂载至全局
  root.generateRandomSharePin = generateRandomSharePin;
  root.openShareModal = openShareModal;
  root.closeShareModal = closeShareModal;
  root.openUnlockModal = openUnlockModal;
  root.closeUnlockModal = closeUnlockModal;
  root.bindUnlockModal = bindUnlockModal;
  root.bindShareModal = bindShareModal;
  root.openSharesManageModal = openSharesManageModal;
  root.closeSharesManageModal = closeSharesManageModal;
  root.bindSharesManageModal = bindSharesManageModal;

})(typeof window !== "undefined" ? window : globalThis);

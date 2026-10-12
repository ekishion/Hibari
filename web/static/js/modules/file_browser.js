// 即刻轻盘文件浏览模块：沙盒虚拟根隔离、受限面包屑、列表渲染、排序检索与 README 面板。
(function (root) {
  'use strict';

  let currentFiles = [];
  let currentDir = "/";
  let sandboxRoot = ""; // 目录分享沙盒根节点（非空时禁止向上越权浏览）
  let currentDirToken = "";
  let currentDirPassword = "";
  let currentSearchQuery = "";
  let fileListSeq = 0;
  let readmeSeq = 0;

  // ===== 排序与检索 =====
  function sortFiles(files) {
    const list = (files || []).slice();
    const sortKey = (root.sortState && root.sortState.key) || "name";
    const sortDir = (root.sortState && root.sortState.dir) || "asc";

    list.sort(function (a, b) {
      const aIsDir = Number(a.isdir) === 1;
      const bIsDir = Number(b.isdir) === 1;
      if (aIsDir !== bIsDir) return aIsDir ? -1 : 1;

      let res = 0;
      if (sortKey === "size") {
        res = (Number(a.size) || 0) - (Number(b.size) || 0);
      } else if (sortKey === "time") {
        res = (Number(a.server_mtime || a.mtime || a.server_ctime) || 0) - (Number(b.server_mtime || b.mtime || b.server_ctime) || 0);
      } else {
        const aName = a.server_filename || a.filename || (a.path || "").split("/").pop() || "";
        const bName = b.server_filename || b.filename || (b.path || "").split("/").pop() || "";
        res = aName.localeCompare(bName, "zh-CN", { numeric: true, sensitivity: "base" });
      }
      return sortDir === "desc" ? -res : res;
    });
    return list;
  }

  function filterCurrentFiles(query) {
    currentSearchQuery = (query || "").trim().toLowerCase();
    if (!currentSearchQuery) return currentFiles;
    return currentFiles.filter(function (f) {
      const name = (f.server_filename || f.filename || (f.path || "").split("/").pop() || "").toLowerCase();
      return name.indexOf(currentSearchQuery) !== -1;
    });
  }

  function resetFileSearch() {
    currentSearchQuery = "";
    const input = document.getElementById("file-search");
    if (input) input.value = "";
  }

  // ===== 面包屑导航 (支持虚拟根沙盒隔离) =====
  function renderBreadcrumbs() {
    const nav = document.getElementById("breadcrumbs");
    if (!nav) return;
    nav.replaceChildren();

    // 如果处于目录分享沙盒模式下，禁止暴露和点击上级真实路径与全盘根目录
    if (sandboxRoot) {
      const cleanRoot = sandboxRoot.replace(/\/+$/, "") || "/";
      const rootBaseName = cleanRoot.split("/").pop() || "分享目录";

      // 渲染沙盒虚拟根节点
      const rootBtn = document.createElement("button");
      rootBtn.type = "button";
      rootBtn.className = "breadcrumb-item";
      rootBtn.dataset.path = cleanRoot;

      const folderIcon = document.createElement("span");
      folderIcon.className = "breadcrumb-icon";
      if (typeof makeIcon === "function") folderIcon.appendChild(makeIcon("folder"));
      rootBtn.appendChild(folderIcon);

      const label = document.createElement("span");
      label.className = "breadcrumb-label";
      label.textContent = rootBaseName;
      rootBtn.appendChild(label);

      rootBtn.onclick = function () { enterDir(cleanRoot); };
      nav.appendChild(rootBtn);

      // 计算当前目录相对沙盒根的子路径
      let relSub = "";
      if (currentDir.startsWith(cleanRoot + "/")) {
        relSub = currentDir.slice(cleanRoot.length + 1);
      }
      if (relSub) {
        const parts = relSub.split("/").filter(Boolean);
        let acc = cleanRoot;
        for (let i = 0; i < parts.length; i++) {
          acc += "/" + parts[i];
          const partPath = acc;

          const sep = document.createElement("span");
          sep.className = "breadcrumb-sep";
          if (typeof makeIcon === "function") sep.appendChild(makeIcon("chevron-right"));
          nav.appendChild(sep);

          const itemBtn = document.createElement("button");
          itemBtn.type = "button";
          itemBtn.className = "breadcrumb-item";
          itemBtn.dataset.path = partPath;
          itemBtn.textContent = parts[i];
          itemBtn.onclick = function () { enterDir(partPath); };
          nav.appendChild(itemBtn);
        }
      }
      return;
    }

    // 普通全盘浏览模式：显示“根目录”与层级路径
    const rootBtn = document.createElement("button");
    rootBtn.type = "button";
    rootBtn.className = "breadcrumb-item";
    rootBtn.dataset.path = "/";

    const cloudIcon = document.createElement("span");
    cloudIcon.className = "breadcrumb-icon";
    if (typeof makeIcon === "function") cloudIcon.appendChild(makeIcon("cloud"));
    rootBtn.appendChild(cloudIcon);

    const label = document.createElement("span");
    label.className = "breadcrumb-label";
    label.textContent = "根目录";
    rootBtn.appendChild(label);

    rootBtn.onclick = function () { enterDir("/"); };
    nav.appendChild(rootBtn);

    const clean = currentDir.replace(/\/+$/, "");
    if (!clean || clean === "/") return;

    const parts = clean.split("/").filter(Boolean);
    let acc = "";
    for (let i = 0; i < parts.length; i++) {
      acc += "/" + parts[i];
      const partPath = acc;

      const sep = document.createElement("span");
      sep.className = "breadcrumb-sep";
      if (typeof makeIcon === "function") sep.appendChild(makeIcon("chevron-right"));
      nav.appendChild(sep);

      const itemBtn = document.createElement("button");
      itemBtn.type = "button";
      itemBtn.className = "breadcrumb-item";
      itemBtn.dataset.path = partPath;
      itemBtn.textContent = parts[i];
      itemBtn.onclick = function () { enterDir(partPath); };
      nav.appendChild(itemBtn);
    }
  }

  // ===== 构建“返回上一级”项 =====
  function buildParentItem() {
    // 若在沙盒虚拟根，禁止展示返回上一级项
    if (sandboxRoot && (currentDir === sandboxRoot || currentDir === sandboxRoot + "/")) {
      return null;
    }
    if (!sandboxRoot && (currentDir === "/" || !currentDir)) {
      return null;
    }

    const clean = currentDir.replace(/\/+$/, "");
    const parentPath = clean.substring(0, clean.lastIndexOf("/")) || "/";

    // 若计算后的上一级脱离了沙盒根，直接阻断
    if (sandboxRoot && !parentPath.startsWith(sandboxRoot)) {
      return null;
    }

    const item = document.createElement("div");
    item.className = "file-item is-dir";
    item.setAttribute("role", "button");
    item.tabIndex = 0;

    const icon = document.createElement("div");
    icon.className = "file-icon";
    icon.setAttribute("data-cat", "folder");
    if (typeof makeIcon === "function") icon.appendChild(makeIcon("corner-left-up"));

    const info = document.createElement("div");
    info.className = "file-info";
    const name = document.createElement("div");
    name.className = "file-name";
    name.textContent = "返回上级";
    const meta = document.createElement("div");
    meta.className = "file-meta";
    meta.textContent = "返回上一层目录";
    info.appendChild(name);
    info.appendChild(meta);

    item.appendChild(icon);
    item.appendChild(info);

    const go = function () {
      enterDir(parentPath);
    };
    item.addEventListener("click", go);
    item.addEventListener("keydown", function (e) {
      if (e.key === "Enter" || e.key === " ") {
        e.preventDefault();
        go();
      }
    });
    return item;
  }

  // ===== 构建文件/文件夹项 =====
  function buildFileItem(file) {
    const isDir = Number(file.isdir) === 1;
    const fileName = file.server_filename || file.filename || (file.path || "").split("/").pop() || "未命名";
    const filePath = file.path || (currentDir.replace(/\/+$/, "") + "/" + fileName);

    const item = document.createElement("div");
    item.className = "file-item" + (isDir ? " is-dir" : "");
    item.dataset.filename = fileName;

    const metaInfo = typeof fileIconMeta === "function" ? fileIconMeta(fileName, isDir) : { name: isDir ? "folder" : "file" };
    const icon = document.createElement("div");
    icon.className = "file-icon";
    icon.setAttribute("data-cat", isDir ? "folder" : (metaInfo.category || "default"));
    if (typeof makeIcon === "function") icon.appendChild(makeIcon(metaInfo.name));

    const info = document.createElement("div");
    info.className = "file-info";
    const name = document.createElement("div");
    name.className = "file-name";
    name.textContent = fileName;
    name.title = fileName;

    const meta = document.createElement("div");
    meta.className = "file-meta";
    const timeStr = typeof formatTime === "function" ? formatTime(file.server_mtime || file.server_ctime || file.mtime) : "";
    if (isDir) {
      meta.textContent = "文件夹" + (timeStr ? " · " + timeStr : "");
    } else {
      const sizeStr = typeof formatSize === "function" ? formatSize(file.size) : file.size;
      meta.textContent = sizeStr + (timeStr ? " · " + timeStr : "");
    }
    info.appendChild(name);
    info.appendChild(meta);

    item.appendChild(icon);
    item.appendChild(info);

    if (isDir) {
      item.setAttribute("role", "button");
      item.tabIndex = 0;
      const openDir = function () {
        enterDir(filePath);
      };
      item.addEventListener("click", openDir);
      item.addEventListener("keydown", function (e) {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          openDir();
        }
      });

      // 文件夹操作按钮：支持分享子文件夹
      const actionsEl = document.createElement("div");
      actionsEl.className = "file-actions";

      const shareBtn = document.createElement("button");
      shareBtn.className = "file-action";
      shareBtn.type = "button";
      if (typeof makeIcon === "function") shareBtn.appendChild(makeIcon("share-2"));
      const shareLabel = document.createElement("span");
      shareLabel.textContent = "分享";
      shareBtn.appendChild(shareLabel);
      shareBtn.setAttribute("aria-label", "分享文件夹 " + fileName);
      shareBtn.addEventListener("click", function (e) {
        e.stopPropagation();
        if (typeof openShareModal === "function") {
          openShareModal({ type: "dir", path: filePath, name: fileName });
        }
      });
      actionsEl.appendChild(shareBtn);
      item.appendChild(actionsEl);

    } else {
      // 单文件操作按钮
      const actionsEl = document.createElement("div");
      actionsEl.className = "file-actions";

      // 预览按钮（若格式支持视频、音频、图片或文本预览）
      if (typeof previewKind === "function" && previewKind(fileName)) {
        const previewBtn = document.createElement("button");
        previewBtn.className = "file-action";
        previewBtn.type = "button";
        if (typeof makeIcon === "function") previewBtn.appendChild(makeIcon("eye"));
        const previewLabel = document.createElement("span");
        previewLabel.textContent = "预览";
        previewBtn.appendChild(previewLabel);
        previewBtn.setAttribute("aria-label", "预览 " + fileName);
        previewBtn.addEventListener("click", function (e) {
          e.stopPropagation();
          if (typeof openPreview === "function") openPreview(filePath, fileName);
        });
        actionsEl.appendChild(previewBtn);
      }

      // 分享按钮
      const shareBtn = document.createElement("button");
      shareBtn.className = "file-action";
      shareBtn.type = "button";
      if (typeof makeIcon === "function") shareBtn.appendChild(makeIcon("share-2"));
      const shareLabel = document.createElement("span");
      shareLabel.textContent = "分享";
      shareBtn.appendChild(shareLabel);
      shareBtn.setAttribute("aria-label", "分享 " + fileName);
      shareBtn.addEventListener("click", function (e) {
        e.stopPropagation();
        if (typeof openShareModal === "function") {
          openShareModal({ type: "file", path: filePath, name: fileName });
        }
      });
      actionsEl.appendChild(shareBtn);

      // 下载按钮
      const dlBtn = document.createElement("button");
      dlBtn.className = "file-action";
      dlBtn.type = "button";
      if (typeof makeIcon === "function") dlBtn.appendChild(makeIcon("download"));
      const dlLabel = document.createElement("span");
      dlLabel.textContent = "下载";
      dlBtn.appendChild(dlLabel);
      dlBtn.setAttribute("aria-label", "下载 " + fileName);
      const onDownload = function (e) {
        e.preventDefault();
        e.stopPropagation();
        downloadFileDirect(filePath, fileName, dlBtn);
      };
      dlBtn.addEventListener("click", onDownload);
      dlBtn.addEventListener("dblclick", function (e) {
        e.preventDefault();
        e.stopPropagation();
      });
      actionsEl.appendChild(dlBtn);

      item.appendChild(actionsEl);

      // 双击直接下载
      item.addEventListener("dblclick", function (e) {
        if (e.target && e.target.closest && e.target.closest(".file-action")) return;
        downloadFileDirect(filePath, fileName, dlBtn);
      });
    }

    return item;
  }

  // 直接下载单个文件
  function downloadFileDirect(filePath, fileName, btn) {
    if (typeof setButtonLoading === "function") setButtonLoading(btn, true, "");
    requestShortLink(filePath)
      .then(function (url) {
        if (typeof setButtonLoading === "function") setButtonLoading(btn, false);
        triggerDownloadNavigation(url);
      })
      .catch(function (err) {
        if (typeof setButtonLoading === "function") setButtonLoading(btn, false);
        showToast("下载失败：" + err.message, "error");
      });
  }

  // ===== 渲染文件列表视图 =====
  function renderCurrentList() {
    renderBreadcrumbs();
    const container = document.getElementById("file-list");
    if (!container) return;

    const filtered = filterCurrentFiles(currentSearchQuery);
    const sorted = sortFiles(filtered);

    const countEl = document.getElementById("file-count");
    if (countEl) {
      countEl.textContent = "共 " + sorted.length + " 项" + (currentSearchQuery ? "（已筛选）" : "");
    }

    container.replaceChildren();

    const parentRow = buildParentItem();
    if (parentRow) container.appendChild(parentRow);

    if (!sorted.length) {
      if (!parentRow) {
        const empty = document.createElement("div");
        empty.className = "empty-state";
        const emptyIcon = document.createElement("div");
        emptyIcon.className = "empty-icon";
        if (typeof makeIcon === "function") emptyIcon.appendChild(makeIcon("folder-open"));
        empty.appendChild(emptyIcon);
        const p = document.createElement("p");
        p.textContent = currentSearchQuery ? "未找到匹配的文件" : "此文件夹为空";
        empty.appendChild(p);
        container.appendChild(empty);
      }
      return;
    }

    for (let i = 0; i < sorted.length; i++) {
      container.appendChild(buildFileItem(sorted[i]));
    }
  }

  // ===== 加载目录内容 =====
  function loadFiles(dir, opts) {
    fileListSeq++;
    const seq = fileListSeq;
    const options = opts || {};
    const statusEl = document.getElementById("status");
    if (statusEl) {
      statusEl.style.display = "block";
      const tip = statusEl.querySelector(".status-tip");
      if (tip) tip.textContent = "正在拉取文件列表…";
    }

    const payload = {
      dir: dir || currentDir || "/",
      token: currentDirToken,
      password: currentDirPassword,
      refresh: !!options.refresh
    };

    apiFetch("/api/files", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload)
    })
      .then(function (resp) {
        if (resp.status === 401) {
          // 密码保护目录，唤起提取码解锁弹窗
          return resp.json().then(function (errData) {
            if (errData && errData.error && errData.error.code === "share_password_required") {
              if (statusEl) statusEl.style.display = "none";
              if (typeof openUnlockModal === "function") {
                openUnlockModal({
                  onUnlock: function (pwd) {
                    currentDirPassword = pwd;
                    loadFiles(dir, opts);
                  }
                });
              }
              throw new Error("password_required");
            }
            throw new Error(errData.error ? errData.error.message : "未授权访问");
          });
        }
        if (!resp.ok) {
          return resp.text().then(function (text) {
            throw new Error(parseAPIError(resp, text));
          });
        }
        return resp.json();
      })
      .then(function (data) {
        if (seq !== fileListSeq) return;
        if (statusEl) statusEl.style.display = "none";

        // 锁定沙盒虚拟根
        if (data.sandbox_root) {
          sandboxRoot = data.sandbox_root;
        }
        if (data.resolved_dir) {
          currentDir = data.resolved_dir;
        } else if (dir) {
          currentDir = dir;
        }

        currentFiles = data.list || [];
        renderCurrentList();

        // README 探测与展示
        checkAndLoadReadme(currentFiles, currentDir);
      })
      .catch(function (err) {
        if (seq !== fileListSeq) return;
        if (statusEl) statusEl.style.display = "none";
        if (err.message !== "password_required") {
          showToast("加载目录失败：" + err.message, "error");
        }
      });
  }

  function enterDir(dir) {
    if (sandboxRoot && !dir.startsWith(sandboxRoot)) {
      showToast("无法超出共享目录范围", "warning");
      return;
    }
    currentDir = dir;
    resetFileSearch();
    loadFiles(dir);
  }

  // ===== README 面板管理 =====
  function checkAndLoadReadme(files, dir) {
    const readmeFile = (files || []).find(function (f) {
      const name = (f.server_filename || f.filename || "").toLowerCase();
      return name === "readme.md" || name === "readme.txt";
    });
    const panel = document.getElementById("readme-panel");
    if (!readmeFile) {
      if (panel) panel.hidden = true;
      return;
    }
    readmeSeq++;
    const seq = readmeSeq;
    const filePath = readmeFile.path || (dir.replace(/\/+$/, "") + "/" + (readmeFile.server_filename || readmeFile.filename));

    apiFetch("/api/readme", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ path: filePath })
    })
      .then(function (resp) {
        if (!resp.ok) return null;
        return resp.json();
      })
      .then(function (data) {
        if (seq !== readmeSeq || !data || !data.content) {
          if (panel) panel.hidden = true;
          return;
        }
        if (panel) {
          panel.hidden = false;
          const box = document.getElementById("readme-content");
          if (box) {
            box.replaceChildren();
            if (typeof renderMarkdown === "function") {
              renderMarkdown(box, data.content);
            } else {
              box.textContent = data.content;
            }
          }
        }
      })
      .catch(function () {
        if (panel) panel.hidden = true;
      });
  }

  // 挂载至全局
  root.loadFiles = loadFiles;
  root.enterDir = enterDir;
  root.renderCurrentList = renderCurrentList;
  root.renderBreadcrumbs = renderBreadcrumbs;
  root.sortFiles = sortFiles;
  root.filterCurrentFiles = filterCurrentFiles;
  root.resetFileSearch = resetFileSearch;
  Object.defineProperty(root, "currentFiles", {
    get: function () { return currentFiles; },
    set: function (v) { currentFiles = v; }
  });
  Object.defineProperty(root, "currentDir", {
    get: function () { return currentDir; },
    set: function (v) { currentDir = v; }
  });
  Object.defineProperty(root, "sandboxRoot", {
    get: function () { return sandboxRoot; },
    set: function (v) { sandboxRoot = v; }
  });
  Object.defineProperty(root, "currentDirToken", {
    get: function () { return currentDirToken; },
    set: function (v) { currentDirToken = v; }
  });
  Object.defineProperty(root, "currentDirPassword", {
    get: function () { return currentDirPassword; },
    set: function (v) { currentDirPassword = v; }
  });

})(typeof window !== "undefined" ? window : globalThis);

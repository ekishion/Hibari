// 即刻轻盘应用主编排入口：主题、鉴权状态、路由调度与全局事件委托。
(function (root) {
  'use strict';

  // ===== 全局配置与状态 =====
  const THEME_STORAGE_KEY = "jikeqingpan_theme";
  const SORT_STORAGE_KEY = "jikeqingpan_sort";

  let authRequired = false;
  let authenticated = false;
  let uiConfig = { showReadme: true, showReadmeOverview: true, sharePasswordLength: 6 };
  let sortState = { key: "name", dir: "asc" };
  let lastFocusedBeforeModal = null;

  // ===== 主题管理 =====
  function applyTheme(theme) {
    document.documentElement.setAttribute("data-theme", theme === "dark" ? "dark" : "light");
    const themeColor = document.querySelector('meta[name="theme-color"]');
    if (themeColor) themeColor.setAttribute("content", theme === "dark" ? "#0a0a0b" : "#ffffff");
    const btn = document.getElementById("btn-theme");
    if (btn) {
      const iconWrap = btn.querySelector(".btn-icon");
      if (iconWrap && typeof makeIcon === "function") {
        iconWrap.replaceChildren(makeIcon(theme === "dark" ? "sun" : "moon"));
        iconWrap.dataset.iconMounted = "1";
      }
      btn.setAttribute("aria-label", theme === "dark" ? "切换到浅色模式" : "切换到深色模式");
    }
  }

  function currentTheme() {
    return document.documentElement.getAttribute("data-theme") === "dark" ? "dark" : "light";
  }

  function initTheme() {
    let stored = null;
    try { stored = localStorage.getItem(THEME_STORAGE_KEY); } catch (e) { /* ignore */ }
    if (stored !== "light" && stored !== "dark") {
      stored = window.matchMedia && window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
    }
    applyTheme(stored);
  }

  function toggleTheme() {
    const next = currentTheme() === "dark" ? "light" : "dark";
    applyTheme(next);
    try { localStorage.setItem(THEME_STORAGE_KEY, next); } catch (e) { /* ignore */ }
  }

  // ===== 排序控制 =====
  const SORT_FIELD_LABELS = { name: "名称", size: "大小", time: "时间" };

  function loadSortState() {
    try {
      const parsed = JSON.parse(localStorage.getItem(SORT_STORAGE_KEY) || "null");
      if (parsed && ["name", "size", "time"].indexOf(parsed.key) !== -1 &&
        ["asc", "desc"].indexOf(parsed.dir) !== -1) {
        sortState = { key: parsed.key, dir: parsed.dir };
      }
    } catch (e) { /* ignore */ }
    syncSortControls();
  }

  function saveSortState() {
    try { localStorage.setItem(SORT_STORAGE_KEY, JSON.stringify(sortState)); } catch (e) { /* ignore */ }
  }

  function syncSortControls() {
    const dirBtn = document.getElementById("btn-sort-dir");
    const menu = document.getElementById("sort-menu");
    const label = document.getElementById("sort-field-label");
    if (label) label.textContent = SORT_FIELD_LABELS[sortState.key] || "名称";
    if (menu) {
      const options = menu.querySelectorAll(".sort-option");
      for (let i = 0; i < options.length; i++) {
        const opt = options[i];
        const active = opt.dataset.key === sortState.key;
        opt.classList.toggle("is-active", active);
        opt.setAttribute("aria-selected", active ? "true" : "false");
      }
    }
    if (dirBtn) {
      const iconWrap = dirBtn.querySelector(".btn-icon");
      if (iconWrap && typeof makeIcon === "function") {
        iconWrap.replaceChildren(makeIcon(sortState.dir === "asc" ? "arrow-up" : "arrow-down"));
        iconWrap.dataset.iconMounted = "1";
      }
      dirBtn.setAttribute("aria-label", sortState.dir === "asc" ? "当前升序，点击切换为降序" : "当前降序，点击切换为升序");
    }
  }

  function setSortMenuOpen(open) {
    const menu = document.getElementById("sort-menu");
    const trigger = document.getElementById("btn-sort-field");
    if (!menu || !trigger) return;
    menu.hidden = !open;
    trigger.setAttribute("aria-expanded", open ? "true" : "false");
  }

  function bindSortControls() {
    const trigger = document.getElementById("btn-sort-field");
    const menu = document.getElementById("sort-menu");
    const dirBtn = document.getElementById("btn-sort-dir");

    if (trigger && menu && !trigger.dataset.bound) {
      trigger.dataset.bound = "1";
      trigger.addEventListener("click", function (e) {
        e.stopPropagation();
        setSortMenuOpen(menu.hidden);
      });
      menu.addEventListener("click", function (e) {
        e.stopPropagation();
      });
      const options = menu.querySelectorAll(".sort-option");
      for (let i = 0; i < options.length; i++) {
        options[i].addEventListener("click", function () {
          if (sortState.key !== options[i].dataset.key) {
            sortState.key = options[i].dataset.key;
            saveSortState();
            syncSortControls();
            if (typeof renderCurrentList === "function") renderCurrentList();
          }
          setSortMenuOpen(false);
        });
      }
      document.addEventListener("click", function () {
        setSortMenuOpen(false);
      });
    }

    if (dirBtn && !dirBtn.dataset.bound) {
      dirBtn.dataset.bound = "1";
      dirBtn.addEventListener("click", function () {
        sortState.dir = sortState.dir === "asc" ? "desc" : "asc";
        saveSortState();
        syncSortControls();
        if (typeof renderCurrentList === "function") renderCurrentList();
      });
    }
  }

  // ===== 鉴权管理 =====
  function showLogin(show) {
    const overlay = document.getElementById("login-overlay");
    if (!overlay) return;
    if (show) {
      overlay.hidden = false;
      overlay.style.display = "flex";
      overlay.setAttribute("aria-hidden", "false");
      const input = document.getElementById("login-token");
      if (input) {
        setTimeout(function () {
          input.focus();
          input.select();
        }, 50);
      }
    } else {
      overlay.hidden = true;
      overlay.style.display = "none";
      overlay.setAttribute("aria-hidden", "true");
    }
  }

  function updateAuthUI() {
    const logoutBtn = document.getElementById("btn-logout");
    const loginBtn = document.getElementById("btn-login-open");
    const shareBtn = document.getElementById("btn-share");
    const sharesManageBtn = document.getElementById("btn-shares-manage");
    if (logoutBtn) {
      logoutBtn.hidden = !(authRequired && authenticated);
    }
    if (loginBtn) {
      loginBtn.hidden = !(authRequired && !authenticated);
    }
    if (shareBtn) {
      // 在单目录分享沙盒模式下，隐藏全局分享按钮
      if (root.sandboxRoot) {
        shareBtn.hidden = true;
      } else {
        shareBtn.hidden = authRequired && !authenticated;
      }
    }
    if (sharesManageBtn) {
      if (root.sandboxRoot) {
        sharesManageBtn.hidden = true;
      } else {
        sharesManageBtn.hidden = authRequired && !authenticated;
      }
    }
  }

  function requireLogin(message) {
    authRequired = true;
    authenticated = false;
    updateAuthUI();
    showLogin(true);

    const errEl = document.getElementById("login-error");
    if (errEl) {
      errEl.hidden = !message;
      errEl.textContent = message || "";
    }
  }

  function checkAuth() {
    return apiFetch("/api/auth/status", { method: "GET" })
      .then(function (resp) {
        if (!resp.ok) throw new Error("无法确认登录状态");
        return resp.json();
      })
      .then(function (data) {
        authRequired = !!data.auth_required;
        authenticated = !!data.authenticated;
        uiConfig.showReadme = data.show_readme !== false;
        uiConfig.showReadmeOverview = data.show_readme_overview !== false;
        if (typeof data.share_password_length === "number" && data.share_password_length >= 2) {
          uiConfig.sharePasswordLength = data.share_password_length;
        }
        updateAuthUI();
        if (authRequired && !authenticated) {
          requireLogin("");
          return false;
        }
        showLogin(false);
        return true;
      });
  }

  function doLogin(token) {
    return apiFetch("/api/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ token: token })
    }).then(function (resp) {
      return resp.text().then(function (text) {
        if (!resp.ok) {
          const err = new Error(parseAPIError(resp, text));
          if (resp.status === 429) {
            const retry = parseInt(resp.headers.get("Retry-After") || "", 10);
            if (!isNaN(retry) && retry > 0) err.retryAfter = retry;
          }
          throw err;
        }
        authRequired = true;
        authenticated = true;
        updateAuthUI();
        showLogin(false);
        showToast("验证成功", "success");
        if (typeof loadFiles === "function") {
          return loadFiles(root.currentDir || "/");
        }
      });
    });
  }

  function doLogout() {
    return apiFetch("/api/logout", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: "{}"
    })
      .catch(function () { /* clear UI regardless */ })
      .then(function () {
        authenticated = false;
        updateAuthUI();
        const listEl = document.getElementById("file-list");
        if (listEl) listEl.replaceChildren();
        if (authRequired) {
          requireLogin("");
          showToast("已退出登录", "info");
        } else {
          showLogin(false);
          showToast("已退出登录", "info");
        }
      });
  }

  function bindLoginForm(loginForm) {
    if (!loginForm || loginForm.dataset.bound === "1") return;
    loginForm.dataset.bound = "1";
    loginForm.addEventListener("submit", function (e) {
      e.preventDefault();
      const input = document.getElementById("login-token");
      const token = input ? input.value.trim() : "";
      const errEl = document.getElementById("login-error");
      if (errEl) {
        errEl.hidden = true;
        errEl.textContent = "";
      }
      if (!token) {
        if (errEl) {
          errEl.hidden = false;
          errEl.textContent = "请输入访问令牌";
        }
        if (input) input.focus();
        return;
      }
      const btn = document.getElementById("btn-login");
      setButtonLoading(btn, true, "验证中…");
      doLogin(token)
        .catch(function (err) {
          if (err && err.retryAfter) {
            startLoginCountdown(err.retryAfter);
          } else if (errEl) {
            errEl.hidden = false;
            errEl.textContent = err.message || "验证失败，请重试";
            showLogin(true);
            if (input) {
              input.focus();
              input.select();
            }
          }
        })
        .finally(function () {
          if (!btn || !btn.dataset.countdown) setButtonLoading(btn, false);
        });
    });
  }

  function startLoginCountdown(seconds) {
    const btn = document.getElementById("btn-login");
    const errEl = document.getElementById("login-error");
    const input = document.getElementById("login-token");
    if (!btn) return;
    const label = btn.querySelector(".btn-label");
    let remaining = Math.max(1, seconds);
    btn.dataset.countdown = "1";
    if (input) input.blur();
    const tick = function () {
      if (errEl) {
        errEl.hidden = false;
        errEl.textContent = "验证过于频繁，" + remaining + " 秒后再试";
      }
      if (label) label.textContent = "请等待 " + remaining + " 秒";
      if (remaining <= 0) {
        clearInterval(timer);
        delete btn.dataset.countdown;
        setButtonLoading(btn, false);
        if (errEl) {
          errEl.hidden = true;
          errEl.textContent = "";
        }
        if (input) input.focus();
        return;
      }
      remaining--;
    };
    tick();
    const timer = setInterval(tick, 1000);
  }

  // ===== 路由与主流程初始化 =====
  function initApp() {
    // 挂载静态 Lucide 图标
    if (typeof mountIcons === "function") mountIcons();

    // 初始化深浅配色主题
    initTheme();

    const btnTheme = document.getElementById("btn-theme");
    if (btnTheme) btnTheme.addEventListener("click", toggleTheme);

    // 绑定模态框与组件
    if (typeof bindShareModal === "function") bindShareModal();
    if (typeof bindSharesManageModal === "function") bindSharesManageModal();
    if (typeof bindUnlockModal === "function") bindUnlockModal();

    const sharesManageBtn = document.getElementById("btn-shares-manage");
    if (sharesManageBtn) {
      sharesManageBtn.addEventListener("click", function () {
        if (typeof openSharesManageModal === "function") openSharesManageModal();
      });
    }

    const loginForm = document.getElementById("login-form");
    if (loginForm) bindLoginForm(loginForm);

    const loginOpenBtn = document.getElementById("btn-login-open");
    if (loginOpenBtn) {
      loginOpenBtn.addEventListener("click", function () {
        requireLogin("");
      });
    }

    const logoutBtn = document.getElementById("btn-logout");
    if (logoutBtn) {
      logoutBtn.addEventListener("click", doLogout);
    }

    const refreshBtn = document.getElementById("btn-refresh");
    if (refreshBtn) {
      refreshBtn.addEventListener("click", function () {
        if (authRequired && !authenticated) {
          requireLogin("");
          return;
        }
        if (typeof loadFiles === "function") {
          loadFiles(root.currentDir || "/", { refresh: true });
        }
      });
    }

    // 分享当前目录按钮
    const btnShareCurrent = document.getElementById("btn-share");
    if (btnShareCurrent) {
      btnShareCurrent.addEventListener("click", function () {
        if (typeof openShareModal === "function") {
          openShareModal({
            type: "dir",
            path: root.currentDir || "/",
            name: (root.currentDir || "/").split("/").pop() || "根目录"
          });
        }
      });
    }

    // 排序控制绑定
    loadSortState();
    bindSortControls();

    // 搜索框过滤与 Escape 清空
    const searchBox = document.getElementById("file-search");
    if (searchBox) {
      let searchDebounce = null;
      searchBox.addEventListener("input", function () {
        if (searchDebounce) clearTimeout(searchDebounce);
        searchDebounce = setTimeout(function () {
          searchDebounce = null;
          if (typeof filterCurrentFiles === "function") {
            filterCurrentFiles(searchBox.value);
            if (typeof renderCurrentList === "function") renderCurrentList();
          }
        }, 150);
      });
      searchBox.addEventListener("keydown", function (event) {
        if (event.key === "Escape") {
          event.stopPropagation();
          searchBox.value = "";
          if (typeof resetFileSearch === "function") resetFileSearch();
          if (typeof renderCurrentList === "function") renderCurrentList();
          searchBox.blur();
        }
      });
    }

    // 路由分发决策：
    // 1. 单文件分享落地页 (?s={token})
    const shareToken = shareTokenFromLocation();
    const urlParams = new URLSearchParams(window.location.search);
    const isExpired = urlParams.get("err") === "expired";

    if (shareToken) {
      const appMain = document.getElementById("app-main");
      if (appMain) appMain.hidden = true;
      if (isExpired) {
        if (typeof renderShareErrorCard === "function") {
          renderShareErrorCard("下载链接已失效", "该分享链接已过期、已达到下载次数上限或已被创建者删除。");
        }
      } else {
        const queryPwd = urlParams.get("pwd") || "";
        if (typeof initSharePage === "function") {
          initSharePage(shareToken, queryPwd);
        }
      }
      return;
    }

    // 2. 目录分享受限模式 (?d={token})
    const dirToken = dirTokenFromLocation();
    const dirPwd = urlParams.get("pwd") || "";
    if (dirToken) {
      root.currentDirToken = dirToken;
      if (dirPwd) root.currentDirPassword = dirPwd;
      if (typeof loadFiles === "function") {
        loadFiles("", { token: dirToken });
      }
      return;
    }

    // 3. 普通全盘浏览模式
    checkAuth()
      .then(function (ok) {
        if (!ok) return;
        const initialDir = urlParams.get("dir") || root.currentDir || "/";
        if (typeof loadFiles === "function") {
          loadFiles(initialDir);
        }
      })
      .catch(function (err) {
        console.warn("[鉴权] 初始化异常", err);
        requireLogin((err && err.message) || "无法确认登录状态，请刷新页面重试");
      });
  }

  // 挂载至全局
  root.initApp = initApp;
  root.checkAuth = checkAuth;
  root.doLogin = doLogin;
  root.doLogout = doLogout;
  root.requireLogin = requireLogin;
  root.showLogin = showLogin;

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", initApp);
  } else {
    initApp();
  }

})(typeof window !== "undefined" ? window : globalThis);

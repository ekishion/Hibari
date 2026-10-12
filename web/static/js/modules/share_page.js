// 即刻轻盘单文件独立落地页模块：无原生表单 CSP 隐患、单色墨风统一设计、提取码即时验证、直接下载与在线预览。
(function (root) {
  'use strict';

  function getShareLandingContainer() {
    let container = document.getElementById("share-landing");
    if (!container) {
      container = document.createElement("div");
      container.id = "share-landing";
      container.className = "share-landing-wrapper";
      document.body.appendChild(container);
    }
    return container;
  }

  // ===== 渲染优雅失效卡片 =====
  function renderShareErrorCard(title, message) {
    const container = getShareLandingContainer();
    container.hidden = false;

    const mainApp = document.getElementById("app-main");
    if (mainApp) mainApp.hidden = true;

    container.replaceChildren();

    const card = document.createElement("div");
    card.className = "share-landing-card is-error";

    // 顶部 squircle 徽标
    const iconWrap = document.createElement("div");
    iconWrap.className = "landing-squircle-badge is-warning";
    if (typeof makeIcon === "function") {
      iconWrap.appendChild(makeIcon("link-2-off"));
    }
    card.appendChild(iconWrap);

    const h1 = document.createElement("h1");
    h1.className = "landing-title";
    h1.textContent = title || "下载链接已失效";
    card.appendChild(h1);

    const p = document.createElement("p");
    p.className = "landing-desc";
    p.textContent = message || "该分享链接可能已过期、已被创建者删除或已达到下载次数上限。";
    card.appendChild(p);

    const actions = document.createElement("div");
    actions.className = "landing-actions";

    const btnHome = document.createElement("a");
    btnHome.href = "/";
    btnHome.className = "btn btn-primary btn-block";
    btnHome.textContent = "返回网盘首页";
    actions.appendChild(btnHome);

    card.appendChild(actions);
    container.appendChild(card);
  }

  // ===== 初始化并渲染单文件落地页 =====
  function initSharePage(token, password) {
    if (!token) {
      renderShareErrorCard("参数错误", "无效的分享链接地址");
      return;
    }

    const mainApp = document.getElementById("app-main");
    if (mainApp) mainApp.hidden = true;

    const container = getShareLandingContainer();
    container.hidden = false;
    container.replaceChildren();

    // 初始加载骨架屏
    const card = document.createElement("div");
    card.className = "share-landing-card";
    const spinner = document.createElement("div");
    spinner.className = "spinner landing-loading-spinner";
    if (typeof makeIcon === "function") spinner.appendChild(makeIcon("loader"));
    card.appendChild(spinner);

    const tip = document.createElement("p");
    tip.className = "landing-desc";
    tip.textContent = "正在准备文件信息…";
    card.appendChild(tip);
    container.appendChild(card);

    // 尝试拉取短链元数据
    const rememberedPwd = password || (typeof getStoredSharePassword === "function" ? getStoredSharePassword(token) : "");
    fetchShareInfo(token, rememberedPwd);
  }

  function fetchShareInfo(token, password) {
    apiFetch("/api/share/info", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ token: token, password: password || "" })
    })
      .then(function (resp) {
        if (resp.status === 404) {
          renderShareErrorCard("下载链接已失效", "该分享链接可能已过期、已被创建者取消或已达到最大访问次数。");
          throw new Error("not_found");
        }
        if (resp.status === 401) {
          // 密码不正确，渲染带密码输入框的落地卡片
          return resp.json().then(function () {
            renderPasswordFormCard(token, "", true);
          });
        }
        if (!resp.ok) {
          return resp.text().then(function (text) {
            renderShareErrorCard("获取信息失败", parseAPIError(resp, text));
            throw new Error("api_error");
          });
        }
        return resp.json().then(function (data) {
          if (data.has_password && !data.password_valid) {
            renderPasswordFormCard(token, data.file_name, false);
          } else {
            if (password && typeof setStoredSharePassword === "function") {
              setStoredSharePassword(token, password);
            }
            renderReadyFileCard(data);
          }
        });
      })
      .catch(function (err) {
        if (err.message !== "not_found" && err.message !== "api_error") {
          renderShareErrorCard("网络异常", "无法连接至轻盘服务，请刷新重试");
        }
      });
  }

  // ===== 渲染需要提取码的落地卡片 =====
  function renderPasswordFormCard(token, fileName, isError) {
    const container = getShareLandingContainer();
    container.replaceChildren();

    const card = document.createElement("div");
    card.className = "share-landing-card";

    // 锁图标 squircle
    const badge = document.createElement("div");
    badge.className = "landing-squircle-badge";
    if (typeof makeIcon === "function") badge.appendChild(makeIcon("lock"));
    card.appendChild(badge);

    const h1 = document.createElement("h1");
    h1.className = "landing-title";
    h1.textContent = fileName || "受密码保护的文件";
    card.appendChild(h1);

    const p = document.createElement("p");
    p.className = "landing-desc";
    p.textContent = "该分享链接已设置提取码，请输入访问码提取并下载。";
    card.appendChild(p);

    const form = document.createElement("form");
    form.className = "landing-form";
    form.noValidate = true;

    const inputWrap = document.createElement("div");
    inputWrap.className = "landing-input-wrap";

    const input = document.createElement("input");
    input.type = "text";
    input.name = "pwd";
    input.className = "landing-password-input";
    input.placeholder = "请输入 6 位提取码";
    input.autocomplete = "off";
    input.maxLength = 16;
    input.autofocus = true;
    inputWrap.appendChild(input);
    form.appendChild(inputWrap);

    const errTip = document.createElement("div");
    errTip.className = "landing-error-tip";
    errTip.textContent = isError ? "提取码错误，请重新输入" : "";
    errTip.hidden = !isError;
    form.appendChild(errTip);

    const btnSubmit = document.createElement("button");
    btnSubmit.type = "submit";
    btnSubmit.className = "btn btn-primary btn-block";
    btnSubmit.textContent = "立即提取并下载";
    form.appendChild(btnSubmit);

    form.onsubmit = function (e) {
      e.preventDefault();
      const val = input.value.trim();
      if (!val) {
        input.classList.remove("is-shake");
        void input.offsetWidth;
        input.classList.add("is-shake");
        return;
      }
      setButtonLoading(btnSubmit, true, "正在验证…");
      apiFetch("/api/share/info", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ token: token, password: val })
      })
        .then(function (resp) {
          setButtonLoading(btnSubmit, false);
          if (resp.status === 401) {
            errTip.textContent = "提取码错误，请重新核对后输入";
            errTip.hidden = false;
            input.classList.remove("is-shake");
            void input.offsetWidth;
            input.classList.add("is-shake");
            input.focus();
            return;
          }
          if (!resp.ok) {
            return resp.text().then(function (text) {
              errTip.textContent = parseAPIError(resp, text);
              errTip.hidden = false;
            });
          }
          return resp.json().then(function (data) {
            if (typeof setStoredSharePassword === "function") {
              setStoredSharePassword(token, val);
            }
            renderReadyFileCard(data);
          });
        })
        .catch(function (err) {
          setButtonLoading(btnSubmit, false);
          showToast("验证失败：" + err.message, "error");
        });
    };

    card.appendChild(form);
    container.appendChild(card);
    input.focus();
  }

  // ===== 渲染文件就绪卡片 (文件名、大小、直接下载与在线预览) =====
  function renderReadyFileCard(data) {
    const container = getShareLandingContainer();
    container.replaceChildren();

    const card = document.createElement("div");
    card.className = "share-landing-card";

    // 文件类型图标徽标
    const meta = typeof fileIconMeta === "function" ? fileIconMeta(data.file_name, false) : { name: "file" };
    const badge = document.createElement("div");
    badge.className = "landing-squircle-badge";
    if (typeof makeIcon === "function") badge.appendChild(makeIcon(meta.name));
    card.appendChild(badge);

    const title = document.createElement("h1");
    title.className = "landing-title";
    title.textContent = data.file_name || "文件下载";
    card.appendChild(title);

    // 元数据标签组 (文件大小、过期时间)
    const tags = document.createElement("div");
    tags.className = "landing-meta-tags";

    if (data.size && typeof formatSize === "function") {
      const sizeTag = document.createElement("span");
      sizeTag.className = "landing-meta-tag";
      sizeTag.textContent = formatSize(data.size);
      tags.appendChild(sizeTag);
    }

    if (data.has_password) {
      const pwdTag = document.createElement("span");
      pwdTag.className = "landing-meta-tag is-badge";
      pwdTag.textContent = "提取码已验证";
      tags.appendChild(pwdTag);
    }

    if (data.max_uses > 0) {
      const usesTag = document.createElement("span");
      usesTag.className = "landing-meta-tag";
      usesTag.textContent = "限次下载";
      tags.appendChild(usesTag);
    }

    card.appendChild(tags);

    // 操作按钮区
    const actions = document.createElement("div");
    actions.className = "landing-actions";

    // “立即下载”主按钮：采用 JS a.click 触发直接下载，绝不触犯 CSP form-action 策略
    const btnDownload = document.createElement("button");
    btnDownload.type = "button";
    btnDownload.className = "btn btn-primary btn-block btn-lg";
    if (typeof makeIcon === "function") {
      const iconSpan = document.createElement("span");
      iconSpan.className = "btn-icon";
      iconSpan.appendChild(makeIcon("download"));
      btnDownload.appendChild(iconSpan);
    }
    const labelSpan = document.createElement("span");
    labelSpan.className = "btn-label";
    labelSpan.textContent = "立即下载";
    btnDownload.appendChild(labelSpan);

    btnDownload.onclick = function () {
      if (typeof setButtonLoading === "function") {
        setButtonLoading(btnDownload, true, "正在启动下载…");
      }
      setTimeout(function () {
        if (typeof setButtonLoading === "function") {
          setButtonLoading(btnDownload, false);
        }
      }, 3000);
      triggerDownloadNavigation(data.download_url);
    };
    actions.appendChild(btnDownload);

    // “在线预览”按钮（若格式支持视频、音频、图片或文本预览）
    if (typeof previewKind === "function" && previewKind(data.file_name)) {
      const btnPreview = document.createElement("button");
      btnPreview.type = "button";
      btnPreview.className = "btn btn-secondary btn-block";
      if (typeof makeIcon === "function") {
        const pIcon = document.createElement("span");
        pIcon.className = "btn-icon";
        pIcon.appendChild(makeIcon("eye"));
        btnPreview.appendChild(pIcon);
      }
      const pLabel = document.createElement("span");
      pLabel.className = "btn-label";
      pLabel.textContent = "在线预览";
      btnPreview.appendChild(pLabel);

      btnPreview.onclick = function () {
        if (typeof openPreview === "function") {
          openPreview(data.file_path, data.file_name);
        }
      };
      actions.appendChild(btnPreview);
    }

    const backHome = document.createElement("a");
    backHome.href = "/";
    backHome.className = "btn btn-ghost btn-block";
    backHome.textContent = "进入网盘首页";
    actions.appendChild(backHome);

    card.appendChild(actions);
    container.appendChild(card);
  }

  // 挂载至全局
  root.initSharePage = initSharePage;
  root.renderShareErrorCard = renderShareErrorCard;

})(typeof window !== "undefined" ? window : globalThis);

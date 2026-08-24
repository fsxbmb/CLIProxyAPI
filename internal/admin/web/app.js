"use strict";

const state = {
  managementKey: localStorage.getItem("cliproxy-management-key") || "",
  apiKey: "",
  apiBase: "http://127.0.0.1:8317/v1",
  accounts: [],
  relays: [],
  models: [],
  quotas: {},
  balanceConfigs: (() => { try { return JSON.parse(localStorage.getItem("cliproxy-balance-configs") || "{}"); } catch { return {}; } })(),
  quotaRefreshing: false,
  quotaTimer: null,
  connected: false,
  keyVisible: false,
};

const $ = (selector) => document.querySelector(selector);
const $$ = (selector) => [...document.querySelectorAll(selector)];

function notify(message, error = false) {
  const toast = $("#toast");
  toast.textContent = message;
  toast.className = `toast show${error ? " error" : ""}`;
  clearTimeout(notify.timer);
  notify.timer = setTimeout(() => { toast.className = "toast"; }, 3200);
}

async function decodeResponse(response) {
  const text = await response.text();
  let data = {};
  if (text) {
    try { data = JSON.parse(text); } catch { data = { text }; }
  }
  if (!response.ok) {
    const message = data.error || data.message || data.details || `${response.status} ${response.statusText}`;
    throw new Error(message);
  }
  return data;
}

async function management(path, options = {}) {
  if (!state.managementKey) throw new Error("请先输入 Management key");
  const headers = new Headers(options.headers || {});
  headers.set("Authorization", `Bearer ${state.managementKey}`);
  let body = options.body;
  if (body && typeof body !== "string") {
    headers.set("Content-Type", "application/json");
    body = JSON.stringify(body);
  }
  const response = await fetch(`/api/${path}`, { ...options, headers, body });
  return decodeResponse(response);
}

async function proxyAPI(path, options = {}) {
  if (!state.apiKey) throw new Error("尚未读取本机 API key");
  const headers = new Headers(options.headers || {});
  headers.set("Authorization", `Bearer ${state.apiKey}`);
  let body = options.body;
  if (body && typeof body !== "string") {
    headers.set("Content-Type", "application/json");
    body = JSON.stringify(body);
  }
  const response = await fetch(`/proxy/${path}`, { ...options, headers, body });
  return decodeResponse(response);
}

function setConnection(connected, message = "") {
  state.connected = connected;
  $("#state-dot").className = `dot${connected ? " online" : message ? " error" : ""}`;
  $("#state-text").textContent = connected ? "本机服务已连接" : (message || "等待连接");
  $("#connect-button").textContent = connected ? "已连接" : "连接";
}

async function connect() {
  const input = $("#management-key").value.trim();
  if (input) state.managementKey = input;
  if (!state.managementKey) {
    notify("请输入 Management key", true);
    return;
  }
  $("#connect-button").disabled = true;
  try {
    await management("config");
    localStorage.setItem("cliproxy-management-key", state.managementKey);
    setConnection(true);
    await Promise.allSettled([loadKeys(), loadAccounts(), loadRelays(), loadModels(), loadDebug()]);
    notify("已连接本机管理接口");
  } catch (error) {
    localStorage.removeItem("cliproxy-management-key");
    setConnection(false, "连接失败");
    notify(`连接失败：${error.message}`, true);
  } finally {
    $("#connect-button").disabled = false;
  }
}

async function loadMeta() {
  try {
    const meta = await decodeResponse(await fetch("/meta"));
    state.apiBase = meta.api_base || state.apiBase;
    $("#api-base").textContent = state.apiBase.replace(/^https?:\/\//, "");
    $("#endpoint-value").textContent = state.apiBase;
    updateShellExample();
  } catch (error) {
    notify(`读取本机信息失败：${error.message}`, true);
  }
}

async function loadKeys() {
  const data = await management("api-keys");
  const keys = data["api-keys"] || data.items || [];
  state.apiKey = keys[0] || "";
  state.keyVisible = false;
  renderKey();
  $("#reveal-key").disabled = !state.apiKey;
  $("#copy-key").disabled = !state.apiKey;
  updateShellExample();
}

function renderKey() {
  const value = $("#api-key-value");
  if (!state.apiKey) value.textContent = state.connected ? "未配置" : "连接管理接口后显示";
  else if (state.keyVisible) value.textContent = state.apiKey;
  else value.textContent = `${state.apiKey.slice(0, 10)}${"•".repeat(18)}${state.apiKey.slice(-5)}`;
  $("#reveal-key").textContent = state.keyVisible ? "隐藏" : "显示";
}

function updateShellExample() {
  const key = state.apiKey || "你的本机 API key";
  $("#shell-example").textContent = `export OPENAI_BASE_URL="${state.apiBase}"\nexport OPENAI_API_KEY="${key}"`;
}

async function loadAccounts() {
  const data = await management("auth-files");
  state.accounts = Array.isArray(data.files) ? data.files : [];
  renderAccounts();
  if (state.connected) void loadQuotas({ silent: true });
}

const QUOTA_PROVIDERS = new Set(["codex", "antigravity", "gemini", "xai", "relay"]);

function accountProvider(account) {
  return String(account.provider || account.type || "unknown").toLowerCase();
}

function accountKey(account) {
  return account.name || account.id || account.auth_index || account.authIndex;
}

function authIndex(account) {
  return account.auth_index || account.authIndex || "";
}

function accountIdentity(account) {
  return account.email || account.account || account.label || account.name || "未知账号";
}

async function vendorCall(account, method, url, header, data = "") {
  const index = authIndex(account);
  if (!index) throw new Error("凭据缺少 auth_index，请重新登录");
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), 20000);
  let result;
  try {
    result = await management("api-call", {
      method: "POST",
      signal: controller.signal,
      body: { auth_index: index, method, url, header, data },
    });
  } catch (error) {
    if (error.name === "AbortError") throw new Error("官方额度接口超过 20 秒未响应");
    throw error;
  } finally {
    clearTimeout(timeout);
  }
  let body = result.body;
  if (typeof body === "string" && body.trim()) {
    try { body = JSON.parse(body); } catch { /* Keep the vendor error text. */ }
  }
  const status = Number(result.status_code || 0);
  if (status < 200 || status >= 300) {
    const detail = body?.error?.message || body?.error || body?.message || (typeof body === "string" ? body : "");
    throw new Error(`官方接口 HTTP ${status}${detail ? `：${String(detail).slice(0, 160)}` : ""}`);
  }
  return { body, header: result.header || {} };
}

function numberValue(value) {
  const number = Number(value);
  return Number.isFinite(number) ? number : null;
}

function resetFromWindow(window) {
  const epoch = numberValue(window?.reset_at ?? window?.resetAt);
  if (epoch !== null) return new Date(epoch * 1000);
  const after = numberValue(window?.reset_after_seconds ?? window?.resetAfterSeconds);
  return after === null ? null : new Date(Date.now() + after * 1000);
}

function parseCodexQuota(payload) {
  const limits = [
    ["Codex", payload?.rate_limit ?? payload?.rateLimit],
    ["代码审查", payload?.code_review_rate_limit ?? payload?.codeReviewRateLimit],
  ];
  const rows = [];
  for (const [group, limit] of limits) {
    if (!limit) continue;
    const windows = [limit.primary_window ?? limit.primaryWindow, limit.secondary_window ?? limit.secondaryWindow].filter(Boolean);
    for (const window of windows) {
      const seconds = numberValue(window.limit_window_seconds ?? window.limitWindowSeconds);
      const label = seconds === 18000 ? "5 小时" : seconds === 604800 ? "每周" : seconds && seconds >= 2419200 ? "每月" : group;
      const used = numberValue(window.used_percent ?? window.usedPercent);
      rows.push({ label: group === "Codex" ? label : `${group} · ${label}`, remaining: used === null ? null : 100 - used, reset: resetFromWindow(window) });
    }
  }
  if (!rows.length) throw new Error("官方接口未返回额度窗口");
  return { plan: payload?.plan_type || payload?.planType || "ChatGPT", rows };
}

function parseGeminiQuota(payload) {
  const rows = [];
  for (const group of payload?.groups || []) {
    for (const bucket of group?.buckets || []) {
      const fraction = numberValue(bucket.remainingFraction ?? bucket.remaining_fraction);
      if (fraction === null) continue;
      const window = String(bucket.window || "").toLowerCase();
      const period = window === "5h" ? "5 小时" : window === "weekly" ? "每周" : (bucket.displayName || bucket.display_name || window);
      const rawGroup = group.displayName || group.display_name || "Gemini";
      const groupName = /claude|gpt/i.test(rawGroup) ? "Antigravity 第三方模型" : /gemini/i.test(rawGroup) ? "Gemini 模型" : rawGroup;
      rows.push({ label: `${groupName} · ${period}`, remaining: fraction * 100, reset: bucket.resetTime || bucket.reset_time ? new Date(bucket.resetTime || bucket.reset_time) : null });
    }
  }
  if (!rows.length) throw new Error("官方接口未返回额度分组");
  return { plan: "Antigravity", rows };
}

function xaiCent(value) {
  return numberValue(value && typeof value === "object" ? value.val : value);
}

function parseXaiQuota(payload) {
  const config = payload?.config;
  if (!config) throw new Error("官方接口未返回 billing 配置");
  const usage = numberValue(config.creditUsagePercent ?? config.credit_usage_percent);
  const limit = xaiCent(config.monthlyLimit ?? config.monthly_limit);
  const used = xaiCent(config.used);
  const productUsage = config.productUsage ?? config.product_usage ?? [];
  const rows = [];
  if (usage !== null) rows.push({ label: "每周额度", remaining: 100 - usage, reset: null });
  for (const item of productUsage) {
    const percent = numberValue(item.usagePercent ?? item.usage_percent);
    if (percent !== null) rows.push({ label: item.product || "模型额度", remaining: 100 - percent, reset: null });
  }
  if (limit !== null && limit > 0 && used !== null) rows.push({ label: "每月包含额度", remaining: 100 - (Math.min(used, limit) / limit * 100), reset: null });
  const period = config.currentPeriod ?? config.current_period ?? {};
  const resetRaw = period.end || config.billingPeriodEnd || config.billing_period_end;
  const reset = resetRaw ? new Date(resetRaw) : null;
  rows.forEach((row) => { row.reset ||= reset; });
  return { plan: "Grok", rows, note: rows.length ? "" : "账号可用；Grok 当前未向此 OAuth 凭据提供额度总量。", reset };
}

function resolvePath(obj, path) {
  if (!path) return obj;
  const parts = String(path).split(/[.\[\]]+/).filter(Boolean);
  let current = obj;
  for (const part of parts) {
    if (current === null || current === undefined) return null;
    current = current[part];
  }
  return current;
}

// 已知官方中转的余额查询预设：保存中转后自动识别，无需手动配置。
const BALANCE_PRESETS = [
  {
    match: /api\.deepseek\.com/i,
    url: (base) => base.replace(/\/v1\/?$/, "").replace(/\/+$/, "") + "/user/balance",
    path: "balance_infos[0].total_balance",
    currencyPath: "balance_infos[0].currency",
    defaultCurrency: "CNY",
  },
  {
    match: /openrouter\.ai/i,
    url: (base) => base.replace(/\/+$/, "") + "/credits",
    path: "data.credits.total",
    currencyPath: "data.credits.currency",
    defaultCurrency: "USD",
  },
  {
    match: /api\.moonshot\.ai/i,
    url: (base) => base.replace(/\/v1\/?$/, "").replace(/\/+$/, "") + "/v1/users/me/balance",
    path: "data.available_balance",
    currencyPath: "data.currency",
    defaultCurrency: "CNY",
  },
  {
    match: /dashscope\.aliyuncs\.com/i,
    url: (base) => base.replace(/\/compatible-mode\/?$/, "").replace(/\/+$/, "") + "/api/v1/checkout",
    path: "data.available_balance",
    currencyPath: null,
    defaultCurrency: "CNY",
  },
];

function balancePresetFor(relay) {
  const base = String(relay["base-url"] || "");
  for (const preset of BALANCE_PRESETS) {
    if (preset.match.test(base)) return preset;
  }
  return null;
}

function balanceConfigFor(relay) {
  const manual = state.balanceConfigs[relay.name];
  if (manual) return manual;
  const preset = balancePresetFor(relay);
  if (!preset) return null;
  return {
    url: preset.url(relay["base-url"]),
    path: preset.path,
    currency: preset.defaultCurrency,
    currencyPath: preset.currencyPath,
  };
}

function parseRelayBalance(payload, config) {
  const value = resolvePath(payload, config.path);
  if (value === null || value === undefined || value === "") throw new Error("余额 JSON 路径未匹配到数据");
  const number = Number(value);
  if (!Number.isFinite(number)) throw new Error(`余额值无法解析：${value}`);
  let currency = config.currency || "";
  if (!currency && config.currencyPath) {
    const cur = resolvePath(payload, config.currencyPath);
    if (cur) currency = String(cur);
  }
  return { plan: "余额", rows: [{ label: "账户余额", remaining: null, reset: null, balance: `${number.toFixed(2)} ${currency}`.trim() }], note: "" };
}

async function fetchAccountQuota(account) {
  const provider = accountProvider(account);
  if (provider === "relay") {
    const result = await vendorCall(account, "GET", account.balance.url, {
      Authorization: "Bearer $TOKEN$", "Content-Type": "application/json",
    });
    return parseRelayBalance(result.body, account.balance);
  }
  if (provider === "codex") {
    const result = await vendorCall(account, "GET", "https://chatgpt.com/backend-api/wham/usage", {
      Authorization: "Bearer $TOKEN$", "Content-Type": "application/json", "User-Agent": "codex_cli_rs/0.76.0",
    });
    return parseCodexQuota(result.body);
  }
  if (provider === "antigravity" || provider === "gemini") {
    const project = account.project_id || account.projectId;
    if (!project) throw new Error("凭据缺少 project_id，请重新登录 Gemini");
    const urls = [
      "https://daily-cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary",
      "https://cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary",
    ];
    let lastError;
    for (const url of urls) {
      try {
        const result = await vendorCall(account, "POST", url, {
          Authorization: "Bearer $TOKEN$", "Content-Type": "application/json", "User-Agent": "antigravity/cli/1.0.13 (aidev_client; os_type=darwin; arch=arm64)",
        }, JSON.stringify({ project }));
        return parseGeminiQuota(result.body);
      } catch (error) { lastError = error; }
    }
    throw lastError;
  }
  if (provider === "xai") {
    const headers = {
      Authorization: "Bearer $TOKEN$", "x-xai-token-auth": "xai-grok-cli", "x-grok-client-version": "0.2.91", Accept: "*/*", "User-Agent": "grok-pager/0.2.91 grok-shell/0.2.91 (macos; aarch64)",
    };
    const [weekly, monthly] = await Promise.allSettled([
      vendorCall(account, "GET", "https://cli-chat-proxy.grok.com/v1/billing?format=credits", headers),
      vendorCall(account, "GET", "https://cli-chat-proxy.grok.com/v1/billing", headers),
    ]);
    const result = weekly.status === "fulfilled" ? weekly.value : monthly.status === "fulfilled" ? monthly.value : null;
    if (!result) throw weekly.reason || monthly.reason || new Error("Grok 额度查询失败");
    return parseXaiQuota(result.body);
  }
  throw new Error("该供应商不支持额度预览");
}

function formatReset(value) {
  if (!value || Number.isNaN(value.getTime())) return "";
  return `重置：${value.toLocaleString("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" })}`;
}

function appendQuotaContent(cell, account) {
  const provider = accountProvider(account);
  if (!QUOTA_PROVIDERS.has(provider)) {
    const unsupported = document.createElement("span"); unsupported.className = "quota-note"; unsupported.textContent = "暂不支持"; cell.append(unsupported); return;
  }
  const quota = state.quotas[accountKey(account)];
  if (account.disabled || !quota || quota.status === "loading" || quota.status === "error") {
    const note = document.createElement("span");
    note.className = quota?.status === "error" ? "quota-error" : "quota-note";
    note.textContent = account.disabled ? "账号已停用" : quota?.status === "loading" ? "正在自动读取…" : quota?.status === "error" ? quota.error : "等待自动刷新";
    cell.append(note); return;
  }
  const plan = document.createElement("span"); plan.className = "quota-status success"; plan.textContent = quota.data.plan || "已更新"; cell.append(plan);
  for (const row of quota.data.rows || []) {
    if (row.balance !== undefined && row.balance !== null) {
      const line = document.createElement("div"); line.className = "quota-line";
      const label = document.createElement("span"); label.textContent = row.label;
      const value = document.createElement("strong"); value.textContent = row.balance;
      line.append(label, value);
      const track = document.createElement("div"); track.className = "quota-track";
      const fill = document.createElement("span"); fill.style.width = "100%";
      track.append(fill);
      cell.append(line, track);
      continue;
    }
    const meter = document.createElement("div"); meter.className = "quota-meter";
    const line = document.createElement("div"); line.className = "quota-line";
    const label = document.createElement("span"); label.textContent = row.label;
    const remaining = Math.max(0, Math.min(100, row.remaining));
    const value = document.createElement("strong"); value.textContent = `${remaining.toFixed(remaining >= 10 ? 0 : 1)}%`;
    line.append(label, value);
    const track = document.createElement("div"); track.className = "quota-track";
    const fill = document.createElement("span"); fill.style.width = `${remaining}%`; if (remaining < 20) fill.className = "low"; track.append(fill);
    const reset = document.createElement("small"); reset.textContent = formatReset(row.reset);
    meter.append(line, track, reset); cell.append(meter);
  }
  if (quota.data.note) { const note = document.createElement("p"); note.className = "quota-note"; note.textContent = quota.data.note; cell.append(note); }
  if (!(quota.data.rows || []).length && quota.data.reset) { const reset = document.createElement("p"); reset.className = "quota-note"; reset.textContent = formatReset(quota.data.reset); cell.append(reset); }
}

function renderQuotas() {
  renderAccounts();
}

async function loadQuotas({ silent = false } = {}) {
  if (state.quotaRefreshing) return;
  const button = $("#refresh-quotas");
  const targets = [];
  state.accounts.forEach((account) => {
    if (QUOTA_PROVIDERS.has(accountProvider(account)) && !account.disabled) targets.push(account);
  });
  state.relays.forEach((relay) => {
    const balance = balanceConfigFor(relay);
    if (!balance || relay.disabled) return;
    const keyEntry = (relay["api-key-entries"] || [])[0];
    const authIndex = keyEntry && (keyEntry["auth-index"] || keyEntry["auth_index"]);
    if (!authIndex) return;
    targets.push({ name: relay.name, provider: "relay", email: relay.name, auth_index: authIndex, balance });
  });
  if (!targets.length) { if (!silent) notify("没有可查询的账号或中转", true); return; }
  state.quotaRefreshing = true;
  button.disabled = true;
  targets.forEach((account) => state.quotas[accountKey(account)] = { status: "loading" });
  renderQuotas();
  try {
    const results = await Promise.allSettled(targets.map(async (account) => {
      try {
        const data = await fetchAccountQuota(account);
        state.quotas[accountKey(account)] = { status: "success", data };
      } catch (error) {
        state.quotas[accountKey(account)] = { status: "error", error: error.message || "额度查询失败" };
      }
      renderQuotas();
    }));
    const failed = results.filter((result) => result.status === "rejected").length + targets.filter((account) => state.quotas[accountKey(account)]?.status === "error").length;
    $("#quota-updated").textContent = `更新于 ${new Date().toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit" })}`;
    if (!silent) notify(failed ? `额度刷新完成，${failed} 个账号失败` : "所有账号与中转额度已刷新", failed > 0);
  } finally {
    button.disabled = false;
    state.quotaRefreshing = false;
    clearTimeout(state.quotaTimer);
    state.quotaTimer = setTimeout(() => loadQuotas({ silent: true }), 5 * 60 * 1000);
  }
}

function providerName(account) {
  const provider = String(account.provider || account.type || "unknown").toLowerCase();
  return ({ codex: "ChatGPT Plus", claude: "Claude", anthropic: "Claude", antigravity: "Gemini", gemini: "Gemini", xai: "Grok", kimi: "Kimi" })[provider] || provider;
}

function renderAccounts() {
  const table = $("#account-table");
  table.replaceChildren();
  if (!state.accounts.length) {
    const row = document.createElement("tr");
    const cell = document.createElement("td");
    cell.colSpan = 6;
    cell.className = "empty";
    cell.textContent = "还没有 OAuth 账号";
    row.append(cell);
    table.append(row);
  }
  state.accounts.forEach((account) => {
    const row = document.createElement("tr");
    const provider = document.createElement("td");
    provider.textContent = providerName(account);
    const identity = document.createElement("td");
    identity.textContent = account.email || account.account || account.label || account.name || "—";
    const status = document.createElement("td");
    const badge = document.createElement("span");
    badge.className = `badge${account.disabled ? " disabled" : ""}`;
    badge.textContent = account.disabled ? "已停用" : (account.status || "可用");
    status.append(badge);
    const requests = document.createElement("td");
    requests.textContent = `${account.success || 0} 成功 / ${account.failed || 0} 失败`;
    const quota = document.createElement("td");
    quota.className = "account-quota";
    appendQuotaContent(quota, account);
    const actions = document.createElement("td");
    actions.className = "row-actions";
    const toggle = document.createElement("button");
    toggle.textContent = account.disabled ? "启用" : "停用";
    toggle.addEventListener("click", () => toggleAccount(account));
    const remove = document.createElement("button");
    remove.className = "danger";
    remove.textContent = "删除";
    remove.addEventListener("click", () => deleteAccount(account));
    actions.append(toggle, remove);
    row.append(provider, identity, status, requests, quota, actions);
    table.append(row);
  });
  state.relays.forEach((relay) => {
    if (!balanceConfigFor(relay)) return;
    const keyEntry = (relay["api-key-entries"] || [])[0];
    const authIndex = keyEntry && (keyEntry["auth-index"] || keyEntry["auth_index"]);
    const row = document.createElement("tr");
    row.className = "relay-balance-row";
    const provider = document.createElement("td"); provider.textContent = "中转余额";
    const identity = document.createElement("td"); identity.textContent = relay.name;
    const status = document.createElement("td");
    const badge = document.createElement("span");
    badge.className = `badge${relay.disabled ? " disabled" : ""}`;
    badge.textContent = relay.disabled ? "已停用" : "可用";
    status.append(badge);
    const requests = document.createElement("td"); requests.textContent = "—";
    const quota = document.createElement("td"); quota.className = "account-quota";
    appendQuotaContent(quota, { name: relay.name, provider: "relay", email: relay.name, disabled: relay.disabled, auth_index: authIndex, balance: balanceConfigFor(relay) });
    const actions = document.createElement("td");
    row.append(provider, identity, status, requests, quota, actions);
    table.append(row);
  });
  const codex = state.accounts.filter((item) => String(item.provider || item.type).toLowerCase() === "codex").length;
  $("#account-count").textContent = String(state.accounts.length);
  $("#codex-count").textContent = `${codex} / 2`;
}

async function toggleAccount(account) {
  try {
    await management("auth-files/status", { method: "PATCH", body: { name: account.name || account.id, disabled: !account.disabled } });
    await loadAccounts();
    notify(account.disabled ? "账号已启用" : "账号已停用");
  } catch (error) { notify(`更新失败：${error.message}`, true); }
}

async function deleteAccount(account) {
  const name = account.name || account.id;
  if (!confirm(`删除本机凭据 ${name}？此操作不会删除厂商账号。`)) return;
  try {
    await management(`auth-files?name=${encodeURIComponent(name)}`, { method: "DELETE" });
    await loadAccounts();
    await loadModels();
    notify("本机凭据已删除");
  } catch (error) { notify(`删除失败：${error.message}`, true); }
}

async function startOAuth(button) {
  if (!state.connected) {
    notify("请先连接管理接口", true);
    return;
  }
  button.disabled = true;
  const provider = button.dataset.provider;
  try {
    const data = await management(`${button.dataset.endpoint}?is_webui=true`);
    if (data.url) window.open(data.url, "_blank", "noopener,noreferrer");
    const deviceNote = data.user_code ? ` 设备码：${data.user_code}` : "";
    $("#oauth-progress").textContent = `${provider} 登录进行中…${deviceNote}`;
    notify(`${provider} 登录窗口已打开${deviceNote}`);
    if (data.state) pollOAuth(data.state, provider);
  } catch (error) {
    notify(`${provider} 登录启动失败：${error.message}`, true);
  } finally {
    button.disabled = false;
  }
}

async function pollOAuth(oauthState, provider) {
  const deadline = Date.now() + 5 * 60 * 1000;
  while (Date.now() < deadline) {
    await new Promise((resolve) => setTimeout(resolve, 1500));
    try {
      const data = await management(`get-auth-status?state=${encodeURIComponent(oauthState)}`);
      const status = String(data.status || "").toLowerCase();
      if (status === "ok" || status === "success" || status === "completed") {
        $("#oauth-progress").textContent = `${provider} 登录成功`;
        await loadAccounts();
        await loadModels();
        notify(`${provider} 登录成功`);
        return;
      }
      if (status === "error" || status === "failed") throw new Error(data.error || data.message || "OAuth 登录失败");
    } catch (error) {
      $("#oauth-progress").textContent = `${provider} 登录失败`;
      notify(error.message, true);
      return;
    }
  }
  $("#oauth-progress").textContent = `${provider} 登录已超时`;
}

async function loadRelays() {
  const data = await management("openai-compatibility");
  state.relays = data["openai-compatibility"] || data.items || [];
  renderRelays();
  // 确保中转余额查询在中转列表就绪后再执行（页面加载时可能与 loadQuotas 竞争）
  if (state.connected) void loadQuotas({ silent: true });
}

function renderRelays() {
  const list = $("#relay-list");
  list.replaceChildren();
  if (!state.relays.length) {
    const empty = document.createElement("p");
    empty.className = "empty";
    empty.textContent = "还没有配置第三方中转";
    list.append(empty);
  }
  state.relays.forEach((relay, index) => {
    const item = document.createElement("div");
    item.className = "relay-item";
    const text = document.createElement("div");
    const name = document.createElement("strong");
    name.textContent = relay.name || `中转 ${index + 1}`;
    const detail = document.createElement("small");
    const models = Array.isArray(relay.models) ? relay.models.length : 0;
    const balance = balanceConfigFor(relay);
    detail.textContent = `${relay["base-url"] || "—"} · ${models} 个模型${relay.prefix ? ` · 前缀 ${relay.prefix}` : ""}${balance ? ` · 已配置余额查询` : ""}`;
    text.append(name, detail);
    const remove = document.createElement("button");
    remove.className = "danger";
    remove.textContent = "删除";
    remove.addEventListener("click", () => deleteRelay(index));
    item.append(text, remove);
    list.append(item);
  });
  $("#relay-count").textContent = String(state.relays.length);
}

function parseModels(raw) {
  return raw.split(/\n|,/).map((line) => line.trim()).filter(Boolean).map((line) => {
    const parts = line.split("=").map((part) => part.trim());
    return { name: parts[0], alias: parts[1] || parts[0] };
  });
}

async function saveRelay(event) {
  event.preventDefault();
  if (!state.connected) return notify("请先连接管理接口", true);
  const name = $("#relay-name").value.trim();
  const entry = {
    name,
    prefix: $("#relay-prefix").value.trim(),
    "base-url": $("#relay-url").value.trim().replace(/\/$/, ""),
    "api-key-entries": [{ "api-key": $("#relay-key").value.trim() }],
    models: parseModels($("#relay-models").value),
  };
  if (!name || !entry["base-url"] || !entry["api-key-entries"][0]["api-key"] || !entry.models.length) {
    return notify("请完整填写名称、URL、API key 和模型", true);
  }
  // 余额查询配置（非敏感，存浏览器本地）
  const balanceUrl = $("#relay-balance-url").value.trim();
  if (balanceUrl) {
    state.balanceConfigs[name] = {
      url: balanceUrl,
      path: $("#relay-balance-path").value.trim() || null,
      currency: $("#relay-balance-currency").value.trim() || null,
    };
  } else {
    delete state.balanceConfigs[name];
  }
  try { localStorage.setItem("cliproxy-balance-configs", JSON.stringify(state.balanceConfigs)); } catch { /* ignore */ }
  const next = state.relays.filter((relay) => String(relay.name).toLowerCase() !== name.toLowerCase());
  next.push(entry);
  try {
    await management("openai-compatibility", { method: "PUT", body: next });
    $("#relay-form").reset();
    await loadRelays();
    await loadModels();
    if (balanceUrl) { loadQuotas({ silent: true }); }
    notify("中转配置已保存并热重载");
  } catch (error) { notify(`保存失败：${error.message}`, true); }
}

async function deleteRelay(index) {
  const relay = state.relays[index];
  if (!confirm(`删除中转 ${relay.name || index + 1}？`)) return;
  delete state.balanceConfigs[relay.name];
  try { localStorage.setItem("cliproxy-balance-configs", JSON.stringify(state.balanceConfigs)); } catch { /* ignore */ }
  const next = state.relays.filter((_, itemIndex) => itemIndex !== index);
  try {
    await management("openai-compatibility", { method: "PUT", body: next });
    await loadRelays();
    await loadModels();
    notify("中转已删除");
  } catch (error) { notify(`删除失败：${error.message}`, true); }
}

function fillQwenPreset() {
  $("#relay-name").value = "Qwen";
  $("#relay-prefix").value = "qwen";
  $("#relay-url").value = "https://dashscope.aliyuncs.com/compatible-mode/v1";
  $("#relay-models").value = "qwen3-coder-plus=qwen-coder\nqwen-plus=qwen-plus";
  $("#relay-key").focus();
  notify("已填入 DashScope 兼容接口，请补充 API key 并核对模型名");
}

function vendorOf(modelId) {
  const raw = String(modelId || "");
  const lower = raw.toLowerCase();
  const slash = raw.includes("/") ? lower.split("/")[0] : "";
  const groups = [
    [/^(gpt|o[1-9]|codex)/, "ChatGPT / Codex"],
    [/^grok/, "Grok"],
    [/^gemini/, "Gemini"],
    [/^claude/, "Claude"],
    [/^deepseek/, "DeepSeek"],
    [/^qwen/, "Qwen"],
    [/^kimi/, "Kimi"],
    [/^glm/, "GLM"],
    [/^mistral/, "Mistral"],
  ];
  const probe = slash || lower;
  for (const [pattern, label] of groups) {
    if (pattern.test(probe)) return label;
  }
  return "其他模型";
}

async function loadModels() {
  if (!state.apiKey) await loadKeys();
  const data = await proxyAPI("v1/models");
  state.models = Array.isArray(data.data) ? data.data : (data.models || []);
  const select = $("#test-model");
  select.replaceChildren();
  const placeholder = document.createElement("option");
  placeholder.value = "";
  placeholder.textContent = state.models.length ? "选择模型" : "暂无可用模型";
  select.append(placeholder);
  const grouped = new Map();
  state.models.forEach((model) => {
    const vendor = vendorOf(model.id || model.name);
    if (!grouped.has(vendor)) grouped.set(vendor, []);
    grouped.get(vendor).push(model);
  });
  [...grouped.entries()].sort((a, b) => a[0].localeCompare(b[0])).forEach(([vendor, models]) => {
    const group = document.createElement("optgroup");
    group.label = vendor;
    models.forEach((model) => {
      const option = document.createElement("option");
      option.value = model.id || model.name || model;
      option.textContent = model.display_name || model.id || model.name || model;
      group.append(option);
    });
    select.append(group);
  });
  $("#model-count").textContent = String(state.models.length);
}

async function runModelTest() {
  const model = $("#test-model").value;
  const prompt = $("#test-prompt").value.trim();
  if (!model) return notify("请先选择一个模型", true);
  $("#run-test").disabled = true;
  let elapsed = 0;
  $("#test-output").textContent = "请求中… 0 秒";
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), 45000);
  const ticker = setInterval(() => {
    elapsed += 1;
    $("#test-output").textContent = `请求中… ${elapsed} 秒`;
  }, 1000);
  try {
    const data = await proxyAPI("v1/chat/completions", {
      method: "POST",
      signal: controller.signal,
      body: { model, messages: [{ role: "user", content: prompt }], stream: false, max_tokens: 80 },
    });
    const content = data.choices?.[0]?.message?.content ?? data.output_text ?? data;
    $("#test-output").textContent = typeof content === "string" ? content : JSON.stringify(content, null, 2);
    notify("模型请求成功");
  } catch (error) {
    const message = error.name === "AbortError" ? "请求超过 45 秒，已取消" : error.message;
    $("#test-output").textContent = `失败：${message}`;
    notify(`测试失败：${message}`, true);
  } finally {
    clearTimeout(timeout);
    clearInterval(ticker);
    $("#run-test").disabled = false;
  }
}

async function loadDebug() {
  const data = await management("debug");
  $("#debug-toggle").checked = Boolean(data.debug);
}

async function setDebug() {
  try {
    await management("debug", { method: "PUT", body: { value: $("#debug-toggle").checked } });
    notify($("#debug-toggle").checked ? "Debug 日志已开启" : "Debug 日志已关闭");
  } catch (error) { notify(`设置失败：${error.message}`, true); }
}

async function diagnostics() {
  const output = $("#diagnostic-output");
  output.textContent = "检查中…";
  try {
    const [health, config] = await Promise.all([decodeResponse(await fetch("/health")), management("config")]);
    await Promise.allSettled([loadAccounts(), loadRelays(), loadModels(), loadDebug()]);
    const safe = {
      web_ui: health.ok ? "ok" : "error",
      proxy_api: "ok",
      host: "127.0.0.1",
      oauth_accounts: state.accounts.length,
      chatgpt_plus_accounts: state.accounts.filter((item) => String(item.provider || item.type).toLowerCase() === "codex").length,
      compatible_relays: state.relays.length,
      visible_models: state.models.length,
      debug: $("#debug-toggle").checked,
      usage_statistics: config["usage-statistics-enabled"] ?? false,
      request_log: config["request-log"] ?? false,
    };
    output.textContent = JSON.stringify(safe, null, 2);
    notify("诊断完成");
  } catch (error) {
    output.textContent = `诊断失败：${error.message}`;
    notify(error.message, true);
  }
}

async function copyText(text, label = "已复制") {
  try { await navigator.clipboard.writeText(text); notify(label); }
  catch { notify("复制失败，请手动选择文本", true); }
}

function bindEvents() {
  $$(".tab").forEach((button) => button.addEventListener("click", () => {
    $$(".tab").forEach((item) => item.classList.toggle("active", item === button));
    $$(".panel").forEach((panel) => panel.classList.toggle("active", panel.id === button.dataset.tab));
  }));
  $("#connect-button").addEventListener("click", connect);
  $("#management-key").addEventListener("keydown", (event) => { if (event.key === "Enter") connect(); });
  $("#reveal-key").addEventListener("click", () => { state.keyVisible = !state.keyVisible; renderKey(); });
  $("#copy-key").addEventListener("click", () => copyText(state.apiKey, "API key 已复制"));
  $("#copy-endpoint").addEventListener("click", () => copyText(state.apiBase, "Base URL 已复制"));
  $("#copy-shell").addEventListener("click", () => copyText($("#shell-example").textContent, "环境变量已复制"));
  $("#refresh-accounts").addEventListener("click", () => loadAccounts().catch((error) => notify(error.message, true)));
  $("#refresh-quotas").addEventListener("click", () => loadQuotas().catch((error) => { $("#refresh-quotas").disabled = false; notify(error.message, true); }));
  $$(".provider-login").forEach((button) => button.addEventListener("click", () => startOAuth(button)));
  $("#relay-form").addEventListener("submit", saveRelay);
  $("#refresh-relays").addEventListener("click", () => loadRelays().catch((error) => notify(error.message, true)));
  $("#qwen-preset").addEventListener("click", fillQwenPreset);
  $("#refresh-models").addEventListener("click", () => loadModels().then(() => notify("模型列表已刷新")).catch((error) => notify(error.message, true)));
  $("#run-test").addEventListener("click", runModelTest);
  $("#debug-toggle").addEventListener("change", setDebug);
  $("#run-diagnostics").addEventListener("click", diagnostics);
}

async function boot() {
  bindEvents();
  await loadMeta();
  if (state.managementKey) {
    $("#management-key").value = state.managementKey;
    await connect();
  }
}

boot();

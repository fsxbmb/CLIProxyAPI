"use strict";

const state = {
  managementKey: sessionStorage.getItem("cliproxy-management-key") || "",
  apiKey: "",
  apiBase: "http://127.0.0.1:8317/v1",
  accounts: [],
  relays: [],
  models: [],
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
    sessionStorage.setItem("cliproxy-management-key", state.managementKey);
    setConnection(true);
    await Promise.allSettled([loadKeys(), loadAccounts(), loadRelays(), loadModels(), loadDebug()]);
    notify("已连接本机管理接口");
  } catch (error) {
    sessionStorage.removeItem("cliproxy-management-key");
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
    cell.colSpan = 5;
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
    row.append(provider, identity, status, requests, actions);
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
    detail.textContent = `${relay["base-url"] || "—"} · ${models} 个模型${relay.prefix ? ` · 前缀 ${relay.prefix}` : ""}`;
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
  const next = state.relays.filter((relay) => String(relay.name).toLowerCase() !== name.toLowerCase());
  next.push(entry);
  try {
    await management("openai-compatibility", { method: "PUT", body: next });
    $("#relay-form").reset();
    await loadRelays();
    await loadModels();
    notify("中转配置已保存并热重载");
  } catch (error) { notify(`保存失败：${error.message}`, true); }
}

async function deleteRelay(index) {
  const relay = state.relays[index];
  if (!confirm(`删除中转 ${relay.name || index + 1}？`)) return;
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
  state.models.forEach((model) => {
    const option = document.createElement("option");
    option.value = model.id || model.name || model;
    option.textContent = model.display_name || model.id || model.name || model;
    select.append(option);
  });
  $("#model-count").textContent = String(state.models.length);
}

async function runModelTest() {
  const model = $("#test-model").value;
  const prompt = $("#test-prompt").value.trim();
  if (!model) return notify("请先选择一个模型", true);
  $("#run-test").disabled = true;
  $("#test-output").textContent = "请求中…";
  try {
    const data = await proxyAPI("v1/chat/completions", {
      method: "POST",
      body: { model, messages: [{ role: "user", content: prompt }], stream: false, max_tokens: 80 },
    });
    const content = data.choices?.[0]?.message?.content ?? data.output_text ?? data;
    $("#test-output").textContent = typeof content === "string" ? content : JSON.stringify(content, null, 2);
    notify("模型请求成功");
  } catch (error) {
    $("#test-output").textContent = `失败：${error.message}`;
    notify(`测试失败：${error.message}`, true);
  } finally { $("#run-test").disabled = false; }
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

const state = {
  tasks: [],
  devices: [],
  clientId: getClientId(),
  selectedId: null,
  network: null,
};

const form = document.querySelector("#task-form");
const parentSelect = document.querySelector("#parentId");
const connection = document.querySelector("#connection");
const lanUrls = document.querySelector("#lan-urls");
const devices = document.querySelector("#devices");
const deviceCount = document.querySelector("#device-count");
const map = document.querySelector(".map");
const canvas = document.querySelector("#edges");
const nodes = document.querySelector("#nodes");
const details = document.querySelector("#details");

form.addEventListener("submit", async (event) => {
  event.preventDefault();
  const data = new FormData(form);
  const title = String(data.get("title") || "").trim();
  if (!title) {
    return;
  }

  const response = await fetch("/api/tasks", {
    method: "POST",
    headers: csrfHeaders(),
    body: data,
  });
  if (!response.ok) {
    await showRequestError(response);
    return;
  }
  form.reset();
});

window.addEventListener("resize", () => render());

startEvents();
loadNetwork();
loadDevices();
startClientStatusReporting();

async function startEvents() {
  if (!("EventSource" in window)) {
    connection.textContent = "manual";
    await loadSnapshot();
    return;
  }

  const events = new EventSource(`/api/events?client=${encodeURIComponent(state.clientId)}`);
  events.addEventListener("open", () => {
    connection.textContent = "live";
    reportClientStatus();
  });
  events.addEventListener("snapshot", (event) => {
    const snapshot = JSON.parse(event.data);
    state.tasks = snapshot.tasks || [];
    if (state.selectedId && !taskById().has(state.selectedId)) {
      state.selectedId = null;
    }
    render();
  });
  events.addEventListener("devices", (event) => {
    const snapshot = JSON.parse(event.data);
    state.devices = snapshot.devices || [];
    renderDevices();
  });
  events.addEventListener("error", () => {
    connection.textContent = "reconnecting";
  });
}

async function loadSnapshot() {
  const response = await fetch("/api/tasks");
  if (!response.ok) {
    return;
  }
  const snapshot = await response.json();
  state.tasks = snapshot.tasks || [];
  render();
}

async function loadNetwork() {
  const response = await fetch("/api/network");
  if (!response.ok) {
    return;
  }
  state.network = await response.json();
  renderNetwork();
}

async function loadDevices() {
  const response = await fetch("/api/devices");
  if (!response.ok) {
    return;
  }
  const snapshot = await response.json();
  state.devices = snapshot.devices || [];
  renderDevices();
}

function render() {
  renderParentOptions();
  renderGraph();
  renderDetails();
  renderDevices();
}

function renderParentOptions() {
  const previous = parentSelect.value;
  parentSelect.replaceChildren(new Option("Root", ""));
  for (const task of state.tasks) {
    const option = new Option(task.title, task.id);
    parentSelect.add(option);
  }
  parentSelect.value = previous;
}

function renderNetwork() {
  lanUrls.replaceChildren();
  const urls = state.network?.urls || [];
  if (urls.length === 0) {
    const empty = document.createElement("p");
    empty.className = "empty";
    empty.textContent = "No private network address found";
    lanUrls.append(empty);
    return;
  }

  for (const url of urls) {
    const link = document.createElement("a");
    link.href = url;
    link.textContent = url;
    lanUrls.append(link);
  }
}

function renderDevices() {
  devices.replaceChildren();
  const online = state.devices.length;
  deviceCount.textContent = `${online} online`;

  if (online === 0) {
    const empty = document.createElement("p");
    empty.className = "empty";
    empty.textContent = "No live browser sessions";
    devices.append(empty);
    return;
  }

  for (const device of state.devices) {
    const row = document.createElement("div");
    row.className = "device-card";

    const top = document.createElement("div");
    top.className = "device-top";

    const name = document.createElement("p");
    name.className = "device-name";
    name.textContent = device.name || "Unknown device";

    const pill = document.createElement("span");
    pill.className = "device-pill";
    pill.textContent = onlineLabel(device.health);
    if (device.health?.online === false) {
      pill.classList.add("offline");
    }

    top.append(name, pill);

    const meta = document.createElement("p");
    meta.className = "meta";
    const connections = device.connections === 1 ? "1 tab" : `${device.connections} tabs`;
    meta.textContent = `${device.address || "local"} | ${connections} | ${formatAge(device.connectedAt)}`;

    const metrics = document.createElement("div");
    metrics.className = "device-metrics";
    metrics.append(
      renderMetric("battery", batteryLabel(device.health), batteryBar(device.health)),
      renderMetric("network", networkLabel(device.health)),
      renderMetric("signal", signalLabel(device.health)),
    );

    row.append(top, meta, metrics);
    devices.append(row);
  }
}

function renderMetric(label, value, detail) {
  const metric = document.createElement("div");
  metric.className = "device-metric";

  const labelNode = document.createElement("span");
  labelNode.textContent = label;

  const valueNode = document.createElement("strong");
  valueNode.textContent = value;

  metric.append(labelNode, valueNode);
  if (detail) {
    metric.append(detail);
  }
  return metric;
}

function batteryBar(health) {
  if (typeof health?.batteryPercent !== "number") {
    return null;
  }
  const track = document.createElement("div");
  track.className = "battery-track";
  const fill = document.createElement("span");
  fill.style.width = `${Math.max(0, Math.min(100, health.batteryPercent))}%`;
  track.append(fill);
  return track;
}

function renderGraph() {
  const ordered = [...state.tasks].sort(compareTasks);
  const height = Math.max(560, ordered.length * 88 + 180);
  map.style.minHeight = `${height}px`;
  nodes.style.height = `${height}px`;
  canvas.style.height = `${height}px`;

  const rect = map.getBoundingClientRect();
  const width = Math.max(rect.width, 360);
  const dpr = window.devicePixelRatio || 1;
  canvas.width = Math.floor(width * dpr);
  canvas.height = Math.floor(height * dpr);
  canvas.style.width = `${width}px`;
  canvas.style.height = `${height}px`;

  const ctx = canvas.getContext("2d");
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
  ctx.clearRect(0, 0, width, height);

  const byId = taskById();
  const children = childrenByParent(ordered, byId);
  const positions = layoutTasks(ordered, children, byId, width, height);

  drawEdges(ctx, ordered, positions, byId);
  drawNodes(ordered, positions);
}

function drawEdges(ctx, ordered, positions, byId) {
  const styles = getComputedStyle(document.documentElement);
  ctx.strokeStyle = styles.getPropertyValue("--line-strong").trim() || "currentColor";
  ctx.lineWidth = 1;

  for (const task of ordered) {
    if (!task.parentId || !byId.has(task.parentId)) {
      continue;
    }
    const from = positions.get(task.parentId);
    const to = positions.get(task.id);
    if (!from || !to) {
      continue;
    }
    const midX = from.x + (to.x - from.x) * 0.58;
    ctx.beginPath();
    ctx.moveTo(from.x, from.y);
    ctx.bezierCurveTo(midX, from.y, midX, to.y, to.x, to.y);
    ctx.stroke();
  }
}

function drawNodes(ordered, positions) {
  nodes.replaceChildren();

  if (ordered.length === 0) {
    const empty = document.createElement("p");
    empty.className = "empty";
    empty.textContent = "No tasks yet";
    empty.style.position = "absolute";
    empty.style.left = "32px";
    empty.style.top = "32px";
    nodes.append(empty);
    return;
  }

  for (const task of ordered) {
    const point = positions.get(task.id);
    if (!point) {
      continue;
    }
    const node = document.createElement("article");
    node.className = "node";
    if (task.done) {
      node.classList.add("done");
    }
    if (state.selectedId === task.id) {
      node.classList.add("selected");
    }
    node.style.left = `${point.x}px`;
    node.style.top = `${point.y}px`;
    node.addEventListener("click", () => {
      state.selectedId = task.id;
      render();
    });

    const title = document.createElement("h2");
    title.textContent = task.title;

    const footer = document.createElement("div");
    footer.className = "node-footer";

    const meta = document.createElement("p");
    meta.className = "meta";
    meta.textContent = task.attachments?.length
      ? `${task.attachments.length} file${task.attachments.length === 1 ? "" : "s"}`
      : task.done
        ? "done"
        : "open";

    const actions = document.createElement("div");
    actions.className = "mini-actions";

    const done = document.createElement("button");
    done.type = "button";
    done.title = task.done ? "Mark open" : "Mark done";
    done.textContent = task.done ? "open" : "done";
    done.addEventListener("click", async (event) => {
      event.stopPropagation();
      await patchTask(task.id, { done: !task.done });
    });

    const remove = document.createElement("button");
    remove.type = "button";
    remove.title = "Delete task";
    remove.textContent = "delete";
    remove.addEventListener("click", async (event) => {
      event.stopPropagation();
      await deleteTask(task.id);
    });

    actions.append(done, remove);
    footer.append(meta, actions);
    node.append(title, footer);
    nodes.append(node);
  }
}

function renderDetails() {
  const byId = taskById();
  const selected = state.selectedId ? byId.get(state.selectedId) : state.tasks[0];
  details.replaceChildren();

  if (!selected) {
    const empty = document.createElement("p");
    empty.className = "empty";
    empty.textContent = "Select a node";
    details.append(empty);
    return;
  }

  const title = document.createElement("h2");
  title.className = "details-title";
  title.textContent = selected.title;

  const status = document.createElement("p");
  status.className = "meta";
  status.textContent = selected.done ? "done" : "open";

  const notes = document.createElement("div");
  notes.className = "details-section";
  const notesBody = document.createElement("p");
  notesBody.className = "details-notes";
  notesBody.textContent = selected.notes || "No notes";
  notes.append(notesBody);

  details.append(title, status, notes);

  if (selected.attachments?.length) {
    const section = document.createElement("div");
    section.className = "details-section attachments";
    for (const attachment of selected.attachments) {
      section.append(renderAttachment(attachment));
    }
    details.append(section);
  }
}

function renderAttachment(attachment) {
  const item = document.createElement("div");
  item.className = "attachment";

  if (attachment.type?.startsWith("image/")) {
    const image = document.createElement("img");
    image.src = attachment.url;
    image.alt = attachment.name;
    item.append(image);
  }

  const link = document.createElement("a");
  link.href = attachment.url;
  link.target = "_blank";
  link.rel = "noreferrer";
  link.textContent = attachment.name;
  item.append(link);

  const meta = document.createElement("p");
  meta.className = "meta";
  meta.textContent = formatBytes(attachment.size || 0);
  item.append(meta);

  return item;
}

function layoutTasks(tasks, children, byId, width, height) {
  const roots = tasks.filter((task) => !task.parentId || !byId.has(task.parentId));
  const positions = new Map();
  const maxDepth = Math.max(1, depthOfForest(roots, children));
  const xGap = Math.min(260, Math.max(150, (width - 140) / maxDepth));
  const startX = width < 520 ? 118 : 150;
  const rightPadding = width < 520 ? 88 : 130;
  let cursorY = 82;

  for (const root of roots) {
    place(root, 0);
    cursorY += 34;
  }

  return positions;

  function place(task, depth) {
    const kids = children.get(task.id) || [];
    let y;
    if (kids.length === 0) {
      y = cursorY;
      cursorY += 88;
    } else {
      const start = cursorY;
      for (const child of kids) {
        place(child, depth + 1);
      }
      const end = cursorY - 88;
      y = (start + end) / 2;
    }
    positions.set(task.id, {
      x: Math.min(width - rightPadding, startX + depth * xGap),
      y: Math.min(height - 72, Math.max(72, y)),
    });
  }
}

function depthOfForest(roots, children) {
  let max = 1;
  for (const root of roots) {
    max = Math.max(max, walk(root, 1));
  }
  return max;

  function walk(task, depth) {
    let next = depth;
    for (const child of children.get(task.id) || []) {
      next = Math.max(next, walk(child, depth + 1));
    }
    return next;
  }
}

function childrenByParent(tasks, byId) {
  const children = new Map();
  for (const task of tasks) {
    if (!task.parentId || !byId.has(task.parentId)) {
      continue;
    }
    if (!children.has(task.parentId)) {
      children.set(task.parentId, []);
    }
    children.get(task.parentId).push(task);
  }
  for (const list of children.values()) {
    list.sort(compareTasks);
  }
  return children;
}

function taskById() {
  return new Map(state.tasks.map((task) => [task.id, task]));
}

function compareTasks(a, b) {
  return String(a.createdAt).localeCompare(String(b.createdAt)) || a.title.localeCompare(b.title);
}

async function patchTask(id, payload) {
  const response = await fetch(`/api/tasks/${id}`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json", ...csrfHeaders() },
    body: JSON.stringify(payload),
  });
  if (!response.ok) {
    await showRequestError(response);
  }
}

async function deleteTask(id) {
  const response = await fetch(`/api/tasks/${id}`, {
    method: "DELETE",
    headers: csrfHeaders(),
  });
  if (!response.ok) {
    await showRequestError(response);
  }
}

function getClientId() {
  const key = "doit.clientId";
  const existing = window.localStorage.getItem(key);
  if (existing) {
    return existing;
  }
  const id = typeof crypto.randomUUID === "function"
    ? crypto.randomUUID()
    : `client-${Date.now()}-${Math.random().toString(16).slice(2)}`;
  window.localStorage.setItem(key, id);
  return id;
}

function csrfHeaders() {
  const token = readCookie("doit_csrf");
  return token ? { "X-CSRF-Token": token } : {};
}

function readCookie(name) {
  const prefix = `${name}=`;
  return document.cookie
    .split(";")
    .map((part) => part.trim())
    .find((part) => part.startsWith(prefix))
    ?.slice(prefix.length) || "";
}

async function startClientStatusReporting() {
  await attachBatteryListeners();
  await reportClientStatus();
  window.setInterval(reportClientStatus, 30000);
  window.addEventListener("online", reportClientStatus);
  window.addEventListener("offline", reportClientStatus);

  const connectionInfo = getConnectionInfo();
  if (connectionInfo?.addEventListener) {
    connectionInfo.addEventListener("change", reportClientStatus);
  }
}

async function attachBatteryListeners() {
  const battery = await readBattery();
  if (!battery?.addEventListener) {
    return;
  }
  battery.addEventListener("levelchange", reportClientStatus);
  battery.addEventListener("chargingchange", reportClientStatus);
}

async function reportClientStatus() {
  const status = await collectClientStatus();
  try {
    await fetch("/api/client-status", {
      method: "POST",
      headers: { "Content-Type": "application/json", ...csrfHeaders() },
      body: JSON.stringify(status),
      keepalive: true,
    });
  } catch {
    connection.textContent = "status delayed";
  }
}

async function collectClientStatus() {
  const status = {
    browserId: state.clientId,
    online: navigator.onLine,
  };

  const connectionInfo = getConnectionInfo();
  if (connectionInfo) {
    if (connectionInfo.effectiveType) {
      status.effectiveType = connectionInfo.effectiveType;
    } else if (connectionInfo.type) {
      status.effectiveType = connectionInfo.type;
    }
    if (typeof connectionInfo.downlink === "number") {
      status.downlinkMbps = connectionInfo.downlink;
    }
    if (typeof connectionInfo.rtt === "number") {
      status.rttMs = connectionInfo.rtt;
    }
  }

  const battery = await readBattery();
  if (battery) {
    status.batteryPercent = Math.round(battery.level * 100);
    status.charging = battery.charging;
  }

  return status;
}

function getConnectionInfo() {
  return navigator.connection || navigator.mozConnection || navigator.webkitConnection || null;
}

async function readBattery() {
  if (window.doitBattery) {
    return window.doitBattery;
  }
  if (typeof navigator.getBattery !== "function") {
    return null;
  }
  try {
    window.doitBattery = await navigator.getBattery();
    return window.doitBattery;
  } catch {
    return null;
  }
}

async function showRequestError(response) {
  let message = "Request failed";
  try {
    const body = await response.json();
    message = body.error || message;
  } catch {
    message = await response.text();
  }
  connection.textContent = message;
}

function formatBytes(bytes) {
  if (bytes < 1024) {
    return `${bytes} B`;
  }
  const units = ["KiB", "MiB", "GiB"];
  let value = bytes / 1024;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  return `${value.toFixed(1)} ${units[unit]}`;
}

function onlineLabel(health) {
  if (health?.online === true) {
    return "online";
  }
  if (health?.online === false) {
    return "offline";
  }
  return "unknown";
}

function batteryLabel(health) {
  if (typeof health?.batteryPercent !== "number") {
    return "unknown";
  }
  const suffix = health.charging === true ? " charging" : "";
  return `${health.batteryPercent}%${suffix}`;
}

function networkLabel(health) {
  const parts = [];
  if (health?.effectiveType) {
    parts.push(health.effectiveType);
  }
  if (typeof health?.downlinkMbps === "number") {
    parts.push(`${formatNumber(health.downlinkMbps)} Mbps`);
  }
  return parts.length ? parts.join(" | ") : "unknown";
}

function signalLabel(health) {
  if (typeof health?.rttMs !== "number") {
    return "unknown";
  }
  return `${health.rttMs} ms rtt`;
}

function formatNumber(value) {
  if (value >= 10) {
    return Math.round(value).toString();
  }
  return value.toFixed(1);
}

function formatAge(value) {
  const started = new Date(value);
  if (Number.isNaN(started.getTime())) {
    return "now";
  }
  const seconds = Math.max(0, Math.floor((Date.now() - started.getTime()) / 1000));
  if (seconds < 60) {
    return "now";
  }
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) {
    return `${minutes}m`;
  }
  const hours = Math.floor(minutes / 60);
  return `${hours}h`;
}

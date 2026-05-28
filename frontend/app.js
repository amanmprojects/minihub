const reposEl = document.querySelector("#repos");
const detailEl = document.querySelector("#repo-detail");
const form = document.querySelector("#create-repo");
const refresh = document.querySelector("#refresh");
const template = document.querySelector("#repo-card-template");
const loginForm = document.querySelector("#login-form");

let repos = [];
let selected = null;
let branches = [];
let currentBranch = "HEAD";
let currentPath = "";
let authToken = localStorage.getItem("minihubToken") || "";

async function api(path, options = {}) {
  const headers = { "Content-Type": "application/json", ...(options.headers || {}) };
  if (authToken) headers.Authorization = `Bearer ${authToken}`;
  const response = await fetch(path, {
    headers,
    ...options,
  });
  if (!response.ok) {
    const payload = await response.json().catch(() => ({}));
    throw new Error(payload.error || `Request failed: ${response.status}`);
  }
  if (response.status === 204) return null;
  const text = await response.text();
  return text ? JSON.parse(text) : null;
}

async function loadRepos() {
  reposEl.innerHTML = `<div class="message">Loading repositories...</div>`;
  try {
    repos = await api("/api/repos");
    renderRepos();
    if (selected) {
      const match = repos.find((repo) => repo.name === selected.name);
      if (match) selectRepo(match, currentBranch);
    }
  } catch (error) {
    reposEl.innerHTML = `<div class="message error">${escapeHTML(error.message)}</div>`;
  }
}

function renderRepos() {
  reposEl.innerHTML = "";
  if (repos.length === 0) {
    reposEl.innerHTML = `<div class="message">No repositories yet.</div>`;
    return;
  }
  for (const repo of repos) {
    const node = template.content.firstElementChild.cloneNode(true);
    node.querySelector("strong").textContent = repo.name;
    node.querySelector("span").textContent = repo.description || "No description";
    node.classList.toggle("active", selected?.name === repo.name);
    node.addEventListener("click", () => selectRepo(repo));
    reposEl.appendChild(node);
  }
}

async function selectRepo(repo, preferredBranch = "") {
  selected = repo;
  currentPath = "";
  renderRepos();
  detailEl.innerHTML = `
    <div class="detail-head">
      <div class="repo-title-row">
        <div>
          <h2>${escapeHTML(repo.name)}</h2>
          <p class="muted">${escapeHTML(repo.description || "No description")}</p>
        </div>
        <div class="branch-controls">
          <select id="branch-select" aria-label="Branch"></select>
          <button type="button" id="delete-branch">Delete</button>
        </div>
      </div>
      <div class="clone-row">
        <code>git clone ${escapeHTML(repo.cloneUrl)}</code>
        <button type="button" id="copy-clone">Copy</button>
      </div>
      <form id="create-branch" class="inline-form">
        <input name="name" placeholder="new-branch" autocomplete="off" required />
        <input name="source" placeholder="source ref" autocomplete="off" />
        <button type="submit">Create branch</button>
      </form>
    </div>
    <nav class="tabs" aria-label="Repository sections">
      <button type="button" class="tab active" data-tab="code">Code</button>
      <button type="button" class="tab" data-tab="commits">Commits</button>
      <button type="button" class="tab" data-tab="pulls">Pull requests</button>
      <button type="button" class="tab" data-tab="issues">Issues</button>
      <button type="button" class="tab" data-tab="ops">Releases & ops</button>
      <button type="button" class="tab" data-tab="people">Users & orgs</button>
      <button type="button" class="tab" data-tab="settings">Settings</button>
    </nav>
    <div class="content-grid">
      <section class="panel code-panel">
        <h3>Files <span id="path-label" class="muted"></span></h3>
        <div id="tree" class="rows"><div class="message">Loading files...</div></div>
      </section>
      <section class="panel">
        <h3>Recent commits</h3>
        <div id="commits" class="rows"><div class="message">Loading commits...</div></div>
      </section>
    </div>
    <section class="panel detail-panel" id="commit-detail" hidden>
      <h3>Commit detail</h3>
      <div class="message">Select a commit to inspect its diff.</div>
    </section>
    <section class="panel detail-panel" id="pulls-panel" hidden>${pullsHTML()}</section>
    <section class="panel detail-panel" id="issues-panel" hidden>${issuesHTML()}</section>
    <section class="panel detail-panel" id="ops-panel" hidden>${opsHTML()}</section>
    <section class="panel detail-panel" id="people-panel" hidden>${peopleHTML()}</section>
    <section class="panel detail-panel" id="settings-panel" hidden>
      <h3>Repository settings</h3>
      <form id="settings-form" class="settings-form">
        <label>Description <input name="description" value="${escapeAttr(repo.description || "")}" /></label>
        <label>Protected branches <input name="protectedBranches" value="${escapeAttr((repo.protectedBranches || []).join(", "))}" /></label>
        <button type="submit">Save settings</button>
      </form>
      <form id="grant-permission" class="settings-form compact-form">
        <strong>Grant permission</strong>
        <input name="userId" placeholder="user id" required />
        <select name="role"><option>read</option><option>triage</option><option>write</option><option>maintain</option><option>admin</option></select>
        <button type="submit">Grant</button>
      </form>
      <div id="permissions-list" class="rows"></div>
    </section>
  `;

  document.querySelector("#copy-clone").addEventListener("click", async () => {
    await navigator.clipboard.writeText(repo.cloneUrl);
  });
  wireTabs();
  wireSettings(repo.name);
  wirePermissionForm(repo.name);
  wireProductForms(repo.name);
  await loadBranches(repo.name, preferredBranch || repo.defaultBranch || "HEAD");
  wireBranchForms(repo.name);
  await Promise.all([loadTree(repo.name), loadCommits(repo.name), loadProductData(repo.name), loadPermissions(repo.name)]);
}

function wireTabs() {
  document.querySelectorAll(".tab").forEach((tab) => {
    tab.addEventListener("click", () => {
      document.querySelectorAll(".tab").forEach((item) => item.classList.remove("active"));
      tab.classList.add("active");
      const mode = tab.dataset.tab;
      document.querySelector(".content-grid").hidden = mode !== "code" && mode !== "commits";
      document.querySelector("#commit-detail").hidden = mode !== "commits";
      document.querySelector("#pulls-panel").hidden = mode !== "pulls";
      document.querySelector("#issues-panel").hidden = mode !== "issues";
      document.querySelector("#ops-panel").hidden = mode !== "ops";
      document.querySelector("#people-panel").hidden = mode !== "people";
      document.querySelector("#settings-panel").hidden = mode !== "settings";
    });
  });
}

async function loadBranches(repoName, preferredBranch) {
  branches = await api(`/api/repos/${encodeURIComponent(repoName)}/branches`);
  const branchSelect = document.querySelector("#branch-select");
  branchSelect.innerHTML = branches
    .map((branch) => `<option value="${escapeAttr(branch.name)}">${escapeHTML(branch.name)}${branch.default ? " (default)" : ""}${branch.protected ? " (protected)" : ""}</option>`)
    .join("");
  const exists = branches.some((branch) => branch.name === preferredBranch);
  currentBranch = exists ? preferredBranch : branches.find((branch) => branch.default)?.name || branches[0]?.name || "HEAD";
  branchSelect.value = currentBranch;
  branchSelect.addEventListener("change", async () => {
    currentBranch = branchSelect.value;
    currentPath = "";
    await Promise.all([loadTree(repoName), loadCommits(repoName)]);
  });
}

function wireBranchForms(repoName) {
  document.querySelector("#create-branch").addEventListener("submit", async (event) => {
    event.preventDefault();
    const data = Object.fromEntries(new FormData(event.currentTarget));
    data.source = data.source || currentBranch;
    try {
      await api(`/api/repos/${encodeURIComponent(repoName)}/branches`, {
        method: "POST",
        body: JSON.stringify(data),
      });
      event.currentTarget.reset();
      await loadBranches(repoName, data.name);
      await Promise.all([loadTree(repoName), loadCommits(repoName)]);
    } catch (error) {
      showCommitDetail(`<div class="message error">${escapeHTML(error.message)}</div>`);
    }
  });

  document.querySelector("#delete-branch").addEventListener("click", async () => {
    const branch = branches.find((item) => item.name === currentBranch);
    if (!branch || branch.default) return;
    try {
      await api(`/api/repos/${encodeURIComponent(repoName)}/branches?name=${encodeURIComponent(currentBranch)}`, { method: "DELETE" });
      await loadBranches(repoName, selected.defaultBranch || "HEAD");
      await Promise.all([loadTree(repoName), loadCommits(repoName)]);
    } catch (error) {
      showCommitDetail(`<div class="message error">${escapeHTML(error.message)}</div>`);
    }
  });
}

async function loadTree(repoName, dir = currentPath) {
  currentPath = dir || "";
  const treeEl = document.querySelector("#tree");
  const pathLabel = document.querySelector("#path-label");
  pathLabel.textContent = currentPath ? `/${currentPath}` : "/";
  try {
    const params = new URLSearchParams({ ref: currentBranch });
    if (currentPath) params.set("path", currentPath);
    const entries = await api(`/api/repos/${encodeURIComponent(repoName)}/tree?${params}`);
    if (entries.length === 0) {
      treeEl.innerHTML = `<div class="message">No files on this branch.</div>`;
      return;
    }
    const parent = currentPath.includes("/") ? currentPath.slice(0, currentPath.lastIndexOf("/")) : "";
    const up = currentPath ? `<div class="row"><span class="badge">dir</span><button type="button" data-path="${escapeAttr(parent)}" data-type="tree">..</button></div>` : "";
    treeEl.innerHTML = up + entries
      .map((entry) => {
        const kind = entry.type === "tree" ? "dir" : "file";
        return `<div class="row"><span class="badge">${kind}</span><button type="button" data-path="${escapeAttr(entry.path)}" data-type="${entry.type}">${escapeHTML(entry.name)}</button></div>`;
      })
      .join("");
    treeEl.querySelectorAll("button[data-type='tree']").forEach((button) => {
      button.addEventListener("click", () => loadTree(repoName, button.dataset.path));
    });
    treeEl.querySelectorAll("button[data-type='blob']").forEach((button) => {
      button.addEventListener("click", () => loadBlob(repoName, button.dataset.path));
    });
  } catch (error) {
    treeEl.innerHTML = `<div class="message error">${escapeHTML(error.message)}</div>`;
  }
}

async function loadBlob(repoName, path) {
  const treeEl = document.querySelector("#tree");
  const params = new URLSearchParams({ ref: currentBranch, path });
  const response = await fetch(`/api/repos/${encodeURIComponent(repoName)}/blob?${params}`);
  if (!response.ok) {
    treeEl.innerHTML = `<div class="message error">Unable to load ${escapeHTML(path)}</div>`;
    return;
  }
  const text = await response.text();
  treeEl.innerHTML = `<div class="row"><button type="button" id="back-to-tree">Back to files</button></div><pre>${escapeHTML(text)}</pre>`;
  document.querySelector("#back-to-tree").addEventListener("click", () => loadTree(repoName, currentPath));
}

async function loadCommits(repoName) {
  const commitsEl = document.querySelector("#commits");
  try {
    const commits = await api(`/api/repos/${encodeURIComponent(repoName)}/commits?ref=${encodeURIComponent(currentBranch)}`);
    if (commits.length === 0) {
      commitsEl.innerHTML = `<div class="message">No commits on this branch.</div>`;
      return;
    }
    commitsEl.innerHTML = commits
      .map((commit) => `<button class="commit-row" type="button" data-hash="${escapeAttr(commit.hash)}"><span class="badge">${escapeHTML(commit.hash.slice(0, 7))}</span><span>${escapeHTML(commit.subject)}</span><small>${escapeHTML(commit.author)}</small></button>`)
      .join("");
    commitsEl.querySelectorAll(".commit-row").forEach((button) => {
      button.addEventListener("click", () => loadCommit(repoName, button.dataset.hash));
    });
  } catch (error) {
    commitsEl.innerHTML = `<div class="message error">${escapeHTML(error.message)}</div>`;
  }
}

async function loadCommit(repoName, hash) {
  const detail = await api(`/api/repos/${encodeURIComponent(repoName)}/commits/${encodeURIComponent(hash)}`);
  showCommitDetail(`
    <div class="commit-meta">
      <strong>${escapeHTML(detail.subject)}</strong>
      <span class="muted">${escapeHTML(detail.hash)} by ${escapeHTML(detail.author)} &lt;${escapeHTML(detail.authorEmail)}&gt;</span>
    </div>
    <pre>${escapeHTML(detail.diff || "No diff for this commit.")}</pre>
  `);
  document.querySelector("[data-tab='commits']").click();
}

function showCommitDetail(html) {
  const panel = document.querySelector("#commit-detail");
  panel.hidden = false;
  panel.innerHTML = `<h3>Commit detail</h3>${html}`;
}

function wireSettings(repoName) {
  document.querySelector("#settings-form").addEventListener("submit", async (event) => {
    event.preventDefault();
    const formData = new FormData(event.currentTarget);
    const protectedBranches = String(formData.get("protectedBranches") || "")
      .split(",")
      .map((item) => item.trim())
      .filter(Boolean);
    try {
      selected = await api(`/api/repos/${encodeURIComponent(repoName)}/settings`, {
        method: "PATCH",
        body: JSON.stringify({
          description: String(formData.get("description") || ""),
          protectedBranches,
        }),
      });
      await loadRepos();
      await loadBranches(repoName, currentBranch);
    } catch (error) {
      showCommitDetail(`<div class="message error">${escapeHTML(error.message)}</div>`);
    }
  });
}

function pullsHTML() {
  return `
    <h3>Pull requests</h3>
    <form id="create-pr" class="settings-form compact-form">
      <input name="title" placeholder="Title" required />
      <input name="sourceBranch" placeholder="source branch" required />
      <input name="targetBranch" placeholder="target branch" value="main" required />
      <input name="body" placeholder="Description" />
      <button type="submit">Open pull request</button>
    </form>
    <div id="pulls-list" class="rows"></div>
    <div id="pr-detail" class="subdetail"></div>
  `;
}

function issuesHTML() {
  return `
    <h3>Issues</h3>
    <form id="create-issue" class="settings-form compact-form">
      <input name="title" placeholder="Title" required />
      <input name="body" placeholder="Description" />
      <button type="submit">Open issue</button>
    </form>
    <div id="issues-list" class="rows"></div>
  `;
}

function opsHTML() {
  return `
    <h3>Releases, webhooks, CI</h3>
    <div class="ops-grid">
      <form id="create-release" class="settings-form compact-form">
        <strong>Release</strong>
        <input name="tagName" placeholder="v1.0.0" required />
        <input name="title" placeholder="Title" required />
        <input name="notes" placeholder="Notes" />
        <button type="submit">Create release</button>
      </form>
      <form id="create-webhook" class="settings-form compact-form">
        <strong>Webhook</strong>
        <input name="url" placeholder="https://example.com/hook" required />
        <input name="events" placeholder="push,pull_request" />
        <button type="submit">Add webhook</button>
      </form>
      <form id="create-ci" class="settings-form compact-form">
        <strong>Record CI run</strong>
        <input name="commitSha" placeholder="commit sha" required />
        <input name="branch" placeholder="branch" />
        <select name="status"><option>queued</option><option>running</option><option>success</option><option>failure</option><option>cancelled</option></select>
        <button type="submit">Record CI run</button>
      </form>
      <form id="run-ci" class="settings-form compact-form">
        <strong>Execute CI</strong>
        <input name="ref" placeholder="branch or sha" />
        <button type="submit">Run .minihub/ci.sh</button>
      </form>
    </div>
    <div class="content-grid">
      <div><h3>Releases</h3><div id="releases-list" class="rows"></div></div>
      <div><h3>Webhooks</h3><div id="webhooks-list" class="rows"></div></div>
      <div><h3>CI runs</h3><div id="ci-list" class="rows"></div></div>
    </div>
  `;
}

function peopleHTML() {
  return `
    <h3>Users and orgs</h3>
    <div class="ops-grid">
      <form id="create-user" class="settings-form compact-form">
        <strong>User</strong>
        <input name="username" placeholder="username" required />
        <input name="displayName" placeholder="Display name" />
        <input name="email" placeholder="email" required />
        <input name="password" placeholder="password" type="password" />
        <button type="submit">Create user</button>
      </form>
      <form id="create-org" class="settings-form compact-form">
        <strong>Organization</strong>
        <input name="name" placeholder="org-name" required />
        <input name="displayName" placeholder="Display name" />
        <button type="submit">Create org</button>
      </form>
    </div>
    <div class="content-grid">
      <div><h3>Users</h3><div id="users-list" class="rows"></div></div>
      <div><h3>Organizations</h3><div id="orgs-list" class="rows"></div></div>
    </div>
  `;
}

function wireProductForms(repoName) {
  wireJSONForm("#create-pr", `/api/repos/${encodeURIComponent(repoName)}/pulls`, () => loadPulls(repoName));
  wireJSONForm("#create-issue", `/api/repos/${encodeURIComponent(repoName)}/issues`, () => loadIssues(repoName));
  wireJSONForm("#create-release", `/api/repos/${encodeURIComponent(repoName)}/releases`, () => loadReleases(repoName));
  wireJSONForm("#create-webhook", `/api/repos/${encodeURIComponent(repoName)}/webhooks`, () => loadWebhooks(repoName));
  wireJSONForm("#create-ci", `/api/repos/${encodeURIComponent(repoName)}/ci`, () => loadCI(repoName));
  wireJSONForm("#run-ci", `/api/repos/${encodeURIComponent(repoName)}/ci/run`, () => loadCI(repoName));
  wireJSONForm("#create-user", "/api/users", loadPeople);
  wireJSONForm("#create-org", "/api/orgs", loadPeople);
}

function wireJSONForm(selector, url, after, method = "POST") {
  const formEl = document.querySelector(selector);
  formEl.addEventListener("submit", async (event) => {
    event.preventDefault();
    const data = Object.fromEntries(new FormData(formEl));
    for (const key of ["userId", "authorId", "reviewerId", "assigneeId", "lineNumber"]) {
      if (data[key]) data[key] = Number(data[key]);
    }
    if (selector === "#create-webhook" && !("active" in data)) data.active = true;
    if ("active" in data) data.active = data.active === true || data.active === "on";
    try {
      await api(url, { method, body: JSON.stringify(data) });
      formEl.reset();
      await after();
    } catch (error) {
      showCommitDetail(`<div class="message error">${escapeHTML(error.message)}</div>`);
    }
  });
}

async function loadProductData(repoName) {
  await Promise.all([loadPulls(repoName), loadIssues(repoName), loadReleases(repoName), loadWebhooks(repoName), loadCI(repoName), loadPeople()]);
}

async function loadPulls(repoName) {
  const pulls = await api(`/api/repos/${encodeURIComponent(repoName)}/pulls`);
  renderRows("#pulls-list", pulls, (item) => `#${item.number} ${escapeHTML(item.title)} <span class="muted">${escapeHTML(item.sourceBranch)} -> ${escapeHTML(item.targetBranch)} ${escapeHTML(item.status)}</span>`, (row, item) => {
    row.addEventListener("click", () => loadPRDetail(repoName, item.number));
  });
}

async function loadPRDetail(repoName, number) {
  const diff = await fetch(`/api/repos/${encodeURIComponent(repoName)}/pulls/${number}/diff`).then((r) => r.text());
  const reviews = await api(`/api/repos/${encodeURIComponent(repoName)}/pulls/${number}/reviews`);
  const comments = await api(`/api/repos/${encodeURIComponent(repoName)}/pulls/${number}/comments`);
  document.querySelector("#pr-detail").innerHTML = `
    <div class="toolbar-row">
      <button type="button" id="merge-pr">Merge</button>
      <form id="review-pr" class="inline-form"><select name="state"><option>approved</option><option>changes_requested</option><option>commented</option></select><input name="body" placeholder="Review" /><button>Review</button></form>
      <form id="comment-pr" class="inline-form"><input name="body" placeholder="Comment" required /><button>Comment</button></form>
    </div>
    <div class="rows">${reviews.map((r) => `<div class="row"><span class="badge">${escapeHTML(r.state)}</span><span>${escapeHTML(r.body || "")}</span></div>`).join("")}${comments.map((c) => `<div class="row"><span class="badge">comment</span><span>${escapeHTML(c.body || "")}</span></div>`).join("")}</div>
    <pre>${escapeHTML(diff)}</pre>
  `;
  document.querySelector("#merge-pr").addEventListener("click", async () => {
    await api(`/api/repos/${encodeURIComponent(repoName)}/pulls/${number}/merge`, { method: "POST", body: "{}" });
    await loadPulls(repoName);
    await loadPRDetail(repoName, number);
  });
  wireJSONForm("#review-pr", `/api/repos/${encodeURIComponent(repoName)}/pulls/${number}/reviews`, () => loadPRDetail(repoName, number));
  wireJSONForm("#comment-pr", `/api/repos/${encodeURIComponent(repoName)}/pulls/${number}/comments`, () => loadPRDetail(repoName, number));
}

async function loadIssues(repoName) {
  const items = await api(`/api/repos/${encodeURIComponent(repoName)}/issues`);
  renderRows("#issues-list", items, (item) => `#${item.number} ${escapeHTML(item.title)} <span class="muted">${escapeHTML(item.status)}</span>`);
}

async function loadReleases(repoName) {
  const items = await api(`/api/repos/${encodeURIComponent(repoName)}/releases`);
  renderRows("#releases-list", items, (item) => `${escapeHTML(item.tagName)} <span class="muted">${escapeHTML(item.title)}</span>`);
}

async function loadWebhooks(repoName) {
  const items = await api(`/api/repos/${encodeURIComponent(repoName)}/webhooks`);
  renderRows("#webhooks-list", items, (item) => `${escapeHTML(item.url)} <span class="muted">${escapeHTML(item.events)}</span>`);
}

async function loadCI(repoName) {
  const items = await api(`/api/repos/${encodeURIComponent(repoName)}/ci`);
  renderRows("#ci-list", items, (item) => `${escapeHTML(String(item.commitSha || "").slice(0, 7))} <span class="muted">${escapeHTML(item.branch || "")} ${escapeHTML(item.status)}</span>`);
}

async function loadPeople() {
  const [users, orgs] = await Promise.all([api("/api/users"), api("/api/orgs")]);
  renderRows("#users-list", users, (item) => `${escapeHTML(item.username)} <span class="muted">${escapeHTML(item.email)}</span>`);
  renderRows("#orgs-list", orgs, (item) => `${escapeHTML(item.name)} <span class="muted">${escapeHTML(item.displayName || "")}</span>`);
}

function wirePermissionForm(repoName) {
  wireJSONForm("#grant-permission", `/api/repos/${encodeURIComponent(repoName)}/permissions`, () => loadPermissions(repoName), "PUT");
}

async function loadPermissions(repoName) {
  const items = await api(`/api/repos/${encodeURIComponent(repoName)}/permissions`);
  renderRows("#permissions-list", items, (item) => `${escapeHTML(item.username)} <span class="muted">${escapeHTML(item.role)}</span>`);
}

function renderRows(selector, items, render, wire = null) {
  const el = document.querySelector(selector);
  if (!items || items.length === 0) {
    el.innerHTML = `<div class="message">None yet.</div>`;
    return;
  }
  el.innerHTML = "";
  for (const item of items) {
    const row = document.createElement("button");
    row.type = "button";
    row.className = "row action-row";
    row.innerHTML = render(item);
    if (wire) wire(row, item);
    el.appendChild(row);
  }
}

loginForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  const data = Object.fromEntries(new FormData(loginForm));
  try {
    const session = await api("/api/login", { method: "POST", body: JSON.stringify(data) });
    authToken = session.token;
    localStorage.setItem("minihubToken", authToken);
    loginForm.reset();
  } catch (error) {
    detailEl.innerHTML = `<div class="empty error">${escapeHTML(error.message)}</div>`;
  }
});

form.addEventListener("submit", async (event) => {
  event.preventDefault();
  const data = Object.fromEntries(new FormData(form));
  try {
    const repo = await api("/api/repos", {
      method: "POST",
      body: JSON.stringify(data),
    });
    form.reset();
    await loadRepos();
    selectRepo(repo);
  } catch (error) {
    detailEl.innerHTML = `<div class="empty error">${escapeHTML(error.message)}</div>`;
  }
});

refresh.addEventListener("click", loadRepos);

function escapeHTML(value) {
  return String(value).replace(/[&<>"']/g, (char) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[char]);
}

function escapeAttr(value) {
  return escapeHTML(value).replace(/`/g, "&#96;");
}

loadRepos();

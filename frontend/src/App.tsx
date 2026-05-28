import { FormEvent, ReactNode, useEffect, useMemo, useState } from "react";
import { Activity, Boxes, CheckCircle2, Code2, Copy, File, Folder, GitBranch, GitCommit, FolderGit, GitPullRequest, Menu, Moon, Plus, RefreshCw, Settings, Shield, Sun, Users, XCircle } from "lucide-react";
import { api, setToken, text } from "./api/client";
import type { Branch, CIRun, Comment, Commit, CommitDetail, Issue, Organization, Permission, PullRequest, Release, Repository, Review, Session, TreeEntry, User, Webhook } from "./api/types";

type Tab = "code" | "commits" | "pulls" | "issues" | "ops" | "people" | "settings";
type Theme = "dark" | "light";
type CodeMode = "tree" | "blob" | null;
type Notice = { kind: "success" | "error"; text: string } | null;
type RouteState = { repo: Repository | null; repoName: string; tab: Tab; codeMode: CodeMode; branch: string; path: string; notFound: boolean };

const tabs: { id: Tab; label: string; path: string; icon: typeof Code2 }[] = [
  { id: "code", label: "Code", path: "", icon: Code2 },
  { id: "commits", label: "Commits", path: "commits", icon: GitCommit },
  { id: "pulls", label: "Pull requests", path: "pulls", icon: GitPullRequest },
  { id: "issues", label: "Issues", path: "issues", icon: Activity },
  { id: "ops", label: "Releases & ops", path: "releases", icon: Boxes },
  { id: "people", label: "Users & orgs", path: "people", icon: Users },
  { id: "settings", label: "Settings", path: "settings", icon: Settings },
];

const tabByPath = new Map(tabs.filter((tab) => tab.path).map((tab) => [tab.path, tab.id]));

export function App() {
  const [theme, setTheme] = useState<Theme>(() => (localStorage.getItem("minihubTheme") as Theme) || (matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light"));
  const [repos, setRepos] = useState<Repository[]>([]);
  const [loadingRepos, setLoadingRepos] = useState(true);
  const [notice, setNotice] = useState<Notice>(null);
  const [mobileOpen, setMobileOpen] = useState(false);
  const [pathname, setPathname] = useState(window.location.pathname);

  const route = useMemo(() => parseRoute(pathname, repos), [pathname, repos]);

  useEffect(() => {
    document.documentElement.classList.toggle("dark", theme === "dark");
    localStorage.setItem("minihubTheme", theme);
  }, [theme]);

  useEffect(() => {
    const onPopState = () => setPathname(window.location.pathname);
    window.addEventListener("popstate", onPopState);
    return () => window.removeEventListener("popstate", onPopState);
  }, []);

  async function loadRepos(selectName?: string) {
    setLoadingRepos(true);
    try {
      const items = await api<Repository[]>("/api/repos");
      setRepos(items);
      if (selectName) navigate(repoUrl(selectName));
    } catch (error) {
      flash(errorMessage(error), "error");
    } finally {
      setLoadingRepos(false);
    }
  }

  useEffect(() => {
    loadRepos();
  }, []);

  function navigate(nextPath: string) {
    if (window.location.pathname !== nextPath) window.history.pushState(null, "", nextPath);
    setPathname(window.location.pathname);
    setMobileOpen(false);
  }

  function flash(text: string, kind: "success" | "error" = "success") {
    setNotice({ kind, text });
    window.setTimeout(() => setNotice(null), 3500);
  }

  async function createRepo(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = event.currentTarget;
    const data = Object.fromEntries(new FormData(form));
    try {
      const repo = await api<Repository>("/api/repos", { method: "POST", body: JSON.stringify(data) });
      form.reset();
      await loadRepos(repo.name);
      flash("Repository created");
    } catch (error) {
      flash(errorMessage(error), "error");
    }
  }

  async function login(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = event.currentTarget;
    const data = Object.fromEntries(new FormData(form));
    try {
      const session = await api<Session>("/api/login", { method: "POST", body: JSON.stringify(data) });
      setToken(session.token);
      form.reset();
      flash("Signed in");
    } catch (error) {
      flash(errorMessage(error), "error");
    }
  }

  return (
    <div className="min-h-screen bg-background text-foreground">
      <header className="sticky top-0 z-30 border-b border-border bg-header text-header-foreground">
        <div className="mx-auto flex h-16 max-w-screen-2xl items-center gap-3 px-4 sm:px-6">
          <button className="header-icon lg:hidden" onClick={() => setMobileOpen(true)} aria-label="Open navigation"><Menu size={20} /></button>
          <button className="flex items-center gap-3 text-left" onClick={() => navigate("/")} aria-label="Minihub home">
            <FolderGit size={32} />
            <div className="hidden sm:block">
              <h1 className="text-base font-semibold leading-tight">Minihub</h1>
              <p className="text-xs text-header-muted">Self-hosted Git repositories</p>
            </div>
          </button>
          <form onSubmit={login} className="ml-auto hidden items-center gap-2 md:flex">
            <input className="header-input w-32" name="username" placeholder="Username" autoComplete="username" />
            <input className="header-input w-32" name="password" placeholder="Password" type="password" autoComplete="current-password" />
            <button className="header-button" type="submit">Sign in</button>
          </form>
          <button className="header-icon" onClick={() => setTheme(theme === "dark" ? "light" : "dark")} aria-label="Toggle theme">{theme === "dark" ? <Sun size={18} /> : <Moon size={18} />}</button>
        </div>
      </header>

      <div className="mx-auto grid max-w-screen-2xl gap-6 px-4 py-6 sm:px-6 lg:grid-cols-[296px_1fr]">
        <Sidebar repos={repos} selected={route.repo} loading={loadingRepos} createRepo={createRepo} refresh={() => loadRepos()} select={(repo) => navigate(repoUrl(repo.name))} />
        {mobileOpen && <div className="fixed inset-0 z-40 bg-black/60 lg:hidden" onClick={() => setMobileOpen(false)}><aside className="h-full w-[88vw] max-w-sm bg-card" onClick={(event) => event.stopPropagation()}><Sidebar repos={repos} selected={route.repo} loading={loadingRepos} createRepo={createRepo} refresh={() => loadRepos()} select={(repo) => navigate(repoUrl(repo.name))} mobile /></aside></div>}
        <main className="min-w-0">
          {route.repo ? <RepoDetail repo={route.repo} route={route} navigate={navigate} flash={flash} refreshRepos={() => loadRepos(route.repo?.name)} /> : route.notFound && !loadingRepos ? <NotFound repoName={route.repoName} /> : <EmptyState />}
        </main>
      </div>
      {notice && <div className={`toast ${notice.kind === "error" ? "toast-error" : "toast-success"}`}>{notice.kind === "error" ? <XCircle size={18} /> : <CheckCircle2 size={18} />}{notice.text}</div>}
    </div>
  );
}

function Sidebar({ repos, selected, loading, createRepo, refresh, select, mobile = false }: { repos: Repository[]; selected: Repository | null; loading: boolean; createRepo: (event: FormEvent<HTMLFormElement>) => void; refresh: () => void; select: (repo: Repository) => void; mobile?: boolean }) {
  return (
    <aside className={`${mobile ? "" : "hidden lg:block"} min-h-[calc(100vh-7rem)] rounded-md border border-border bg-card`}>
      <div className="border-b border-border p-4">
        <div className="mb-3 flex items-center justify-between">
          <h2 className="text-sm font-semibold">Repositories</h2>
          <button className="icon-btn" onClick={refresh} aria-label="Refresh repositories"><RefreshCw size={16} /></button>
        </div>
        <form onSubmit={createRepo} className="grid gap-2">
          <input className="input" name="name" placeholder="team/project" required autoComplete="off" />
          <input className="input" name="description" placeholder="Description" autoComplete="off" />
          <button className="btn-primary" type="submit"><Plus size={16} />New repository</button>
        </form>
      </div>
      <div className="p-2">
        {loading && <SkeletonRows />}
        {!loading && repos.length === 0 && <p className="rounded-md border border-dashed border-border p-4 text-sm text-muted-foreground">No repositories yet. Create one to get started.</p>}
        {repos.map((repo) => (
          <button key={repo.name} onClick={() => select(repo)} className={`repo-card ${selected?.name === repo.name ? "repo-card-active" : ""}`}>
            <span className="truncate font-semibold text-link">{repo.name}</span>
            <span className="line-clamp-2 text-xs text-muted-foreground">{repo.description || "No description"}</span>
          </button>
        ))}
      </div>
    </aside>
  );
}

function RepoDetail({ repo, route, navigate, flash, refreshRepos }: { repo: Repository; route: RouteState; navigate: (path: string) => void; flash: (message: string, kind?: "success" | "error") => void; refreshRepos: () => void }) {
  const [branches, setBranches] = useState<Branch[]>([]);
  const [branch, setBranch] = useState(route.branch || repo.defaultBranch || "HEAD");

  async function loadBranches(preferred = branch) {
    try {
      const items = await api<Branch[]>(`/api/repos/${encodeURIComponent(repo.name)}/branches`);
      setBranches(items);
      const next = items.find((item) => item.name === preferred)?.name || items.find((item) => item.default)?.name || items[0]?.name || "HEAD";
      setBranch(next);
    } catch (error) {
      flash(errorMessage(error), "error");
    }
  }

  useEffect(() => {
    const preferred = route.branch || repo.defaultBranch || "HEAD";
    setBranch(preferred);
    loadBranches(preferred);
  }, [repo.name, route.branch]);

  async function createBranch(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = event.currentTarget;
    const data = Object.fromEntries(new FormData(form));
    data.source = data.source || branch;
    try {
      await api(`/api/repos/${encodeURIComponent(repo.name)}/branches`, { method: "POST", body: JSON.stringify(data) });
      form.reset();
      await loadBranches(String(data.name));
      navigate(repoUrl(repo.name, "tree", String(data.name)));
      flash("Branch created");
    } catch (error) {
      flash(errorMessage(error), "error");
    }
  }

  async function deleteBranch() {
    const current = branches.find((item) => item.name === branch);
    if (!current || current.default) return;
    try {
      await api(`/api/repos/${encodeURIComponent(repo.name)}/branches?name=${encodeURIComponent(branch)}`, { method: "DELETE" });
      await loadBranches(repo.defaultBranch || "HEAD");
      navigate(repoUrl(repo.name));
      flash("Branch deleted");
    } catch (error) {
      flash(errorMessage(error), "error");
    }
  }

  function changeBranch(nextBranch: string) {
    setBranch(nextBranch);
    if (route.tab === "code") navigate(repoUrl(repo.name, "tree", nextBranch, route.path));
  }

  function navigateTab(nextTab: Tab) {
    const item = tabs.find((tab) => tab.id === nextTab);
    navigate(item?.path ? repoUrl(repo.name, item.path) : repoUrl(repo.name));
  }

  return (
    <div className="space-y-5">
      <section className="repo-header">
        <div className="flex flex-col gap-4 border-b border-border pb-4 md:flex-row md:items-start md:justify-between">
          <div className="min-w-0">
            <div className="flex min-w-0 items-center gap-2 text-xl">
              <GitBranch className="shrink-0 text-muted-foreground" size={20} />
              <h2 className="break-words font-semibold text-link">{repo.name}</h2>
              <span className="badge">Private</span>
            </div>
            <p className="mt-2 max-w-3xl text-sm text-muted-foreground">{repo.description || "No description provided."}</p>
          </div>
          <div className="flex flex-wrap gap-2">
            <select className="input min-w-44" value={branch} onChange={(event) => changeBranch(event.target.value)}>{branches.map((item) => <option key={item.name} value={item.name}>{item.name}{item.default ? " · default" : ""}{item.protected ? " · protected" : ""}</option>)}</select>
            <button className="btn-secondary" onClick={deleteBranch}>Delete branch</button>
          </div>
        </div>
        <nav className="tabs mt-3" aria-label="Repository sections">{tabs.map((item) => { const Icon = item.icon; return <button key={item.id} onClick={() => navigateTab(item.id)} className={`tab ${route.tab === item.id ? "tab-active" : ""}`}><Icon size={16} />{item.label}</button>; })}</nav>
      </section>

      <section className="panel">
        <div className="grid gap-3 p-4 xl:grid-cols-[1fr_auto]">
          <code className="clone-box">git clone {repo.cloneUrl}</code>
          <button className="btn-secondary" onClick={async () => { await navigator.clipboard.writeText(repo.cloneUrl); flash("Clone URL copied"); }}><Copy size={16} />Copy</button>
        </div>
        <form onSubmit={createBranch} className="grid gap-2 border-t border-border p-4 md:grid-cols-[1fr_1fr_auto]">
          <input className="input" name="name" placeholder="new-branch" required autoComplete="off" />
          <input className="input" name="source" placeholder={`source ref (${branch})`} autoComplete="off" />
          <button className="btn-primary" type="submit"><GitBranch size={16} />Create branch</button>
        </form>
      </section>

      {route.tab === "code" && <CodeTab repo={repo} branch={branch} mode={route.codeMode} routePath={route.path} navigateTree={(path) => navigate(repoUrl(repo.name, "tree", branch, path))} navigateBlob={(path) => navigate(repoUrl(repo.name, "blob", branch, path))} flash={flash} />}
      {route.tab === "commits" && <CommitsTab repo={repo} branch={branch} flash={flash} />}
      {route.tab === "pulls" && <PullsTab repo={repo} flash={flash} />}
      {route.tab === "issues" && <IssuesTab repo={repo} flash={flash} />}
      {route.tab === "ops" && <OpsTab repo={repo} flash={flash} />}
      {route.tab === "people" && <PeopleTab flash={flash} />}
      {route.tab === "settings" && <SettingsTab repo={repo} flash={flash} refreshRepos={refreshRepos} loadBranches={loadBranches} />}
    </div>
  );
}

function CodeTab({ repo, branch, mode, routePath, navigateTree, navigateBlob, flash }: { repo: Repository; branch: string; mode: CodeMode; routePath: string; navigateTree: (path: string) => void; navigateBlob: (path: string) => void; flash: (message: string, kind?: "success" | "error") => void }) {
  const [path, setPath] = useState("");
  const [entries, setEntries] = useState<TreeEntry[]>([]);
  const [blob, setBlob] = useState<{ path: string; content: string } | null>(null);
  const [loading, setLoading] = useState(false);

  async function loadTree(nextPath = "") {
    setLoading(true);
    setBlob(null);
    setPath(nextPath);
    try {
      const params = new URLSearchParams({ ref: branch });
      if (nextPath) params.set("path", nextPath);
      setEntries(await api<TreeEntry[]>(`/api/repos/${encodeURIComponent(repo.name)}/tree?${params}`));
    } catch (error) {
      flash(errorMessage(error), "error");
    } finally {
      setLoading(false);
    }
  }

  async function loadBlob(blobPath: string) {
    setLoading(true);
    try {
      const params = new URLSearchParams({ ref: branch, path: blobPath });
      setPath(parentPath(blobPath));
      setBlob({ path: blobPath, content: await text(`/api/repos/${encodeURIComponent(repo.name)}/blob?${params}`) });
    } catch (error) {
      flash(errorMessage(error), "error");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    if (mode === "blob" && routePath) loadBlob(routePath);
    else loadTree(routePath);
  }, [repo.name, branch, mode, routePath]);

  const parent = parentPath(path);

  return <section className="panel"><PanelTitle title="Files" subtitle={blob?.path || `/${routePath}`} />{loading && <SkeletonRows />}{!loading && blob ? <><div className="border-b border-border p-3"><button className="btn-secondary" onClick={() => navigateTree(path)}>Back to files</button></div><pre className="code-block">{blob.content}</pre></> : !loading && <div className="divide-y divide-border">{path && <FileRow kind="dir" name=".." onClick={() => navigateTree(parent)} />}{entries.length === 0 ? <EmptyRows text="No files on this branch." /> : entries.map((entry) => <FileRow key={entry.path} kind={entry.type === "tree" ? "dir" : "file"} name={entry.name} onClick={() => entry.type === "tree" ? navigateTree(entry.path) : navigateBlob(entry.path)} />)}</div>}</section>;
}

function CommitsTab({ repo, branch, flash }: { repo: Repository; branch: string; flash: (message: string, kind?: "success" | "error") => void }) {
  const [commits, setCommits] = useState<Commit[]>([]);
  const [detail, setDetail] = useState<CommitDetail | null>(null);

  async function loadCommits() {
    try {
      setCommits(await api<Commit[]>(`/api/repos/${encodeURIComponent(repo.name)}/commits?ref=${encodeURIComponent(branch)}`));
      setDetail(null);
    } catch (error) {
      flash(errorMessage(error), "error");
    }
  }

  async function loadCommit(hash: string) {
    try {
      setDetail(await api<CommitDetail>(`/api/repos/${encodeURIComponent(repo.name)}/commits/${encodeURIComponent(hash)}`));
    } catch (error) {
      flash(errorMessage(error), "error");
    }
  }

  useEffect(() => {
    loadCommits();
  }, [repo.name, branch]);

  return <div className="grid gap-5 xl:grid-cols-[420px_1fr]"><section className="panel"><PanelTitle title="Recent commits" subtitle={branch} /><div className="divide-y divide-border">{commits.length === 0 ? <EmptyRows text="No commits on this branch." /> : commits.map((commit) => <button key={commit.hash} className="commit-row" onClick={() => loadCommit(commit.hash)}><span className="badge">{commit.hash.slice(0, 7)}</span><span className="min-w-0 truncate font-medium">{commit.subject}</span><span className="col-start-2 truncate text-xs text-muted-foreground">{commit.author}</span></button>)}</div></section><section className="panel min-w-0"><PanelTitle title="Commit detail" subtitle={detail?.hash.slice(0, 12) || "Select a commit"} />{detail ? <><div className="space-y-1 border-b border-border p-4"><h3 className="font-semibold">{detail.subject}</h3><p className="text-sm text-muted-foreground">{detail.author} &lt;{detail.authorEmail}&gt;</p></div><pre className="code-block">{detail.diff || "No diff for this commit."}</pre></> : <EmptyRows text="Select a commit to inspect its diff." />}</section></div>;
}

function PullsTab({ repo, flash }: { repo: Repository; flash: (message: string, kind?: "success" | "error") => void }) {
  const [items, setItems] = useState<PullRequest[]>([]);
  const [detail, setDetail] = useState<{ number: number; diff: string; reviews: Review[]; comments: Comment[] } | null>(null);

  async function load() { setItems(await api<PullRequest[]>(`/api/repos/${encodeURIComponent(repo.name)}/pulls`)); }
  async function open(number: number) { setDetail({ number, diff: await text(`/api/repos/${encodeURIComponent(repo.name)}/pulls/${number}/diff`), reviews: await api<Review[]>(`/api/repos/${encodeURIComponent(repo.name)}/pulls/${number}/reviews`), comments: await api<Comment[]>(`/api/repos/${encodeURIComponent(repo.name)}/pulls/${number}/comments`) }); }
  async function submit(event: FormEvent<HTMLFormElement>) { event.preventDefault(); const form = event.currentTarget; try { await api(`/api/repos/${encodeURIComponent(repo.name)}/pulls`, { method: "POST", body: JSON.stringify(Object.fromEntries(new FormData(form))) }); form.reset(); await load(); flash("Pull request opened"); } catch (error) { flash(errorMessage(error), "error"); } }
  async function merge() { if (!detail) return; try { await api(`/api/repos/${encodeURIComponent(repo.name)}/pulls/${detail.number}/merge`, { method: "POST", body: "{}" }); await load(); await open(detail.number); flash("Pull request merged"); } catch (error) { flash(errorMessage(error), "error"); } }

  useEffect(() => { load().catch((error) => flash(errorMessage(error), "error")); }, [repo.name]);

  return <CrudPanel title="Pull requests" form={<form onSubmit={submit} className="form-grid"><input className="input" name="title" placeholder="Title" required /><input className="input" name="sourceBranch" placeholder="source branch" required /><input className="input" name="targetBranch" placeholder="target branch" defaultValue="main" required /><input className="input" name="body" placeholder="Description" /><button className="btn-primary">Open PR</button></form>} items={items.map((item) => ({ key: item.number, title: `#${item.number} ${item.title}`, subtitle: `${item.sourceBranch} → ${item.targetBranch} · ${item.status}`, action: () => open(item.number) }))}>{detail && <section className="mt-5 overflow-hidden rounded-md border border-border"><div className="flex flex-wrap items-center justify-between gap-3 border-b border-border p-4"><h3 className="font-semibold">PR #{detail.number}</h3><button className="btn-primary" onClick={merge}>Merge</button></div><div className="grid gap-3 border-b border-border p-4 md:grid-cols-2"><MiniList title="Reviews" items={detail.reviews.map((r) => `${r.state}: ${r.body || ""}`)} /><MiniList title="Comments" items={detail.comments.map((c) => c.body || "")} /></div><pre className="code-block">{detail.diff}</pre></section>}</CrudPanel>;
}

function IssuesTab({ repo, flash }: { repo: Repository; flash: (message: string, kind?: "success" | "error") => void }) {
  return <GenericResource<Issue> repo={repo} path="issues" title="Issues" flash={flash} form={<><input className="input" name="title" placeholder="Title" required /><input className="input" name="body" placeholder="Description" /><button className="btn-primary">Open issue</button></>} render={(item) => ({ title: `#${item.number} ${item.title}`, subtitle: item.status })} />;
}

function OpsTab({ repo, flash }: { repo: Repository; flash: (message: string, kind?: "success" | "error") => void }) {
  const [releases, setReleases] = useState<Release[]>([]); const [webhooks, setWebhooks] = useState<Webhook[]>([]); const [ci, setCi] = useState<CIRun[]>([]);
  async function load() { const base = `/api/repos/${encodeURIComponent(repo.name)}`; const [a, b, c] = await Promise.all([api<Release[]>(`${base}/releases`), api<Webhook[]>(`${base}/webhooks`), api<CIRun[]>(`${base}/ci`)]); setReleases(a); setWebhooks(b); setCi(c); }
  async function post(path: string, event: FormEvent<HTMLFormElement>, transform = (data: Record<string, FormDataEntryValue | boolean>) => data) { event.preventDefault(); const form = event.currentTarget; try { await api(`/api/repos/${encodeURIComponent(repo.name)}/${path}`, { method: "POST", body: JSON.stringify(transform(Object.fromEntries(new FormData(form)))) }); form.reset(); await load(); flash("Saved"); } catch (error) { flash(errorMessage(error), "error"); } }
  useEffect(() => { load().catch((error) => flash(errorMessage(error), "error")); }, [repo.name]);
  return <div className="space-y-5"><section className="panel"><PanelTitle title="Releases, webhooks, CI" /><div className="grid gap-3 p-4 xl:grid-cols-4"><MiniForm title="Release" onSubmit={(e) => post("releases", e)} fields={["tagName:v1.0.0", "title:Title", "notes:Notes"]} button="Create release" /><MiniForm title="Webhook" onSubmit={(e) => post("webhooks", e, (d) => ({ ...d, active: true }))} fields={["url:https://example.com/hook", "events:push,pull_request"]} button="Add webhook" /><MiniForm title="Record CI" onSubmit={(e) => post("ci", e)} fields={["commitSha:commit sha", "branch:branch", "status:queued"]} button="Record CI" /><MiniForm title="Execute CI" onSubmit={(e) => post("ci/run", e)} fields={["ref:branch or sha"]} button="Run CI" /></div></section><div className="grid gap-5 xl:grid-cols-3"><SimpleList title="Releases" items={releases.map((r) => `${r.tagName} · ${r.title}`)} /><SimpleList title="Webhooks" items={webhooks.map((w) => `${w.url} · ${w.events}`)} /><SimpleList title="CI runs" items={ci.map((run) => `${(run.commitSha || "").slice(0, 7)} · ${run.branch || ""} · ${run.status}`)} /></div></div>;
}

function PeopleTab({ flash }: { flash: (message: string, kind?: "success" | "error") => void }) {
  const [users, setUsers] = useState<User[]>([]); const [orgs, setOrgs] = useState<Organization[]>([]);
  async function load() { const [a, b] = await Promise.all([api<User[]>("/api/users"), api<Organization[]>("/api/orgs")]); setUsers(a); setOrgs(b); }
  async function post(path: string, event: FormEvent<HTMLFormElement>) { event.preventDefault(); const form = event.currentTarget; try { await api(path, { method: "POST", body: JSON.stringify(Object.fromEntries(new FormData(form))) }); form.reset(); await load(); flash("Saved"); } catch (error) { flash(errorMessage(error), "error"); } }
  useEffect(() => { load().catch((error) => flash(errorMessage(error), "error")); }, []);
  return <div className="space-y-5"><section className="panel"><PanelTitle title="Users and organizations" /><div className="grid gap-3 p-4 md:grid-cols-2"><MiniForm title="User" onSubmit={(e) => post("/api/users", e)} fields={["username:username", "displayName:Display name", "email:email", "password:password"]} button="Create user" password="password" /><MiniForm title="Organization" onSubmit={(e) => post("/api/orgs", e)} fields={["name:org-name", "displayName:Display name"]} button="Create org" /></div></section><div className="grid gap-5 md:grid-cols-2"><SimpleList title="Users" items={users.map((u) => `${u.username} · ${u.email}`)} /><SimpleList title="Organizations" items={orgs.map((o) => `${o.name} · ${o.displayName || ""}`)} /></div></div>;
}

function SettingsTab({ repo, flash, refreshRepos, loadBranches }: { repo: Repository; flash: (message: string, kind?: "success" | "error") => void; refreshRepos: () => void; loadBranches: () => Promise<void>; }) {
  const [permissions, setPermissions] = useState<Permission[]>([]);
  async function loadPermissions() { setPermissions(await api<Permission[]>(`/api/repos/${encodeURIComponent(repo.name)}/permissions`)); }
  async function saveSettings(event: FormEvent<HTMLFormElement>) { event.preventDefault(); const form = event.currentTarget; const formData = new FormData(form); try { await api(`/api/repos/${encodeURIComponent(repo.name)}/settings`, { method: "PATCH", body: JSON.stringify({ description: formData.get("description") || "", protectedBranches: String(formData.get("protectedBranches") || "").split(",").map((x) => x.trim()).filter(Boolean) }) }); await refreshRepos(); await loadBranches(); flash("Settings saved"); } catch (error) { flash(errorMessage(error), "error"); } }
  async function grant(event: FormEvent<HTMLFormElement>) { event.preventDefault(); const form = event.currentTarget; const data = Object.fromEntries(new FormData(form)); try { await api(`/api/repos/${encodeURIComponent(repo.name)}/permissions`, { method: "PUT", body: JSON.stringify({ ...data, userId: Number(data.userId) }) }); form.reset(); await loadPermissions(); flash("Permission granted"); } catch (error) { flash(errorMessage(error), "error"); } }
  useEffect(() => { loadPermissions().catch((error) => flash(errorMessage(error), "error")); }, [repo.name]);
  return <div className="grid gap-5 xl:grid-cols-[1fr_380px]"><section className="panel"><PanelTitle title="Repository settings" /><form onSubmit={saveSettings} className="grid gap-4 p-4"><label className="label">Description<input className="input mt-1" name="description" defaultValue={repo.description} /></label><label className="label">Protected branches<input className="input mt-1" name="protectedBranches" defaultValue={(repo.protectedBranches || []).join(", ")} /></label><button className="btn-primary w-fit">Save settings</button></form></section><section className="panel"><PanelTitle title="Permissions" /><form onSubmit={grant} className="grid gap-2 border-b border-border p-4"><input className="input" name="userId" placeholder="user id" required /><select className="input" name="role"><option>read</option><option>triage</option><option>write</option><option>maintain</option><option>admin</option></select><button className="btn-primary">Grant</button></form><div className="divide-y divide-border">{permissions.map((p) => <div key={`${p.userId}-${p.role}`} className="row"><Shield size={16} /><span>{p.username}</span><span className="ml-auto text-muted-foreground">{p.role}</span></div>)}</div></section></div>;
}

function GenericResource<T>({ repo, path, title, form, render, flash }: { repo: Repository; path: string; title: string; form: ReactNode; render: (item: T) => { title: string; subtitle: string }; flash: (message: string, kind?: "success" | "error") => void }) {
  const [items, setItems] = useState<T[]>([]);
  async function load() { setItems(await api<T[]>(`/api/repos/${encodeURIComponent(repo.name)}/${path}`)); }
  async function submit(event: FormEvent<HTMLFormElement>) { event.preventDefault(); const element = event.currentTarget; try { await api(`/api/repos/${encodeURIComponent(repo.name)}/${path}`, { method: "POST", body: JSON.stringify(Object.fromEntries(new FormData(element))) }); element.reset(); await load(); flash("Saved"); } catch (error) { flash(errorMessage(error), "error"); } }
  useEffect(() => { load().catch((error) => flash(errorMessage(error), "error")); }, [repo.name]);
  return <CrudPanel title={title} form={<form onSubmit={submit} className="form-grid">{form}</form>} items={items.map((item, index) => ({ key: index, ...render(item) }))} />;
}

function CrudPanel({ title, form, items, children }: { title: string; form: ReactNode; items: { key: string | number; title: string; subtitle: string; action?: () => void }[]; children?: ReactNode }) {
  return <section className="panel"><PanelTitle title={title} /> <div className="border-b border-border p-4">{form}</div><div className="divide-y divide-border">{items.length === 0 ? <EmptyRows text="None yet." /> : items.map((item) => <button key={item.key} type="button" onClick={item.action} className="row w-full text-left"><span className="font-medium">{item.title}</span><span className="ml-auto text-sm text-muted-foreground">{item.subtitle}</span></button>)}</div>{children}</section>;
}

function MiniForm({ title, fields, button, onSubmit, password }: { title: string; fields: string[]; button: string; onSubmit: (event: FormEvent<HTMLFormElement>) => void; password?: string }) {
  return <form onSubmit={onSubmit} className="rounded-md border border-border bg-subtle p-4"><h3 className="mb-3 font-semibold">{title}</h3><div className="grid gap-2">{fields.map((field) => { const [name, placeholder] = field.split(":"); return <input key={name} className="input" name={name} placeholder={placeholder} type={password === name ? "password" : "text"} required={!["notes", "events", "branch", "ref", "displayName", "password"].includes(name)} />; })}<button className="btn-primary">{button}</button></div></form>;
}

function SimpleList({ title, items }: { title: string; items: string[] }) { return <section className="panel"><PanelTitle title={title} /><div className="divide-y divide-border">{items.length ? items.map((item) => <div className="row" key={item}>{item}</div>) : <EmptyRows text="None yet." />}</div></section>; }
function MiniList({ title, items }: { title: string; items: string[] }) { return <div><h4 className="mb-2 text-sm font-semibold text-muted-foreground">{title}</h4><div className="space-y-2">{items.length ? items.map((item, i) => <p key={i} className="rounded-md bg-muted p-2 text-sm">{item}</p>) : <p className="text-sm text-muted-foreground">None yet.</p>}</div></div>; }
function FileRow({ kind, name, onClick }: { kind: string; name: string; onClick: () => void }) { const Icon = kind === "dir" ? Folder : File; return <button className="row w-full text-left" onClick={onClick}><Icon className={kind === "dir" ? "text-directory" : "text-muted-foreground"} size={16} /><span className="truncate font-medium text-link">{name}</span></button>; }
function PanelTitle({ title, subtitle }: { title: string; subtitle?: string }) { return <div className="flex flex-wrap items-center justify-between gap-2 border-b border-border bg-subtle px-4 py-3"><h3 className="font-semibold">{title}</h3>{subtitle && <span className="max-w-full truncate text-sm text-muted-foreground">{subtitle}</span>}</div>; }
function EmptyRows({ text }: { text: string }) { return <div className="p-6 text-sm text-muted-foreground">{text}</div>; }
function SkeletonRows() { return <div className="space-y-2 p-3">{Array.from({ length: 5 }).map((_, i) => <div key={i} className="h-12 animate-pulse rounded-md bg-muted" />)}</div>; }
function EmptyState() { return <section className="grid min-h-[70vh] place-items-center rounded-md border border-dashed border-border bg-card p-8 text-center"><div><div className="mx-auto mb-4 grid size-14 place-items-center rounded-full bg-muted text-muted-foreground"><FolderGit /></div><h2 className="text-2xl font-semibold">Select or create a repository</h2><p className="mt-2 max-w-md text-muted-foreground">Pick a repository from the sidebar to browse code, pull requests, issues, releases, CI, users, and settings.</p></div></section>; }
function NotFound({ repoName }: { repoName: string }) { return <section className="grid min-h-[70vh] place-items-center rounded-md border border-dashed border-border bg-card p-8 text-center"><div><h2 className="text-2xl font-semibold">Repository not found</h2><p className="mt-2 max-w-md text-muted-foreground">No repository matches <span className="font-mono">{repoName || window.location.pathname}</span>.</p></div></section>; }
function errorMessage(error: unknown) { return error instanceof Error ? error.message : "Something went wrong"; }

function parseRoute(pathname: string, repos: Repository[]): RouteState {
  const rawSegments = pathname.split("/").filter(Boolean);
  const segments = rawSegments.map(decodeSegment);
  if (segments.length === 0) return { repo: null, repoName: "", tab: "code", codeMode: null, branch: "", path: "", notFound: false };
  let matchedRepo: Repository | null = null;
  let matchedCount = 0;
  for (let count = segments.length; count > 0; count -= 1) {
    const candidate = segments.slice(0, count).join("/");
    const repo = repos.find((item) => item.name === candidate);
    if (repo) {
      matchedRepo = repo;
      matchedCount = count;
      break;
    }
  }
  if (!matchedRepo) return { repo: null, repoName: segments.join("/"), tab: "code", codeMode: null, branch: "", path: "", notFound: true };
  const rest = segments.slice(matchedCount);
  if (rest[0] === "tree" || rest[0] === "blob") return { repo: matchedRepo, repoName: matchedRepo.name, tab: "code", codeMode: rest[0], branch: rest[1] || matchedRepo.defaultBranch || "HEAD", path: rest.slice(2).join("/"), notFound: false };
  const tab = tabByPath.get(rest[0] || "") || "code";
  return { repo: matchedRepo, repoName: matchedRepo.name, tab, codeMode: null, branch: matchedRepo.defaultBranch || "HEAD", path: "", notFound: false };
}

function repoUrl(repoName: string, ...parts: string[]) {
  const repoPath = repoName.split("/").filter(Boolean).map(encodeURIComponent).join("/");
  const rest = parts.filter((part) => part !== "").map((part) => encodePath(part)).filter(Boolean).join("/");
  return `/${[repoPath, rest].filter(Boolean).join("/")}`;
}

function encodePath(path: string) {
  return path.split("/").filter(Boolean).map(encodeURIComponent).join("/");
}

function decodeSegment(segment: string) {
  try {
    return decodeURIComponent(segment);
  } catch {
    return segment;
  }
}

function parentPath(path: string) {
  return path.includes("/") ? path.slice(0, path.lastIndexOf("/")) : "";
}

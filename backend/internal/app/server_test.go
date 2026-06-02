package app

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestGitCLICloneAndPushOverHTTP(t *testing.T) {
	server, err := NewServer(Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(server)
	defer ts.Close()

	token := createUserAndLogin(t, ts.URL, "alice", "alice@example.com", "secret")
	repoName := "alice/demo"
	body, _ := json.Marshal(map[string]string{"name": "demo", "description": "test repo", "visibility": "public"})
	resp, err := postJSON(t, ts.URL+"/api/repos", token, body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create repo status = %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	work := filepath.Join(t.TempDir(), "work")
	run(t, "", "git", "clone", gitURL(t, ts.URL, "alice", "secret", repoName), work)
	run(t, work, "git", "remote", "set-url", "origin", gitURL(t, ts.URL, "alice", "secret", repoName))
	writeFile(t, filepath.Join(work, "README.md"), "hello from minihub\n")
	run(t, work, "git", "config", "user.email", "test@example.com")
	run(t, work, "git", "config", "user.name", "Minihub Test")
	run(t, work, "git", "add", "README.md")
	run(t, work, "git", "commit", "-m", "initial commit")
	run(t, work, "git", "push", "origin", "HEAD:main")
	run(t, work, "git", "checkout", "-b", "dev")
	writeFile(t, filepath.Join(work, "DEV.md"), "development branch\n")
	if err := os.MkdirAll(filepath.Join(work, ".minihub"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(work, ".minihub", "ci.sh"), "echo ci ok\n")
	run(t, work, "git", "add", "DEV.md")
	run(t, work, "git", "add", ".minihub/ci.sh")
	run(t, work, "git", "commit", "-m", "dev commit")
	run(t, work, "git", "push", "origin", "dev")

	var tree []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	getJSON(t, ts.URL+"/api/repos/"+repoName+"/tree", &tree)
	if len(tree) != 1 || tree[0].Name != "README.md" || tree[0].Type != "blob" {
		t.Fatalf("tree = %#v", tree)
	}

	var devTree []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	getJSON(t, ts.URL+"/api/repos/"+repoName+"/tree?ref=dev", &devTree)
	if len(devTree) != 3 || devTree[2].Name != "README.md" {
		t.Fatalf("dev tree = %#v", devTree)
	}

	var commits []struct {
		Hash    string `json:"hash"`
		Subject string `json:"subject"`
	}
	getJSON(t, ts.URL+"/api/repos/"+repoName+"/commits", &commits)
	if len(commits) != 1 || commits[0].Subject != "initial commit" {
		t.Fatalf("commits = %#v", commits)
	}

	var devCommits []struct {
		Hash    string `json:"hash"`
		Subject string `json:"subject"`
	}
	getJSON(t, ts.URL+"/api/repos/"+repoName+"/commits?ref=dev", &devCommits)
	if len(devCommits) != 2 || devCommits[0].Subject != "dev commit" {
		t.Fatalf("dev commits = %#v", devCommits)
	}

	var detail struct {
		Subject string `json:"subject"`
		Diff    string `json:"diff"`
	}
	getJSON(t, ts.URL+"/api/repos/"+repoName+"/commits/"+devCommits[0].Hash, &detail)
	if detail.Subject != "dev commit" || !bytes.Contains([]byte(detail.Diff), []byte("DEV.md")) {
		t.Fatalf("commit detail = %#v", detail)
	}

	var branches []struct {
		Name      string `json:"name"`
		Default   bool   `json:"default"`
		Protected bool   `json:"protected"`
	}
	getJSON(t, ts.URL+"/api/repos/"+repoName+"/branches", &branches)
	if len(branches) != 2 || branches[0].Name != "dev" || branches[1].Name != "main" || !branches[1].Default {
		t.Fatalf("branches = %#v", branches)
	}

	body, _ = json.Marshal(map[string]string{"name": "release", "source": "main"})
	resp, err = postJSON(t, ts.URL+"/api/repos/"+repoName+"/branches", token, body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create branch status = %d", resp.StatusCode)
	}
	_ = resp.Body.Close()
	req, err := http.NewRequest(http.MethodDelete, ts.URL+"/api/repos/"+repoName+"/branches?name=release", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete branch status = %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	settingsBody, _ := json.Marshal(map[string]any{"protectedBranches": []string{"dev"}})
	req, err = http.NewRequest(http.MethodPatch, ts.URL+"/api/repos/"+repoName+"/settings", bytes.NewReader(settingsBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("protect branch status = %d", resp.StatusCode)
	}
	_ = resp.Body.Close()
	runFail(t, work, "git", "push", "origin", ":dev")

	userBody, _ := json.Marshal(map[string]string{"username": "bob", "displayName": "Bob", "email": "bob@example.com", "password": "secret"})
	resp, err = http.Post(ts.URL+"/api/users", "application/json", bytes.NewReader(userBody))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create user status = %d", resp.StatusCode)
	}
	_ = resp.Body.Close()
	loginBody, _ := json.Marshal(map[string]string{"email": "bob@example.com", "password": "secret"})
	resp, err = http.Post(ts.URL+"/api/login", "application/json", bytes.NewReader(loginBody))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	prBody, _ := json.Marshal(map[string]any{"title": "Merge dev", "body": "bring dev into main", "sourceBranch": "dev", "targetBranch": "main", "authorId": 1})
	resp, err = postJSON(t, ts.URL+"/api/repos/"+repoName+"/pulls", token, prBody)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create pull request status = %d", resp.StatusCode)
	}
	_ = resp.Body.Close()
	var pulls []map[string]any
	getJSON(t, ts.URL+"/api/repos/"+repoName+"/pulls", &pulls)
	if len(pulls) != 1 || pulls[0]["title"] != "Merge dev" {
		t.Fatalf("pulls = %#v", pulls)
	}
	diffResp, err := http.Get(ts.URL + "/api/repos/" + repoName + "/pulls/1/diff")
	if err != nil {
		t.Fatal(err)
	}
	diffData, _ := io.ReadAll(diffResp.Body)
	_ = diffResp.Body.Close()
	if diffResp.StatusCode != http.StatusOK || !bytes.Contains(diffData, []byte("DEV.md")) {
		t.Fatalf("pull request diff status = %d diff = %s", diffResp.StatusCode, diffData)
	}
	reviewBody, _ := json.Marshal(map[string]any{"reviewerId": 1, "state": "approved", "body": "looks good"})
	resp, err = postJSON(t, ts.URL+"/api/repos/"+repoName+"/pulls/1/reviews", token, reviewBody)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create review status = %d", resp.StatusCode)
	}
	_ = resp.Body.Close()
	commentBody, _ := json.Marshal(map[string]any{"authorId": 1, "body": "inline note", "filePath": "DEV.md", "lineNumber": 1})
	resp, err = postJSON(t, ts.URL+"/api/repos/"+repoName+"/pulls/1/comments", token, commentBody)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create comment status = %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	var webhookHits atomic.Int64
	webhookServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Minihub-Event") == "pull_request" {
			webhookHits.Add(1)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer webhookServer.Close()

	for path, payload := range map[string]map[string]any{
		"/api/repos/" + repoName + "/issues":   {"title": "Bug", "body": "fix me", "authorId": 1},
		"/api/repos/" + repoName + "/releases": {"tagName": "v0.1.0", "title": "v0.1.0", "notes": "first", "authorId": 1},
		"/api/repos/" + repoName + "/webhooks": {"url": webhookServer.URL, "events": "pull_request", "active": true},
		"/api/repos/" + repoName + "/ci":       {"commitSha": devCommits[0].Hash, "branch": "dev", "status": "success", "provider": "minihub"},
	} {
		payloadBody, _ := json.Marshal(payload)
		resp, err = postJSON(t, ts.URL+path, token, payloadBody)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("POST %s status = %d", path, resp.StatusCode)
		}
		_ = resp.Body.Close()
		var items []map[string]any
		getJSONWithToken(t, ts.URL+path, token, &items)
		if len(items) != 1 {
			t.Fatalf("GET %s = %#v", path, items)
		}
	}

	prBody, _ = json.Marshal(map[string]any{"title": "Webhook PR", "sourceBranch": "dev", "targetBranch": "main", "authorId": 1})
	resp, err = postJSON(t, ts.URL+"/api/repos/"+repoName+"/pulls", token, prBody)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create webhook pull request status = %d", resp.StatusCode)
	}
	_ = resp.Body.Close()
	if webhookHits.Load() == 0 {
		t.Fatal("expected pull_request webhook delivery")
	}

	ciRunBody, _ := json.Marshal(map[string]string{"ref": "dev"})
	resp, err = postJSON(t, ts.URL+"/api/repos/"+repoName+"/ci/run", token, ciRunBody)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("run CI status = %d", resp.StatusCode)
	}
	var ciRun map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&ciRun); err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if ciRun["status"] != "success" {
		t.Fatalf("ci run = %#v", ciRun)
	}

	permissionBody, _ := json.Marshal(map[string]any{"userId": 2, "role": "write"})
	req, err = http.NewRequest(http.MethodPut, ts.URL+"/api/repos/"+repoName+"/permissions", bytes.NewReader(permissionBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("grant permission status = %d", resp.StatusCode)
	}
	_ = resp.Body.Close()
	var permissions []map[string]any
	getJSONWithToken(t, ts.URL+"/api/repos/"+repoName+"/permissions", token, &permissions)
	if len(permissions) != 2 || permissions[0]["role"] != "admin" || permissions[1]["role"] != "write" {
		t.Fatalf("permissions = %#v", permissions)
	}

	clone := filepath.Join(t.TempDir(), "clone")
	run(t, "", "git", "clone", ts.URL+"/git/"+repoName+".git", clone)
	if data, err := os.ReadFile(filepath.Join(clone, "README.md")); err != nil || string(data) != "hello from minihub\n" {
		t.Fatalf("cloned README = %q, %v", data, err)
	}
}

func runFail(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("%s %v unexpectedly succeeded:\n%s", name, args, out)
	}
}

func run(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v failed: %v\n%s", name, args, err, out)
	}
}

func writeFile(t *testing.T, p, content string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func getJSON(t *testing.T, url string, target any) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status = %d", url, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		t.Fatal(err)
	}
}

func getJSONWithToken(t *testing.T, targetURL, token string, target any) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, targetURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status = %d", targetURL, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		t.Fatal(err)
	}
}

func createUserAndLogin(t *testing.T, baseURL, username, email, password string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": username, "displayName": username, "email": email, "password": password})
	resp, err := http.Post(baseURL+"/api/users", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create user status = %d", resp.StatusCode)
	}
	_ = resp.Body.Close()
	loginBody, _ := json.Marshal(map[string]string{"email": email, "password": password})
	resp, err = http.Post(baseURL+"/api/login", "application/json", bytes.NewReader(loginBody))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d", resp.StatusCode)
	}
	var session struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		t.Fatal(err)
	}
	return session.Token
}

func postJSON(t *testing.T, targetURL, token string, body []byte) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, targetURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	return http.DefaultClient.Do(req)
}

func gitURL(t *testing.T, baseURL, username, password, repoName string) string {
	t.Helper()
	parsed, err := url.Parse(baseURL)
	if err != nil {
		t.Fatal(err)
	}
	parsed.User = url.UserPassword(username, password)
	parsed.Path = "/git/" + repoName + ".git"
	return parsed.String()
}

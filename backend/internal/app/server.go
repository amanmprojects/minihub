package app

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	DataDir     string
	DBPath      string
	FrontendDir string
}

type Server struct {
	dataDir     string
	repoRoot    string
	frontendDir string
	store       *Store
	mux         *http.ServeMux
}

type Repository struct {
	Name              string    `json:"name"`
	Description       string    `json:"description"`
	DefaultBranch     string    `json:"defaultBranch"`
	ProtectedBranches []string  `json:"protectedBranches"`
	CloneURL          string    `json:"cloneUrl"`
	CreatedAt         time.Time `json:"createdAt"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

type repoMeta struct {
	Description       string    `json:"description"`
	CreatedAt         time.Time `json:"createdAt"`
	ProtectedBranches []string  `json:"protectedBranches"`
}

var repoNamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)*$`)

func NewServer(cfg Config) (*Server, error) {
	if cfg.DataDir == "" {
		cfg.DataDir = "data"
	}
	repoRoot := filepath.Join(cfg.DataDir, "repos")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		return nil, err
	}
	if err := ensureRepositoryHooks(repoRoot); err != nil {
		return nil, err
	}
	store, err := OpenStore(cfg.DataDir, cfg.DBPath)
	if err != nil {
		return nil, err
	}

	s := &Server{
		dataDir:     cfg.DataDir,
		repoRoot:    repoRoot,
		frontendDir: cfg.FrontendDir,
		store:       store,
		mux:         http.NewServeMux(),
	}
	s.routes()
	return s, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/health", s.health)
	s.mux.HandleFunc("GET /api/users", s.listUsers)
	s.mux.HandleFunc("POST /api/users", s.createUser)
	s.mux.HandleFunc("POST /api/login", s.login)
	s.mux.HandleFunc("GET /api/orgs", s.listOrgs)
	s.mux.HandleFunc("POST /api/orgs", s.createOrg)
	s.mux.HandleFunc("GET /api/repos", s.listRepos)
	s.mux.HandleFunc("POST /api/repos", s.createRepo)
	s.mux.HandleFunc("/api/repos/", s.repoAPI)
	s.mux.HandleFunc("/git/", s.gitHTTP)
	s.mux.HandleFunc("/", s.frontend)
}

func (s *Server) currentUser(r *http.Request) (User, bool) {
	header := r.Header.Get("Authorization")
	token := ""
	if strings.HasPrefix(header, "Bearer ") {
		token = strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
	}
	if token != "" {
		user, err := s.store.UserByToken(token)
		if err == nil {
			return user, true
		}
	}
	user, err := s.store.User(1)
	return user, err == nil
}

func (s *Server) requireRepoRole(w http.ResponseWriter, r *http.Request, repoName string, minRole string) (User, bool) {
	user, ok := s.currentUser(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, errors.New("authentication required"))
		return User{}, false
	}
	role, err := s.store.UserRepoRole(repoName, user.ID)
	if err != nil && user.ID != 1 {
		writeError(w, http.StatusForbidden, errors.New("repository permission required"))
		return User{}, false
	}
	if !roleAllows(role, minRole) {
		writeError(w, http.StatusForbidden, errors.New("insufficient repository permission"))
		return User{}, false
	}
	return user, true
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) listUsers(w http.ResponseWriter, _ *http.Request) {
	users, err := s.store.Users()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, users)
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Username    string `json:"username"`
		DisplayName string `json:"displayName"`
		Email       string `json:"email"`
		Password    string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid JSON body"))
		return
	}
	user, err := s.store.CreateUser(input.Username, input.DisplayName, input.Email, input.Password)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, user)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid JSON body"))
		return
	}
	session, err := s.store.Login(input.Username, input.Password)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	writeJSON(w, http.StatusOK, session)
}

func (s *Server) listOrgs(w http.ResponseWriter, _ *http.Request) {
	orgs, err := s.store.Orgs()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, orgs)
}

func (s *Server) createOrg(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name        string `json:"name"`
		DisplayName string `json:"displayName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid JSON body"))
		return
	}
	org, err := s.store.CreateOrg(input.Name, input.DisplayName)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, org)
}

func (s *Server) listRepos(w http.ResponseWriter, r *http.Request) {
	repos, err := s.repositories(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, repos)
}

func (s *Server) createRepo(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid JSON body"))
		return
	}

	name, err := normalizeRepoName(input.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	repoPath := s.repoPath(name)
	if _, err := os.Stat(repoPath); err == nil {
		writeError(w, http.StatusConflict, errors.New("repository already exists"))
		return
	} else if !errors.Is(err, os.ErrNotExist) {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	if err := os.MkdirAll(filepath.Dir(repoPath), 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := runGit("", "init", "--bare", repoPath); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := runGit(repoPath, "symbolic-ref", "HEAD", "refs/heads/main"); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := runGit(repoPath, "config", "http.receivepack", "true"); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := installProtectedBranchHook(repoPath); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	meta := repoMeta{Description: strings.TrimSpace(input.Description), CreatedAt: time.Now().UTC()}
	if err := writeRepoMeta(repoPath, meta); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := s.store.EnsureRepo(name, meta.Description); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	repo, err := s.repository(r, name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, repo)
}

func (s *Server) repoAPI(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/repos/")
	if rest == "" {
		http.NotFound(w, r)
		return
	}

	switch {
	case r.Method == http.MethodGet && strings.Contains(rest, "/commits/"):
		repoName, commitRef := splitAction(rest, "/commits/")
		s.repoCommit(w, r, repoName, commitRef)
	case strings.Contains(rest, "/pulls/"):
		repoName, tail := splitAction(rest, "/pulls/")
		s.pullRequestAPI(w, r, repoName, tail)
	case r.Method == http.MethodGet && strings.HasSuffix(rest, "/pulls"):
		s.listPullRequests(w, r, strings.TrimSuffix(rest, "/pulls"))
	case r.Method == http.MethodPost && strings.HasSuffix(rest, "/pulls"):
		s.createPullRequest(w, r, strings.TrimSuffix(rest, "/pulls"))
	case r.Method == http.MethodGet && strings.HasSuffix(rest, "/issues"):
		s.listIssues(w, r, strings.TrimSuffix(rest, "/issues"))
	case r.Method == http.MethodPost && strings.HasSuffix(rest, "/issues"):
		s.createIssue(w, r, strings.TrimSuffix(rest, "/issues"))
	case r.Method == http.MethodGet && strings.HasSuffix(rest, "/releases"):
		s.listReleases(w, r, strings.TrimSuffix(rest, "/releases"))
	case r.Method == http.MethodPost && strings.HasSuffix(rest, "/releases"):
		s.createRelease(w, r, strings.TrimSuffix(rest, "/releases"))
	case r.Method == http.MethodGet && strings.HasSuffix(rest, "/webhooks"):
		s.listWebhooks(w, r, strings.TrimSuffix(rest, "/webhooks"))
	case r.Method == http.MethodPost && strings.HasSuffix(rest, "/webhooks"):
		s.createWebhook(w, r, strings.TrimSuffix(rest, "/webhooks"))
	case r.Method == http.MethodGet && strings.HasSuffix(rest, "/ci"):
		s.listCIRuns(w, r, strings.TrimSuffix(rest, "/ci"))
	case r.Method == http.MethodPost && strings.HasSuffix(rest, "/ci"):
		s.createCIRun(w, r, strings.TrimSuffix(rest, "/ci"))
	case r.Method == http.MethodPost && strings.HasSuffix(rest, "/ci/run"):
		s.runCI(w, r, strings.TrimSuffix(rest, "/ci/run"))
	case r.Method == http.MethodGet && strings.HasSuffix(rest, "/permissions"):
		s.listPermissions(w, r, strings.TrimSuffix(rest, "/permissions"))
	case r.Method == http.MethodPut && strings.HasSuffix(rest, "/permissions"):
		s.grantPermission(w, r, strings.TrimSuffix(rest, "/permissions"))
	case r.Method == http.MethodGet && strings.HasSuffix(rest, "/commits"):
		s.repoCommits(w, r, strings.TrimSuffix(rest, "/commits"))
	case r.Method == http.MethodGet && strings.HasSuffix(rest, "/branches"):
		s.repoBranches(w, r, strings.TrimSuffix(rest, "/branches"))
	case r.Method == http.MethodPost && strings.HasSuffix(rest, "/branches"):
		s.createBranch(w, r, strings.TrimSuffix(rest, "/branches"))
	case r.Method == http.MethodDelete && strings.HasSuffix(rest, "/branches"):
		s.deleteBranch(w, r, strings.TrimSuffix(rest, "/branches"))
	case r.Method == http.MethodGet && strings.HasSuffix(rest, "/settings"):
		s.repoSettings(w, r, strings.TrimSuffix(rest, "/settings"))
	case r.Method == http.MethodPatch && strings.HasSuffix(rest, "/settings"):
		s.updateRepoSettings(w, r, strings.TrimSuffix(rest, "/settings"))
	case r.Method == http.MethodGet && strings.HasSuffix(rest, "/tree"):
		s.repoTree(w, r, strings.TrimSuffix(rest, "/tree"))
	case r.Method == http.MethodGet && strings.HasSuffix(rest, "/blob"):
		s.repoBlob(w, r, strings.TrimSuffix(rest, "/blob"))
	case r.Method == http.MethodGet:
		s.getRepo(w, r, rest)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) getRepo(w http.ResponseWriter, r *http.Request, rawName string) {
	name, err := normalizeRepoName(rawName)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	repo, err := s.repository(r, name)
	if errors.Is(err, os.ErrNotExist) {
		writeError(w, http.StatusNotFound, errors.New("repository not found"))
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, repo)
}

func (s *Server) listPullRequests(w http.ResponseWriter, _ *http.Request, rawName string) {
	name, err := normalizeRepoName(rawName)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	items, err := s.store.PullRequests(name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) createPullRequest(w http.ResponseWriter, r *http.Request, rawName string) {
	name, err := normalizeRepoName(rawName)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	var input struct {
		Title        string `json:"title"`
		Body         string `json:"body"`
		SourceBranch string `json:"sourceBranch"`
		TargetBranch string `json:"targetBranch"`
		AuthorID     int64  `json:"authorId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid JSON body"))
		return
	}
	if input.Title == "" || input.SourceBranch == "" || input.TargetBranch == "" {
		writeError(w, http.StatusBadRequest, errors.New("title, sourceBranch, and targetBranch are required"))
		return
	}
	user, ok := s.requireRepoRole(w, r, name, "write")
	if !ok {
		return
	}
	if input.AuthorID == 0 {
		input.AuthorID = user.ID
	}
	pr, err := s.store.CreatePullRequest(name, input.Title, input.Body, input.SourceBranch, input.TargetBranch, input.AuthorID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.deliverWebhooks(name, "pull_request", pr)
	writeJSON(w, http.StatusCreated, pr)
}

func (s *Server) pullRequestAPI(w http.ResponseWriter, r *http.Request, repoName, tail string) {
	numberPart, action, _ := strings.Cut(tail, "/")
	number, err := strconv.ParseInt(numberPart, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, errors.New("pull request number is invalid"))
		return
	}
	switch {
	case r.Method == http.MethodGet && action == "":
		pr, err := s.store.PullRequest(repoName, number)
		if err != nil {
			writeError(w, http.StatusNotFound, errors.New("pull request not found"))
			return
		}
		writeJSON(w, http.StatusOK, pr)
	case r.Method == http.MethodPatch && action == "":
		var input struct {
			Status string `json:"status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeError(w, http.StatusBadRequest, errors.New("invalid JSON body"))
			return
		}
		pr, err := s.store.UpdatePullRequestStatus(repoName, number, input.Status, "")
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, pr)
	case r.Method == http.MethodGet && action == "diff":
		s.pullRequestDiff(w, r, repoName, number)
	case r.Method == http.MethodPost && action == "merge":
		if _, ok := s.requireRepoRole(w, r, repoName, "maintain"); !ok {
			return
		}
		s.mergePullRequest(w, r, repoName, number)
	case r.Method == http.MethodGet && action == "reviews":
		items, err := s.store.Reviews(repoName, number)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, items)
	case r.Method == http.MethodPost && action == "reviews":
		var input struct {
			ReviewerID int64  `json:"reviewerId"`
			State      string `json:"state"`
			Body       string `json:"body"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeError(w, http.StatusBadRequest, errors.New("invalid JSON body"))
			return
		}
		user, ok := s.requireRepoRole(w, r, repoName, "triage")
		if !ok {
			return
		}
		if input.ReviewerID == 0 {
			input.ReviewerID = user.ID
		}
		item, err := s.store.CreateReview(repoName, number, input.ReviewerID, input.State, input.Body)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusCreated, item)
	case r.Method == http.MethodGet && action == "comments":
		pr, err := s.store.PullRequest(repoName, number)
		if err != nil {
			writeError(w, http.StatusNotFound, errors.New("pull request not found"))
			return
		}
		items, err := s.store.Comments(repoName, "pull_request", asInt64(pr["id"]))
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, items)
	case r.Method == http.MethodPost && action == "comments":
		pr, err := s.store.PullRequest(repoName, number)
		if err != nil {
			writeError(w, http.StatusNotFound, errors.New("pull request not found"))
			return
		}
		s.createCommentForSubject(w, r, repoName, "pull_request", asInt64(pr["id"]))
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) pullRequestDiff(w http.ResponseWriter, _ *http.Request, repoName string, number int64) {
	pr, err := s.store.PullRequest(repoName, number)
	if err != nil {
		writeError(w, http.StatusNotFound, errors.New("pull request not found"))
		return
	}
	target := fmt.Sprint(pr["targetBranch"])
	source := fmt.Sprint(pr["sourceBranch"])
	out, err := gitOutput(s.repoPath(repoName), "diff", "--find-renames", target+"..."+source)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, out)
}

func (s *Server) mergePullRequest(w http.ResponseWriter, _ *http.Request, repoName string, number int64) {
	// The caller is authorized in the HTTP wrapper before this method mutates refs.
	pr, err := s.store.PullRequest(repoName, number)
	if err != nil {
		writeError(w, http.StatusNotFound, errors.New("pull request not found"))
		return
	}
	target := fmt.Sprint(pr["targetBranch"])
	source := fmt.Sprint(pr["sourceBranch"])
	repoPath := s.repoPath(repoName)
	targetSHA := strings.TrimSpace(mustGit(repoPath, "rev-parse", target))
	sourceSHA := strings.TrimSpace(mustGit(repoPath, "rev-parse", source))
	if targetSHA == "" || sourceSHA == "" {
		writeError(w, http.StatusBadRequest, errors.New("source or target branch not found"))
		return
	}
	tree, err := gitOutput(repoPath, "merge-tree", "--write-tree", target, source)
	if err != nil {
		writeError(w, http.StatusConflict, fmt.Errorf("merge conflict: %w", err))
		return
	}
	message := fmt.Sprintf("Merge pull request #%d\n\n%s", number, fmt.Sprint(pr["title"]))
	commit, err := gitOutput(repoPath, "commit-tree", strings.TrimSpace(tree), "-p", targetSHA, "-p", sourceSHA, "-m", message)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	mergeSHA := strings.TrimSpace(commit)
	if err := runGit(repoPath, "update-ref", "refs/heads/"+target, mergeSHA, targetSHA); err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	updated, err := s.store.UpdatePullRequestStatus(repoName, number, "merged", mergeSHA)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.deliverWebhooks(repoName, "pull_request", updated)
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) listIssues(w http.ResponseWriter, _ *http.Request, rawName string) {
	name, err := normalizeRepoName(rawName)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	items, err := s.store.Issues(name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) createIssue(w http.ResponseWriter, r *http.Request, rawName string) {
	name, err := normalizeRepoName(rawName)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	var input struct {
		Title      string `json:"title"`
		Body       string `json:"body"`
		AuthorID   int64  `json:"authorId"`
		AssigneeID int64  `json:"assigneeId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid JSON body"))
		return
	}
	user, ok := s.requireRepoRole(w, r, name, "triage")
	if !ok {
		return
	}
	if input.AuthorID == 0 {
		input.AuthorID = user.ID
	}
	item, err := s.store.CreateIssue(name, input.Title, input.Body, input.AuthorID, input.AssigneeID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) listReleases(w http.ResponseWriter, _ *http.Request, rawName string) {
	name, err := normalizeRepoName(rawName)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	items, err := s.store.Releases(name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) createRelease(w http.ResponseWriter, r *http.Request, rawName string) {
	name, err := normalizeRepoName(rawName)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	var input struct {
		TagName  string `json:"tagName"`
		Title    string `json:"title"`
		Notes    string `json:"notes"`
		AuthorID int64  `json:"authorId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid JSON body"))
		return
	}
	user, ok := s.requireRepoRole(w, r, name, "write")
	if !ok {
		return
	}
	if input.AuthorID == 0 {
		input.AuthorID = user.ID
	}
	item, err := s.store.CreateRelease(name, input.TagName, input.Title, input.Notes, input.AuthorID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.deliverWebhooks(name, "release", item)
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) listWebhooks(w http.ResponseWriter, _ *http.Request, rawName string) {
	name, err := normalizeRepoName(rawName)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	items, err := s.store.Webhooks(name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) createWebhook(w http.ResponseWriter, r *http.Request, rawName string) {
	name, err := normalizeRepoName(rawName)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	var input struct {
		URL    string `json:"url"`
		Events string `json:"events"`
		Active bool   `json:"active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid JSON body"))
		return
	}
	if _, ok := s.requireRepoRole(w, r, name, "admin"); !ok {
		return
	}
	item, err := s.store.CreateWebhook(name, input.URL, defaultString(input.Events, "push,pull_request"), input.Active)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) listCIRuns(w http.ResponseWriter, _ *http.Request, rawName string) {
	name, err := normalizeRepoName(rawName)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	items, err := s.store.CIRuns(name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) createCIRun(w http.ResponseWriter, r *http.Request, rawName string) {
	name, err := normalizeRepoName(rawName)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	var input struct {
		CommitSHA string `json:"commitSha"`
		Branch    string `json:"branch"`
		Status    string `json:"status"`
		Provider  string `json:"provider"`
		LogURL    string `json:"logUrl"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid JSON body"))
		return
	}
	if _, ok := s.requireRepoRole(w, r, name, "write"); !ok {
		return
	}
	item, err := s.store.CreateCIRun(name, input.CommitSHA, input.Branch, defaultString(input.Status, "queued"), input.Provider, input.LogURL)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) runCI(w http.ResponseWriter, r *http.Request, rawName string) {
	name, err := normalizeRepoName(rawName)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if _, ok := s.requireRepoRole(w, r, name, "write"); !ok {
		return
	}
	var input struct {
		Ref string `json:"ref"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid JSON body"))
		return
	}
	ref := defaultString(input.Ref, s.defaultBranch(name))
	sha := strings.TrimSpace(mustGit(s.repoPath(name), "rev-parse", ref))
	if sha == "" {
		writeError(w, http.StatusBadRequest, errors.New("ref not found"))
		return
	}
	run, err := s.store.CreateCIRun(name, sha, ref, "running", "minihub", "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	status, logPath := s.executeCI(name, ref, asInt64(run["id"]))
	updated, err := s.store.UpdateCIRun(asInt64(run["id"]), status, logPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.deliverWebhooks(name, "ci", updated)
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) listPermissions(w http.ResponseWriter, _ *http.Request, rawName string) {
	name, err := normalizeRepoName(rawName)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	items, err := s.store.RepoPermissions(name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) grantPermission(w http.ResponseWriter, r *http.Request, rawName string) {
	name, err := normalizeRepoName(rawName)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if _, ok := s.requireRepoRole(w, r, name, "admin"); !ok {
		return
	}
	var input struct {
		UserID int64  `json:"userId"`
		Role   string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid JSON body"))
		return
	}
	item, err := s.store.GrantRepoPermission(name, input.UserID, input.Role)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) createCommentForSubject(w http.ResponseWriter, r *http.Request, repoName, subjectType string, subjectID int64) {
	var input struct {
		AuthorID   int64  `json:"authorId"`
		Body       string `json:"body"`
		FilePath   string `json:"filePath"`
		LineNumber *int64 `json:"lineNumber"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid JSON body"))
		return
	}
	user, ok := s.requireRepoRole(w, r, repoName, "triage")
	if !ok {
		return
	}
	if input.AuthorID == 0 {
		input.AuthorID = user.ID
	}
	item, err := s.store.CreateComment(repoName, subjectType, subjectID, input.AuthorID, input.Body, input.FilePath, input.LineNumber)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) repoCommits(w http.ResponseWriter, r *http.Request, rawName string) {
	name, err := normalizeRepoName(rawName)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	repoPath := s.repoPath(name)
	if !isDir(repoPath) {
		writeError(w, http.StatusNotFound, errors.New("repository not found"))
		return
	}

	ref := defaultString(r.URL.Query().Get("ref"), "HEAD")
	out, err := gitOutput(repoPath, "log", ref, "--max-count=30", "--date=iso-strict", "--pretty=format:%H%x00%an%x00%ae%x00%ad%x00%s")
	if err != nil {
		if strings.Contains(err.Error(), "does not have any commits") {
			writeJSON(w, http.StatusOK, []any{})
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	type commit struct {
		Hash        string `json:"hash"`
		Author      string `json:"author"`
		AuthorEmail string `json:"authorEmail"`
		Date        string `json:"date"`
		Subject     string `json:"subject"`
	}
	var commits []commit
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		parts := strings.SplitN(line, "\x00", 5)
		if len(parts) == 5 {
			commits = append(commits, commit{Hash: parts[0], Author: parts[1], AuthorEmail: parts[2], Date: parts[3], Subject: parts[4]})
		}
	}
	writeJSON(w, http.StatusOK, commits)
}

func (s *Server) repoCommit(w http.ResponseWriter, r *http.Request, rawName, commitRef string) {
	name, err := normalizeRepoName(rawName)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if !isDir(s.repoPath(name)) {
		writeError(w, http.StatusNotFound, errors.New("repository not found"))
		return
	}
	commitRef = strings.TrimSpace(commitRef)
	if commitRef == "" {
		writeError(w, http.StatusBadRequest, errors.New("commit ref is required"))
		return
	}

	format := "%H%x00%an%x00%ae%x00%ad%x00%s%x00%B"
	out, err := gitOutput(s.repoPath(name), "show", "--quiet", "--date=iso-strict", "--format="+format, commitRef)
	if err != nil {
		writeError(w, http.StatusNotFound, errors.New("commit not found"))
		return
	}
	parts := strings.SplitN(strings.TrimRight(out, "\n"), "\x00", 6)
	if len(parts) != 6 {
		writeError(w, http.StatusInternalServerError, errors.New("unable to parse commit"))
		return
	}
	diff, err := gitOutput(s.repoPath(name), "show", "--format=", "--find-renames", "--stat", "--patch", commitRef)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"hash":        parts[0],
		"author":      parts[1],
		"authorEmail": parts[2],
		"date":        parts[3],
		"subject":     parts[4],
		"body":        strings.TrimSpace(parts[5]),
		"diff":        strings.TrimSpace(diff),
	})
	_ = r
}

func (s *Server) repoBranches(w http.ResponseWriter, _ *http.Request, rawName string) {
	name, err := normalizeRepoName(rawName)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	repoPath := s.repoPath(name)
	if !isDir(repoPath) {
		writeError(w, http.StatusNotFound, errors.New("repository not found"))
		return
	}

	out, err := gitOutput(repoPath, "for-each-ref", "refs/heads", "--sort=refname", "--format=%(refname:short)%00%(objectname)%00%(committerdate:iso-strict)")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defaultBranch := s.defaultBranch(name)
	protected := readRepoMeta(repoPath).ProtectedBranches
	type branch struct {
		Name      string `json:"name"`
		Commit    string `json:"commit"`
		UpdatedAt string `json:"updatedAt"`
		Default   bool   `json:"default"`
		Protected bool   `json:"protected"`
	}
	var branches []branch
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\x00", 3)
		if len(parts) == 3 {
			branches = append(branches, branch{
				Name:      parts[0],
				Commit:    parts[1],
				UpdatedAt: parts[2],
				Default:   parts[0] == defaultBranch,
				Protected: containsString(protected, parts[0]),
			})
		}
	}
	writeJSON(w, http.StatusOK, branches)
}

func (s *Server) createBranch(w http.ResponseWriter, r *http.Request, rawName string) {
	name, err := normalizeRepoName(rawName)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if _, ok := s.requireRepoRole(w, r, name, "write"); !ok {
		return
	}
	var input struct {
		Name   string `json:"name"`
		Source string `json:"source"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid JSON body"))
		return
	}
	branch, err := normalizeBranchName(input.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	source := defaultString(strings.TrimSpace(input.Source), "HEAD")
	if err := runGit(s.repoPath(name), "branch", branch, source); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.repoBranches(w, r, name)
}

func (s *Server) deleteBranch(w http.ResponseWriter, r *http.Request, rawName string) {
	name, err := normalizeRepoName(rawName)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if _, ok := s.requireRepoRole(w, r, name, "write"); !ok {
		return
	}
	branch, err := normalizeBranchName(r.URL.Query().Get("name"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if branch == s.defaultBranch(name) {
		writeError(w, http.StatusBadRequest, errors.New("cannot delete the default branch"))
		return
	}
	meta := readRepoMeta(s.repoPath(name))
	if containsString(meta.ProtectedBranches, branch) {
		writeError(w, http.StatusForbidden, errors.New("cannot delete a protected branch"))
		return
	}
	if err := runGit(s.repoPath(name), "branch", "-D", branch); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) repoSettings(w http.ResponseWriter, r *http.Request, rawName string) {
	name, err := normalizeRepoName(rawName)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	repo, err := s.repository(r, name)
	if err != nil {
		writeError(w, http.StatusNotFound, errors.New("repository not found"))
		return
	}
	writeJSON(w, http.StatusOK, repo)
}

func (s *Server) updateRepoSettings(w http.ResponseWriter, r *http.Request, rawName string) {
	name, err := normalizeRepoName(rawName)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	repoPath := s.repoPath(name)
	if !isDir(repoPath) {
		writeError(w, http.StatusNotFound, errors.New("repository not found"))
		return
	}
	if _, ok := s.requireRepoRole(w, r, name, "admin"); !ok {
		return
	}
	var input struct {
		Description       *string  `json:"description"`
		ProtectedBranches []string `json:"protectedBranches"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid JSON body"))
		return
	}
	meta := readRepoMeta(repoPath)
	if input.Description != nil {
		meta.Description = strings.TrimSpace(*input.Description)
	}
	if input.ProtectedBranches != nil {
		var branches []string
		for _, branch := range input.ProtectedBranches {
			normalized, err := normalizeBranchName(branch)
			if err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			if !containsString(branches, normalized) {
				branches = append(branches, normalized)
			}
		}
		meta.ProtectedBranches = branches
	}
	if err := writeRepoMeta(repoPath, meta); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	repo, err := s.repository(r, name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, repo)
}

func (s *Server) repoTree(w http.ResponseWriter, r *http.Request, rawName string) {
	name, err := normalizeRepoName(rawName)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	ref := defaultString(r.URL.Query().Get("ref"), "HEAD")
	treePath := strings.Trim(r.URL.Query().Get("path"), "/")
	spec := ref
	if treePath != "" {
		spec += ":" + treePath
	}

	out, err := gitOutput(s.repoPath(name), "ls-tree", spec)
	if err != nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	type entry struct {
		Mode string `json:"mode"`
		Type string `json:"type"`
		Hash string `json:"hash"`
		Name string `json:"name"`
		Path string `json:"path"`
	}
	var entries []entry
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		header, namePart, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		fields := strings.Fields(header)
		if len(fields) == 3 {
			fullPath := path.Join(treePath, namePart)
			entries = append(entries, entry{Mode: fields[0], Type: fields[1], Hash: fields[2], Name: namePart, Path: fullPath})
		}
	}
	writeJSON(w, http.StatusOK, entries)
}

func (s *Server) repoBlob(w http.ResponseWriter, r *http.Request, rawName string) {
	name, err := normalizeRepoName(rawName)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	ref := defaultString(r.URL.Query().Get("ref"), "HEAD")
	blobPath := strings.Trim(r.URL.Query().Get("path"), "/")
	if blobPath == "" {
		writeError(w, http.StatusBadRequest, errors.New("path is required"))
		return
	}
	out, err := gitOutput(s.repoPath(name), "show", ref+":"+blobPath)
	if err != nil {
		writeError(w, http.StatusNotFound, errors.New("file not found"))
		return
	}
	contentType := mime.TypeByExtension(filepath.Ext(blobPath))
	if contentType == "" {
		contentType = "text/plain; charset=utf-8"
	}
	w.Header().Set("Content-Type", contentType)
	_, _ = io.WriteString(w, out)
}

func (s *Server) gitHTTP(w http.ResponseWriter, r *http.Request) {
	pathInfo := strings.TrimPrefix(r.URL.Path, "/git")
	repoName, err := repoNameFromGitPath(pathInfo)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if !isDir(s.repoPath(repoName)) {
		writeError(w, http.StatusNotFound, errors.New("repository not found"))
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	cmd := exec.Command("git", "http-backend")
	cmd.Stdin = bytes.NewReader(body)
	cmd.Env = append(os.Environ(),
		"GIT_PROJECT_ROOT="+s.repoRoot,
		"GIT_HTTP_EXPORT_ALL=1",
		"PATH_INFO="+pathInfo,
		"REQUEST_METHOD="+r.Method,
		"QUERY_STRING="+r.URL.RawQuery,
		"CONTENT_TYPE="+r.Header.Get("Content-Type"),
		"CONTENT_LENGTH="+strconv.Itoa(len(body)),
		"REMOTE_ADDR="+remoteAddr(r),
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("git http-backend failed: %s", strings.TrimSpace(stderr.String())))
		return
	}
	writeCGIResponse(w, stdout.Bytes())
}

func (s *Server) deliverWebhooks(repoName, event string, payload any) {
	webhooks, err := s.store.ActiveWebhooks(repoName, event)
	if err != nil {
		return
	}
	for _, hook := range webhooks {
		hookURL := fmt.Sprint(hook["url"])
		parsed, err := url.Parse(hookURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			_ = s.store.RecordWebhookDelivery(asInt64(hook["id"]), event, "failure", "invalid webhook URL")
			continue
		}
		body, _ := json.Marshal(map[string]any{"event": event, "repository": repoName, "payload": payload})
		req, err := http.NewRequest(http.MethodPost, hookURL, bytes.NewReader(body))
		if err != nil {
			_ = s.store.RecordWebhookDelivery(asInt64(hook["id"]), event, "failure", err.Error())
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Minihub-Event", event)
		client := http.Client{Timeout: 3 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			_ = s.store.RecordWebhookDelivery(asInt64(hook["id"]), event, "failure", err.Error())
			continue
		}
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
		status := "success"
		if resp.StatusCode >= 300 {
			status = "failure"
		}
		_ = s.store.RecordWebhookDelivery(asInt64(hook["id"]), event, status, fmt.Sprintf("%d %s", resp.StatusCode, strings.TrimSpace(string(data))))
	}
}

func (s *Server) executeCI(repoName, ref string, runID int64) (string, string) {
	workDir, err := os.MkdirTemp("", fmt.Sprintf("minihub-ci-%d-", runID))
	if err != nil {
		return "failure", err.Error()
	}
	defer os.RemoveAll(workDir)
	repoPath := s.repoPath(repoName)
	logPath := filepath.Join(s.dataDir, "ci", fmt.Sprintf("%d.log", runID))
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return "failure", err.Error()
	}
	var log bytes.Buffer
	cmd := exec.Command("git", "--git-dir", repoPath, "--work-tree", workDir, "checkout", "-f", ref)
	cmd.Stdout = &log
	cmd.Stderr = &log
	if err := cmd.Run(); err != nil {
		_ = os.WriteFile(logPath, log.Bytes(), 0o644)
		return "failure", logPath
	}
	script := filepath.Join(workDir, ".minihub", "ci.sh")
	if !isFile(script) {
		_, _ = log.WriteString("No .minihub/ci.sh found; marking run successful.\n")
		_ = os.WriteFile(logPath, log.Bytes(), 0o644)
		return "success", logPath
	}
	cmd = exec.Command("sh", script)
	cmd.Dir = workDir
	cmd.Stdout = &log
	cmd.Stderr = &log
	status := "success"
	if err := cmd.Run(); err != nil {
		status = "failure"
	}
	_ = os.WriteFile(logPath, log.Bytes(), 0o644)
	return status, logPath
}

func (s *Server) frontend(w http.ResponseWriter, r *http.Request) {
	if s.frontendDir == "" {
		http.NotFound(w, r)
		return
	}
	file := filepath.Join(s.frontendDir, filepath.Clean(r.URL.Path))
	if r.URL.Path == "/" || !isFile(file) {
		file = filepath.Join(s.frontendDir, "index.html")
	}
	http.ServeFile(w, r, file)
}

func (s *Server) repositories(r *http.Request) ([]Repository, error) {
	var repos []Repository
	if !isDir(s.repoRoot) {
		return repos, nil
	}
	err := filepath.WalkDir(s.repoRoot, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() || !strings.HasSuffix(d.Name(), ".git") {
			return nil
		}
		if !isFile(filepath.Join(p, "HEAD")) {
			return nil
		}
		rel, err := filepath.Rel(s.repoRoot, p)
		if err != nil {
			return err
		}
		name := strings.TrimSuffix(filepath.ToSlash(rel), ".git")
		repo, err := s.repository(r, name)
		if err != nil {
			return err
		}
		repos = append(repos, repo)
		return filepath.SkipDir
	})
	sort.Slice(repos, func(i, j int) bool {
		return repos[i].UpdatedAt.After(repos[j].UpdatedAt)
	})
	return repos, err
}

func (s *Server) repository(r *http.Request, name string) (Repository, error) {
	repoPath := s.repoPath(name)
	info, err := os.Stat(repoPath)
	if err != nil {
		return Repository{}, err
	}
	meta := readRepoMeta(repoPath)
	if meta.CreatedAt.IsZero() {
		meta.CreatedAt = info.ModTime().UTC()
	}
	return Repository{
		Name:              name,
		Description:       meta.Description,
		DefaultBranch:     s.defaultBranch(name),
		ProtectedBranches: meta.ProtectedBranches,
		CloneURL:          absoluteURL(r, "/git/"+name+".git"),
		CreatedAt:         meta.CreatedAt,
		UpdatedAt:         info.ModTime().UTC(),
	}, nil
}

func (s *Server) repoPath(name string) string {
	return filepath.Join(s.repoRoot, filepath.FromSlash(name)+".git")
}

func (s *Server) defaultBranch(name string) string {
	out, err := gitOutput(s.repoPath(name), "symbolic-ref", "--short", "HEAD")
	if err != nil {
		return "main"
	}
	return strings.TrimSpace(out)
}

func normalizeRepoName(raw string) (string, error) {
	name := strings.TrimSpace(strings.TrimSuffix(raw, ".git"))
	name = strings.Trim(name, "/")
	if name == "" {
		return "", errors.New("repository name is required")
	}
	if !repoNamePattern.MatchString(name) || strings.Contains(name, "..") {
		return "", errors.New("repository name may contain letters, numbers, dots, underscores, dashes, and slashes")
	}
	return name, nil
}

func normalizeBranchName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", errors.New("branch name is required")
	}
	if strings.HasPrefix(name, "-") || strings.Contains(name, "..") || strings.ContainsAny(name, " ~^:?*[\\") || strings.HasSuffix(name, ".") || strings.HasSuffix(name, ".lock") || strings.Contains(name, "@{") {
		return "", errors.New("invalid branch name")
	}
	return name, nil
}

func repoNameFromGitPath(pathInfo string) (string, error) {
	trimmed := strings.TrimPrefix(pathInfo, "/")
	idx := strings.Index(trimmed, ".git")
	if idx < 0 {
		return "", errors.New("git URL must include a .git repository path")
	}
	return normalizeRepoName(trimmed[:idx])
}

func splitAction(rest, marker string) (string, string) {
	repoName, value, _ := strings.Cut(rest, marker)
	return repoName, value
}

func runGit(dir string, args ...string) error {
	_, err := gitOutput(dir, args...)
	return err
}

func gitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func mustGit(dir string, args ...string) string {
	out, err := gitOutput(dir, args...)
	if err != nil {
		return ""
	}
	return out
}

func asInt64(value any) int64 {
	switch v := value.(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		return int64(v)
	case string:
		n, _ := strconv.ParseInt(v, 10, 64)
		return n
	default:
		return 0
	}
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func roleAllows(role, minRole string) bool {
	return roleRank(role) >= roleRank(minRole)
}

func roleRank(role string) int {
	switch role {
	case "admin":
		return 5
	case "maintain":
		return 4
	case "write":
		return 3
	case "triage":
		return 2
	case "read":
		return 1
	default:
		return 0
	}
}

func writeRepoMeta(repoPath string, meta repoMeta) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(repoPath, "minihub.json"), data, 0o644); err != nil {
		return err
	}
	return writeProtectedBranchesFile(repoPath, meta.ProtectedBranches)
}

func readRepoMeta(repoPath string) repoMeta {
	var meta repoMeta
	data, err := os.ReadFile(filepath.Join(repoPath, "minihub.json"))
	if err == nil {
		_ = json.Unmarshal(data, &meta)
	}
	return meta
}

func installProtectedBranchHook(repoPath string) error {
	hook := `#!/bin/sh
protected="$GIT_DIR/minihub-protected-branches"
[ -f "$protected" ] || exit 0
zero="0000000000000000000000000000000000000000"
while read old new refname
do
  branch=${refname#refs/heads/}
  [ "$branch" = "$refname" ] && continue
  grep -Fxq "$branch" "$protected" || continue
  if [ "$new" = "$zero" ]; then
    echo "Minihub: protected branch '$branch' cannot be deleted" >&2
    exit 1
  fi
  if [ "$old" != "$zero" ] && ! git merge-base --is-ancestor "$old" "$new"; then
    echo "Minihub: protected branch '$branch' requires fast-forward updates" >&2
    exit 1
  fi
done
exit 0
`
	path := filepath.Join(repoPath, "hooks", "pre-receive")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(hook), 0o755)
}

func writeProtectedBranchesFile(repoPath string, branches []string) error {
	var b strings.Builder
	for _, branch := range branches {
		if branch != "" {
			b.WriteString(branch)
			b.WriteByte('\n')
		}
	}
	return os.WriteFile(filepath.Join(repoPath, "minihub-protected-branches"), []byte(b.String()), 0o644)
}

func ensureRepositoryHooks(repoRoot string) error {
	if !isDir(repoRoot) {
		return nil
	}
	return filepath.WalkDir(repoRoot, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() || !strings.HasSuffix(d.Name(), ".git") {
			return nil
		}
		if isFile(filepath.Join(p, "HEAD")) {
			meta := readRepoMeta(p)
			if err := installProtectedBranchHook(p); err != nil {
				return err
			}
			if err := writeProtectedBranchesFile(p, meta.ProtectedBranches); err != nil {
				return err
			}
			return filepath.SkipDir
		}
		return nil
	})
}

func writeCGIResponse(w http.ResponseWriter, data []byte) {
	reader := bufio.NewReader(bytes.NewReader(data))
	status := http.StatusOK
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if strings.EqualFold(key, "Status") {
			fields := strings.Fields(value)
			if len(fields) > 0 {
				if code, err := strconv.Atoi(fields[0]); err == nil {
					status = code
				}
			}
			continue
		}
		w.Header().Add(key, value)
	}
	w.WriteHeader(status)
	_, _ = io.Copy(w, reader)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func absoluteURL(r *http.Request, p string) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	}
	return scheme + "://" + r.Host + p
}

func remoteAddr(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func isDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

func isFile(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

package app

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

type User struct {
	ID          int64  `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	Email       string `json:"email"`
	CreatedAt   string `json:"createdAt"`
}

type Permission struct {
	RepoID    int64  `json:"repoId"`
	UserID    int64  `json:"userId"`
	Username  string `json:"username"`
	Role      string `json:"role"`
	CreatedAt string `json:"createdAt"`
}

func OpenStore(dataDir, dbPath string) (*Store, error) {
	if dbPath == "" {
		dbPath = filepath.Join(dataDir, "minihub.db")
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := s.ensureSystemUser(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(sqliteSchema)
	return err
}

func (s *Store) ensureSystemUser() error {
	_, err := s.db.Exec(`INSERT OR IGNORE INTO users (id, username, display_name, email, password_hash) VALUES (1, 'dev', 'Development User', 'dev@minihub.local', '')`)
	return err
}

func (s *Store) EnsureRepo(name, description string) error {
	_, err := s.db.Exec(`INSERT OR IGNORE INTO repo_records (id, owner_type, owner_id, name, description) VALUES ((SELECT COALESCE(MAX(id), 0) + 1 FROM repo_records), 'user', 1, ?, ?)`, name, description)
	return err
}

func (s *Store) RepoID(name string) (int64, error) {
	var id int64
	err := s.db.QueryRow(`SELECT id FROM repo_records WHERE owner_type = 'user' AND owner_id = 1 AND name = ?`, name).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		if err := s.EnsureRepo(name, ""); err != nil {
			return 0, err
		}
		err = s.db.QueryRow(`SELECT id FROM repo_records WHERE owner_type = 'user' AND owner_id = 1 AND name = ?`, name).Scan(&id)
	}
	return id, err
}

func (s *Store) CreateUser(username, displayName, email, password string) (User, error) {
	if username == "" || email == "" {
		return User{}, errors.New("username and email are required")
	}
	hash := hashPassword(password)
	res, err := s.db.Exec(`INSERT INTO users (username, display_name, email, password_hash) VALUES (?, ?, ?, ?)`, username, displayName, email, hash)
	if err != nil {
		return User{}, err
	}
	id, _ := res.LastInsertId()
	return s.User(id)
}

func (s *Store) Users() ([]User, error) {
	rows, err := s.db.Query(`SELECT id, username, display_name, email, created_at FROM users ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []User
	for rows.Next() {
		var user User
		if err := rows.Scan(&user.ID, &user.Username, &user.DisplayName, &user.Email, &user.CreatedAt); err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, rows.Err()
}

func (s *Store) User(id int64) (User, error) {
	var user User
	err := s.db.QueryRow(`SELECT id, username, display_name, email, created_at FROM users WHERE id = ?`, id).Scan(&user.ID, &user.Username, &user.DisplayName, &user.Email, &user.CreatedAt)
	return user, err
}

func (s *Store) Login(username, password string) (map[string]any, error) {
	var id int64
	var hash string
	if err := s.db.QueryRow(`SELECT id, password_hash FROM users WHERE username = ?`, username).Scan(&id, &hash); err != nil {
		return nil, err
	}
	if hash != hashPassword(password) {
		return nil, errors.New("invalid credentials")
	}
	token := randomToken()
	expires := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	if _, err := s.db.Exec(`INSERT INTO sessions (token, user_id, expires_at) VALUES (?, ?, ?)`, token, id, expires); err != nil {
		return nil, err
	}
	user, err := s.User(id)
	if err != nil {
		return nil, err
	}
	return map[string]any{"token": token, "user": user, "expiresAt": expires}, nil
}

func (s *Store) CheckPassword(username, password string) (User, bool) {
	var id int64
	var hash string
	if err := s.db.QueryRow(`SELECT id, password_hash FROM users WHERE username = ?`, username).Scan(&id, &hash); err != nil {
		return User{}, false
	}
	if hash != hashPassword(password) {
		return User{}, false
	}
	user, err := s.User(id)
	return user, err == nil
}

func (s *Store) UserByToken(token string) (User, error) {
	var user User
	err := s.db.QueryRow(`
SELECT users.id, users.username, users.display_name, users.email, users.created_at
FROM sessions
JOIN users ON users.id = sessions.user_id
WHERE sessions.token = ? AND sessions.expires_at > datetime('now')
`, token).Scan(&user.ID, &user.Username, &user.DisplayName, &user.Email, &user.CreatedAt)
	return user, err
}

func (s *Store) CreateOrg(name, displayName string) (map[string]any, error) {
	res, err := s.db.Exec(`INSERT INTO orgs (name, display_name) VALUES (?, ?)`, name, displayName)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.rowByID("orgs", id)
}

func (s *Store) Orgs() ([]map[string]any, error) {
	return s.rows(`SELECT id, name, display_name, created_at FROM orgs ORDER BY name`)
}

func (s *Store) GrantRepoPermission(repoName string, userID int64, role string) (Permission, error) {
	if !validRole(role) {
		return Permission{}, errors.New("invalid role")
	}
	repoID, err := s.RepoID(repoName)
	if err != nil {
		return Permission{}, err
	}
	_, err = s.db.Exec(`
INSERT INTO repo_permissions (repo_id, user_id, role) VALUES (?, ?, ?)
ON CONFLICT(repo_id, user_id) DO UPDATE SET role = excluded.role
`, repoID, userID, role)
	if err != nil {
		return Permission{}, err
	}
	return s.permission(repoID, userID)
}

func (s *Store) RepoPermissions(repoName string) ([]Permission, error) {
	repoID, err := s.RepoID(repoName)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`
SELECT repo_permissions.repo_id, repo_permissions.user_id, users.username, repo_permissions.role, repo_permissions.created_at
FROM repo_permissions
JOIN users ON users.id = repo_permissions.user_id
WHERE repo_permissions.repo_id = ?
ORDER BY users.username
`, repoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var permissions []Permission
	for rows.Next() {
		var p Permission
		if err := rows.Scan(&p.RepoID, &p.UserID, &p.Username, &p.Role, &p.CreatedAt); err != nil {
			return nil, err
		}
		permissions = append(permissions, p)
	}
	return permissions, rows.Err()
}

func (s *Store) UserRepoRole(repoName string, userID int64) (string, error) {
	if userID == 1 {
		return "admin", nil
	}
	repoID, err := s.RepoID(repoName)
	if err != nil {
		return "", err
	}
	var role string
	err = s.db.QueryRow(`SELECT role FROM repo_permissions WHERE repo_id = ? AND user_id = ?`, repoID, userID).Scan(&role)
	return role, err
}

func (s *Store) permission(repoID, userID int64) (Permission, error) {
	var p Permission
	err := s.db.QueryRow(`
SELECT repo_permissions.repo_id, repo_permissions.user_id, users.username, repo_permissions.role, repo_permissions.created_at
FROM repo_permissions
JOIN users ON users.id = repo_permissions.user_id
WHERE repo_permissions.repo_id = ? AND repo_permissions.user_id = ?
`, repoID, userID).Scan(&p.RepoID, &p.UserID, &p.Username, &p.Role, &p.CreatedAt)
	return p, err
}

func (s *Store) CreatePullRequest(repoName, title, body, source, target string, authorID int64) (map[string]any, error) {
	repoID, err := s.RepoID(repoName)
	if err != nil {
		return nil, err
	}
	number, err := s.nextNumber("pull_requests", repoID)
	if err != nil {
		return nil, err
	}
	res, err := s.db.Exec(`INSERT INTO pull_requests (repo_id, number, title, body, source_branch, target_branch, author_id) VALUES (?, ?, ?, ?, ?, ?, ?)`, repoID, number, title, body, source, target, nullUser(authorID))
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.rowByID("pull_requests", id)
}

func (s *Store) PullRequests(repoName string) ([]map[string]any, error) {
	repoID, err := s.RepoID(repoName)
	if err != nil {
		return nil, err
	}
	return s.rows(`SELECT id, number, title, body, source_branch, target_branch, author_id, status, merge_commit_sha, created_at, updated_at FROM pull_requests WHERE repo_id = ? ORDER BY number DESC`, repoID)
}

func (s *Store) UpdatePullRequestStatus(repoName string, number int64, status, mergeSHA string) (map[string]any, error) {
	repoID, err := s.RepoID(repoName)
	if err != nil {
		return nil, err
	}
	_, err = s.db.Exec(`UPDATE pull_requests SET status = ?, merge_commit_sha = ?, updated_at = CURRENT_TIMESTAMP WHERE repo_id = ? AND number = ?`, status, mergeSHA, repoID, number)
	if err != nil {
		return nil, err
	}
	return s.pullRequestByNumber(repoID, number)
}

func (s *Store) PullRequest(repoName string, number int64) (map[string]any, error) {
	repoID, err := s.RepoID(repoName)
	if err != nil {
		return nil, err
	}
	return s.pullRequestByNumber(repoID, number)
}

func (s *Store) pullRequestByNumber(repoID, number int64) (map[string]any, error) {
	return s.row(`SELECT id, number, title, body, source_branch, target_branch, author_id, status, merge_commit_sha, created_at, updated_at FROM pull_requests WHERE repo_id = ? AND number = ?`, repoID, number)
}

func (s *Store) CreateReview(repoName string, prNumber int64, reviewerID int64, state, body string) (map[string]any, error) {
	pr, err := s.PullRequest(repoName, prNumber)
	if err != nil {
		return nil, err
	}
	res, err := s.db.Exec(`INSERT INTO pr_reviews (pull_request_id, reviewer_id, state, body) VALUES (?, ?, ?, ?)`, pr["id"], nullUser(reviewerID), state, body)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.rowByID("pr_reviews", id)
}

func (s *Store) Reviews(repoName string, prNumber int64) ([]map[string]any, error) {
	pr, err := s.PullRequest(repoName, prNumber)
	if err != nil {
		return nil, err
	}
	return s.rows(`SELECT id, pull_request_id, reviewer_id, state, body, created_at FROM pr_reviews WHERE pull_request_id = ? ORDER BY id DESC`, pr["id"])
}

func (s *Store) CreateComment(repoName, subjectType string, subjectID, authorID int64, body, filePath string, lineNumber *int64) (map[string]any, error) {
	repoID, err := s.RepoID(repoName)
	if err != nil {
		return nil, err
	}
	res, err := s.db.Exec(`INSERT INTO comments (repo_id, subject_type, subject_id, author_id, body, file_path, line_number) VALUES (?, ?, ?, ?, ?, ?, ?)`, repoID, subjectType, subjectID, nullUser(authorID), body, filePath, lineNumber)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.rowByID("comments", id)
}

func (s *Store) Comments(repoName, subjectType string, subjectID int64) ([]map[string]any, error) {
	repoID, err := s.RepoID(repoName)
	if err != nil {
		return nil, err
	}
	return s.rows(`SELECT id, subject_type, subject_id, author_id, body, file_path, line_number, created_at, updated_at FROM comments WHERE repo_id = ? AND subject_type = ? AND subject_id = ? ORDER BY id`, repoID, subjectType, subjectID)
}

func (s *Store) CreateIssue(repoName, title, body string, authorID, assigneeID int64) (map[string]any, error) {
	repoID, err := s.RepoID(repoName)
	if err != nil {
		return nil, err
	}
	number, err := s.nextNumber("issues", repoID)
	if err != nil {
		return nil, err
	}
	res, err := s.db.Exec(`INSERT INTO issues (repo_id, number, title, body, author_id, assignee_id) VALUES (?, ?, ?, ?, ?, ?)`, repoID, number, title, body, nullUser(authorID), nullUser(assigneeID))
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.rowByID("issues", id)
}

func (s *Store) Issues(repoName string) ([]map[string]any, error) {
	repoID, err := s.RepoID(repoName)
	if err != nil {
		return nil, err
	}
	return s.rows(`SELECT id, number, title, body, author_id, assignee_id, status, created_at, updated_at FROM issues WHERE repo_id = ? ORDER BY number DESC`, repoID)
}

func (s *Store) CreateRelease(repoName, tag, title, notes string, authorID int64) (map[string]any, error) {
	repoID, err := s.RepoID(repoName)
	if err != nil {
		return nil, err
	}
	res, err := s.db.Exec(`INSERT INTO releases (repo_id, tag_name, title, notes, author_id) VALUES (?, ?, ?, ?, ?)`, repoID, tag, title, notes, nullUser(authorID))
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.rowByID("releases", id)
}

func (s *Store) Releases(repoName string) ([]map[string]any, error) {
	repoID, err := s.RepoID(repoName)
	if err != nil {
		return nil, err
	}
	return s.rows(`SELECT id, tag_name, title, notes, author_id, created_at FROM releases WHERE repo_id = ? ORDER BY id DESC`, repoID)
}

func (s *Store) CreateWebhook(repoName, url, events string, active bool) (map[string]any, error) {
	repoID, err := s.RepoID(repoName)
	if err != nil {
		return nil, err
	}
	res, err := s.db.Exec(`INSERT INTO webhooks (repo_id, url, events, active) VALUES (?, ?, ?, ?)`, repoID, url, events, boolInt(active))
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.rowByID("webhooks", id)
}

func (s *Store) ActiveWebhooks(repoName, event string) ([]map[string]any, error) {
	repoID, err := s.RepoID(repoName)
	if err != nil {
		return nil, err
	}
	return s.rows(`SELECT id, url, events, active, created_at FROM webhooks WHERE repo_id = ? AND active = 1 AND (events = '' OR events LIKE ?) ORDER BY id`, repoID, "%"+event+"%")
}

func (s *Store) RecordWebhookDelivery(webhookID int64, event, status, response string) error {
	_, err := s.db.Exec(`INSERT INTO webhook_deliveries (webhook_id, event, status, response) VALUES (?, ?, ?, ?)`, webhookID, event, status, response)
	return err
}

func (s *Store) Webhooks(repoName string) ([]map[string]any, error) {
	repoID, err := s.RepoID(repoName)
	if err != nil {
		return nil, err
	}
	return s.rows(`SELECT id, url, events, active, created_at FROM webhooks WHERE repo_id = ? ORDER BY id DESC`, repoID)
}

func (s *Store) CreateCIRun(repoName, commitSHA, branch, status, provider, logURL string) (map[string]any, error) {
	repoID, err := s.RepoID(repoName)
	if err != nil {
		return nil, err
	}
	if provider == "" {
		provider = "minihub"
	}
	res, err := s.db.Exec(`INSERT INTO ci_runs (repo_id, commit_sha, branch, status, provider, log_url) VALUES (?, ?, ?, ?, ?, ?)`, repoID, commitSHA, branch, status, provider, logURL)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.rowByID("ci_runs", id)
}

func (s *Store) UpdateCIRun(id int64, status, logURL string) (map[string]any, error) {
	_, err := s.db.Exec(`UPDATE ci_runs SET status = ?, log_url = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, status, logURL, id)
	if err != nil {
		return nil, err
	}
	return s.rowByID("ci_runs", id)
}

func (s *Store) CIRuns(repoName string) ([]map[string]any, error) {
	repoID, err := s.RepoID(repoName)
	if err != nil {
		return nil, err
	}
	return s.rows(`SELECT id, commit_sha, branch, status, provider, log_url, created_at, updated_at FROM ci_runs WHERE repo_id = ? ORDER BY id DESC`, repoID)
}

func (s *Store) nextNumber(table string, repoID int64) (int64, error) {
	var number int64
	query := fmt.Sprintf(`SELECT COALESCE(MAX(number), 0) + 1 FROM %s WHERE repo_id = ?`, table)
	err := s.db.QueryRow(query, repoID).Scan(&number)
	return number, err
}

func (s *Store) rowByID(table string, id int64) (map[string]any, error) {
	return s.row(fmt.Sprintf(`SELECT * FROM %s WHERE id = ?`, table), id)
}

func (s *Store) row(query string, args ...any) (map[string]any, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items, err := scanRows(rows)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, sql.ErrNoRows
	}
	return items[0], nil
}

func (s *Store) rows(query string, args ...any) ([]map[string]any, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRows(rows)
}

func scanRows(rows *sql.Rows) ([]map[string]any, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var out []map[string]any
	for rows.Next() {
		values := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		item := map[string]any{}
		for i, col := range cols {
			item[toCamel(col)] = dbValue(values[i])
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func dbValue(v any) any {
	switch value := v.(type) {
	case []byte:
		return string(value)
	default:
		return value
	}
}

func toCamel(s string) string {
	parts := strings.Split(s, "_")
	for i := 1; i < len(parts); i++ {
		if parts[i] != "" {
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
	}
	return strings.Join(parts, "")
}

func nullUser(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func validRole(role string) bool {
	switch role {
	case "admin", "maintain", "write", "triage", "read":
		return true
	default:
		return false
	}
}

func hashPassword(password string) string {
	sum := sha256.Sum256([]byte(password))
	return hex.EncodeToString(sum[:])
}

func randomToken() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

const sqliteSchema = `
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS users (
  id INTEGER PRIMARY KEY,
  username TEXT NOT NULL UNIQUE,
  display_name TEXT NOT NULL DEFAULT '',
  email TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS sessions (
  token TEXT PRIMARY KEY,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS orgs (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  display_name TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS org_members (
  org_id INTEGER NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  role TEXT NOT NULL CHECK (role IN ('owner', 'maintainer', 'member')),
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (org_id, user_id)
);

CREATE TABLE IF NOT EXISTS repo_records (
  id INTEGER PRIMARY KEY,
  owner_type TEXT NOT NULL CHECK (owner_type IN ('user', 'org')),
  owner_id INTEGER NOT NULL,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  default_branch TEXT NOT NULL DEFAULT 'main',
  visibility TEXT NOT NULL DEFAULT 'private' CHECK (visibility IN ('private', 'internal', 'public')),
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE (owner_type, owner_id, name)
);

CREATE TABLE IF NOT EXISTS repo_permissions (
  repo_id INTEGER NOT NULL REFERENCES repo_records(id) ON DELETE CASCADE,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  role TEXT NOT NULL CHECK (role IN ('admin', 'maintain', 'write', 'triage', 'read')),
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (repo_id, user_id)
);

CREATE TABLE IF NOT EXISTS protected_branches (
  repo_id INTEGER NOT NULL REFERENCES repo_records(id) ON DELETE CASCADE,
  pattern TEXT NOT NULL,
  require_approval_count INTEGER NOT NULL DEFAULT 1,
  block_force_pushes INTEGER NOT NULL DEFAULT 1,
  block_deletions INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (repo_id, pattern)
);

CREATE TABLE IF NOT EXISTS pull_requests (
  id INTEGER PRIMARY KEY,
  repo_id INTEGER NOT NULL REFERENCES repo_records(id) ON DELETE CASCADE,
  number INTEGER NOT NULL,
  title TEXT NOT NULL,
  body TEXT NOT NULL DEFAULT '',
  source_branch TEXT NOT NULL,
  target_branch TEXT NOT NULL,
  author_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
  status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'closed', 'merged')),
  merge_commit_sha TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE (repo_id, number)
);

CREATE TABLE IF NOT EXISTS pr_reviews (
  id INTEGER PRIMARY KEY,
  pull_request_id INTEGER NOT NULL REFERENCES pull_requests(id) ON DELETE CASCADE,
  reviewer_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
  state TEXT NOT NULL CHECK (state IN ('commented', 'approved', 'changes_requested')),
  body TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS comments (
  id INTEGER PRIMARY KEY,
  repo_id INTEGER NOT NULL REFERENCES repo_records(id) ON DELETE CASCADE,
  subject_type TEXT NOT NULL CHECK (subject_type IN ('pull_request', 'issue', 'commit', 'review')),
  subject_id INTEGER NOT NULL,
  author_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
  body TEXT NOT NULL,
  file_path TEXT NOT NULL DEFAULT '',
  line_number INTEGER,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS issues (
  id INTEGER PRIMARY KEY,
  repo_id INTEGER NOT NULL REFERENCES repo_records(id) ON DELETE CASCADE,
  number INTEGER NOT NULL,
  title TEXT NOT NULL,
  body TEXT NOT NULL DEFAULT '',
  author_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
  assignee_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
  status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'closed')),
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE (repo_id, number)
);

CREATE TABLE IF NOT EXISTS releases (
  id INTEGER PRIMARY KEY,
  repo_id INTEGER NOT NULL REFERENCES repo_records(id) ON DELETE CASCADE,
  tag_name TEXT NOT NULL,
  title TEXT NOT NULL,
  notes TEXT NOT NULL DEFAULT '',
  author_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE (repo_id, tag_name)
);

CREATE TABLE IF NOT EXISTS webhooks (
  id INTEGER PRIMARY KEY,
  repo_id INTEGER NOT NULL REFERENCES repo_records(id) ON DELETE CASCADE,
  url TEXT NOT NULL,
  secret_hash TEXT NOT NULL DEFAULT '',
  events TEXT NOT NULL DEFAULT 'push,pull_request',
  active INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS webhook_deliveries (
  id INTEGER PRIMARY KEY,
  webhook_id INTEGER NOT NULL REFERENCES webhooks(id) ON DELETE CASCADE,
  event TEXT NOT NULL,
  status TEXT NOT NULL,
  response TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS ci_runs (
  id INTEGER PRIMARY KEY,
  repo_id INTEGER NOT NULL REFERENCES repo_records(id) ON DELETE CASCADE,
  commit_sha TEXT NOT NULL,
  branch TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL CHECK (status IN ('queued', 'running', 'success', 'failure', 'cancelled')),
  provider TEXT NOT NULL DEFAULT 'minihub',
  log_url TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`

package app

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/crypto/ssh"
)

type SSHConfig struct {
	Addr    string
	DataDir string
	DBPath  string
	KeyPath string
}

func RunSSHServer(cfg SSHConfig) error {
	if cfg.Addr == "" {
		cfg.Addr = ":2222"
	}
	if cfg.DataDir == "" {
		cfg.DataDir = "data"
	}
	if cfg.KeyPath == "" {
		cfg.KeyPath = filepath.Join(cfg.DataDir, "ssh_host_key")
	}
	store, err := OpenStore(cfg.DataDir, cfg.DBPath)
	if err != nil {
		return err
	}
	defer store.Close()
	signer, err := loadOrCreateHostKey(cfg.KeyPath)
	if err != nil {
		return err
	}
	serverConfig := &ssh.ServerConfig{
		PasswordCallback: func(conn ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			user, ok := store.CheckPassword(conn.User(), string(password))
			if !ok {
				return nil, errors.New("invalid credentials")
			}
			return &ssh.Permissions{Extensions: map[string]string{"user_id": fmt.Sprint(user.ID)}}, nil
		},
	}
	serverConfig.AddHostKey(signer)

	listener, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return err
	}
	log.Printf("minihub ssh listening on %s", cfg.Addr)
	for {
		conn, err := listener.Accept()
		if err != nil {
			return err
		}
		go handleSSHConn(conn, serverConfig, store, filepath.Join(cfg.DataDir, "repos"))
	}
}

func handleSSHConn(raw net.Conn, config *ssh.ServerConfig, store *Store, repoRoot string) {
	conn, chans, reqs, err := ssh.NewServerConn(raw, config)
	if err != nil {
		_ = raw.Close()
		return
	}
	defer conn.Close()
	go ssh.DiscardRequests(reqs)
	for ch := range chans {
		if ch.ChannelType() != "session" {
			_ = ch.Reject(ssh.UnknownChannelType, "session required")
			continue
		}
		channel, requests, err := ch.Accept()
		if err != nil {
			continue
		}
		go handleSSHSession(conn, channel, requests, store, repoRoot)
	}
}

func handleSSHSession(conn *ssh.ServerConn, channel ssh.Channel, requests <-chan *ssh.Request, store *Store, repoRoot string) {
	defer channel.Close()
	for req := range requests {
		if req.Type != "exec" {
			_ = req.Reply(false, nil)
			continue
		}
		command := parseSSHCommand(req.Payload)
		code := runGitSSHCommand(command, repoRoot, conn.Permissions, store, channel)
		_ = req.Reply(true, nil)
		_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{uint32(code)}))
		return
	}
}

func runGitSSHCommand(command string, repoRoot string, permissions *ssh.Permissions, store *Store, channel ssh.Channel) int {
	service, repoName, ok := parseGitSSHCommand(command)
	if !ok {
		_, _ = io.WriteString(channel.Stderr(), "unsupported command\n")
		return 1
	}
	userID, _ := strconv.ParseInt(permissions.Extensions["user_id"], 10, 64)
	minRole := "read"
	if service == "git-receive-pack" {
		minRole = "write"
	}
	role, err := store.UserRepoRole(repoName, userID)
	if err != nil || !roleAllows(role, minRole) {
		_, _ = io.WriteString(channel.Stderr(), "repository permission denied\n")
		return 1
	}
	repoPath := filepath.Join(repoRoot, filepath.FromSlash(repoName)+".git")
	if !isDir(repoPath) {
		_, _ = io.WriteString(channel.Stderr(), "repository not found\n")
		return 1
	}
	cmd := exec.Command("git", service, repoPath)
	cmd.Stdin = channel
	cmd.Stdout = channel
	cmd.Stderr = channel.Stderr()
	if err := cmd.Run(); err != nil {
		return 1
	}
	return 0
}

func parseSSHCommand(payload []byte) string {
	if len(payload) < 4 {
		return ""
	}
	n := int(payload[0])<<24 | int(payload[1])<<16 | int(payload[2])<<8 | int(payload[3])
	if n < 0 || n > len(payload)-4 {
		return ""
	}
	return string(payload[4 : 4+n])
}

func parseGitSSHCommand(command string) (string, string, bool) {
	fields := strings.Fields(command)
	if len(fields) != 2 {
		return "", "", false
	}
	service := fields[0]
	if service != "git-upload-pack" && service != "git-receive-pack" {
		return "", "", false
	}
	repo := strings.Trim(fields[1], `'"`)
	repo = strings.TrimPrefix(repo, "/")
	repo = strings.TrimSuffix(repo, ".git")
	if _, err := normalizeRepoName(repo); err != nil {
		return "", "", false
	}
	return service, repo, true
}

func loadOrCreateHostKey(path string) (ssh.Signer, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		return ssh.ParsePrivateKey(data)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	key, err := rsa.GenerateKey(rand.Reader, 3072)
	if err != nil {
		return nil, err
	}
	data = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return nil, err
	}
	return ssh.ParsePrivateKey(data)
}

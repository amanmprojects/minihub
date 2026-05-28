package app

import "testing"

func TestParseGitSSHCommand(t *testing.T) {
	service, repo, ok := parseGitSSHCommand("git-upload-pack 'team/demo.git'")
	if !ok || service != "git-upload-pack" || repo != "team/demo" {
		t.Fatalf("parsed upload command = %q %q %v", service, repo, ok)
	}
	service, repo, ok = parseGitSSHCommand(`git-receive-pack "/demo.git"`)
	if !ok || service != "git-receive-pack" || repo != "demo" {
		t.Fatalf("parsed receive command = %q %q %v", service, repo, ok)
	}
	if _, _, ok := parseGitSSHCommand("rm -rf demo.git"); ok {
		t.Fatal("accepted unsupported SSH command")
	}
}

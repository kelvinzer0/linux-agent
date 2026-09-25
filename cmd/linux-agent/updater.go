package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

type GitHubRelease struct {
	TagName string `json:"tag_name"`
	Name    string `json:"name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

const (
	RepoOwner = "kelvinzer0"
	RepoName  = "linux-agent"
)

// CheckAndUpdate performs self-update from GitHub Releases if a newer version is available.
func CheckAndUpdate(force bool) error {
	log.Printf("[updater] Checking for updates (current version: v%s)...", Version)

	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", RepoOwner, RepoName)
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "linux-agent-updater/1.0.0")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed querying GitHub API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		log.Println("[updater] No releases found yet on GitHub.")
		return nil
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var rel GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return fmt.Errorf("failed decoding release: %w", err)
	}

	latestVer := strings.TrimPrefix(rel.TagName, "v")
	currentVer := strings.TrimPrefix(Version, "v")

	if !force && latestVer == currentVer {
		log.Printf("[updater] linux-agent is already up to date (v%s).", Version)
		return nil
	}

	log.Printf("[updater] New version available: v%s (current: v%s)", latestVer, currentVer)

	// Look for matching asset: linux-agent-linux-<arch> or linux-agent-<arch>
	arch := runtime.GOARCH
	var downloadURL string
	for _, asset := range rel.Assets {
		name := strings.ToLower(asset.Name)
		if strings.Contains(name, "linux") && strings.Contains(name, arch) {
			downloadURL = asset.BrowserDownloadURL
			break
		}
	}

	if downloadURL == "" {
		log.Printf("[updater] No prebuilt binary found for linux/%s in release %s", arch, rel.TagName)
		return nil
	}

	log.Printf("[updater] Downloading binary from %s ...", downloadURL)
	tmpBin, err := downloadBinary(downloadURL)
	if err != nil {
		return fmt.Errorf("failed downloading update: %w", err)
	}
	defer os.Remove(tmpBin)

	execPath, err := os.Executable()
	if err != nil {
		execPath = "/usr/local/bin/linux-agent"
	}

	log.Printf("[updater] Installing update to %s ...", execPath)
	if err := installBinary(tmpBin, execPath); err != nil {
		return fmt.Errorf("failed installing new binary: %w", err)
	}

	log.Printf("[updater] ✅ Successfully updated linux-agent to v%s!", latestVer)

	// Restart systemd service if running under systemctl
	if isSystemdRunning() {
		log.Println("[updater] Restarting linux-agent service via systemctl...")
		_ = exec.Command("systemctl", "restart", "linux-agent").Run()
	}

	return nil
}

func downloadBinary(url string) (string, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "linux-agent-updater/1.0.0")

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}

	tmpFile, err := os.CreateTemp("", "linux-agent-update-*")
	if err != nil {
		return "", err
	}
	defer tmpFile.Close()

	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		return "", err
	}

	if err := os.Chmod(tmpFile.Name(), 0755); err != nil {
		return "", err
	}

	return tmpFile.Name(), nil
}

func installBinary(src, dst string) error {
	// Atomic replacement using temp file in same directory
	dir := "/usr/local/bin"
	if _, err := os.Stat(dir); err != nil {
		dir = os.TempDir()
	}

	tmpDst, err := os.CreateTemp(dir, "linux-agent-new-*")
	if err != nil {
		return err
	}
	tmpDstPath := tmpDst.Name()

	srcFile, err := os.Open(src)
	if err != nil {
		_ = os.Remove(tmpDstPath)
		return err
	}
	defer srcFile.Close()

	if _, err := io.Copy(tmpDst, srcFile); err != nil {
		_ = os.Remove(tmpDstPath)
		_ = tmpDst.Close()
		return err
	}
	_ = tmpDst.Close()
	_ = os.Chmod(tmpDstPath, 0755)

	return os.Rename(tmpDstPath, dst)
}

func isSystemdRunning() bool {
	cmd := exec.Command("systemctl", "is-active", "--quiet", "linux-agent")
	return cmd.Run() == nil
}

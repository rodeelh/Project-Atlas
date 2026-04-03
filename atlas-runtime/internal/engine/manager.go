package engine

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"atlas-runtime-go/internal/logstore"
)

// Manager controls the llama-server subprocess lifecycle.
// It is safe for concurrent use.
type Manager struct {
	installDir string // ~/Library/Application Support/Atlas — where llama-server lives
	modelsDir  string // ~/Library/Application Support/ProjectAtlas/models — user model files

	mu          sync.Mutex
	cmd         *exec.Cmd
	port        int
	loadedModel string
	lastError   string // last process exit reason (empty when running cleanly)

	lastDecodeTPS float64 // cached from most recent /metrics poll

	// Idle auto-eject: stop the model after idleTimeout of no activity.
	idleTimeout  time.Duration // zero = disabled
	lastActivity time.Time
	watcherStop  chan struct{} // closed to stop the idle watcher goroutine
}

// NewManager creates a Manager.
//   - installDir: Atlas install dir (binary at installDir/engine/llama-server)
//   - modelsDir:  directory for GGUF model files
func NewManager(installDir, modelsDir string) *Manager {
	return &Manager{
		installDir: installDir,
		modelsDir:  modelsDir,
	}
}

// SetIdleTimeout configures automatic ejection after d of inactivity.
// Must be called before Start. Zero disables idle ejection.
func (m *Manager) SetIdleTimeout(d time.Duration) {
	m.mu.Lock()
	m.idleTimeout = d
	m.mu.Unlock()
}

// RecordActivity resets the idle timer. Call after each completed inference turn.
func (m *Manager) RecordActivity() {
	m.mu.Lock()
	m.lastActivity = time.Now()
	m.mu.Unlock()
}

// startIdleWatcher launches a background goroutine that stops the model after
// idleTimeout of inactivity. Replaces any existing watcher.
func (m *Manager) startIdleWatcher() {
	if m.idleTimeout <= 0 {
		return
	}
	// Stop any existing watcher.
	if m.watcherStop != nil {
		close(m.watcherStop)
	}
	m.watcherStop = make(chan struct{})
	stop := m.watcherStop
	timeout := m.idleTimeout

	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				m.mu.Lock()
				idle := time.Since(m.lastActivity)
				running := m.isRunningLocked()
				m.mu.Unlock()
				if running && idle >= timeout {
					logstore.Write("info", fmt.Sprintf("Engine idle for %s — auto-ejecting model", idle.Round(time.Minute)),
						map[string]string{"idle": idle.String()})
					_ = m.Stop()
					return
				}
			}
		}
	}()
}

// BinaryPath returns the full path to the llama-server binary.
func (m *Manager) BinaryPath() string {
	return filepath.Join(m.installDir, "engine", "llama-server")
}

// ModelsDir returns the directory where GGUF models are stored.
func (m *Manager) ModelsDir() string {
	return m.modelsDir
}

// BinaryReady reports whether the llama-server binary exists on disk.
func (m *Manager) BinaryReady() bool {
	info, err := os.Stat(m.BinaryPath())
	return err == nil && !info.IsDir()
}

// BinaryVersion returns the llama.cpp build tag (e.g. "b8641") by running
// llama-server --version and parsing the first line. Returns empty string
// if the binary is absent or the version cannot be parsed.
func (m *Manager) BinaryVersion() string {
	if !m.BinaryReady() {
		return ""
	}
	out, err := exec.Command(m.BinaryPath(), "--version").CombinedOutput()
	if err != nil && len(out) == 0 {
		return ""
	}
	// Scan all lines — Metal init messages precede the version line on Apple Silicon.
	// Output format: "version: 8641 (5208e2d5b)"
	for _, line := range strings.Split(string(out), "\n") {
		if idx := strings.Index(line, "version: "); idx >= 0 {
			rest := line[idx+9:]
			if end := strings.IndexAny(rest, " \t("); end > 0 {
				return "b" + strings.TrimSpace(rest[:end])
			}
		}
	}
	return ""
}

// IsRunning reports whether the llama-server process is currently alive.
func (m *Manager) IsRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.isRunningLocked()
}

func (m *Manager) isRunningLocked() bool {
	return m.cmd != nil && m.cmd.Process != nil && m.cmd.ProcessState == nil
}

// LoadedModel returns the model filename currently loaded, empty if not running.
func (m *Manager) LoadedModel() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.loadedModel
}

// LastError returns the last process exit reason, or empty string if healthy.
func (m *Manager) LastError() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastError
}

// Status returns a snapshot of the current engine state.
func (m *Manager) Status(cfgPort int) EngineStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	port := m.port
	if port == 0 {
		port = cfgPort
	}
	return EngineStatus{
		Running:      m.isRunningLocked(),
		LoadedModel:  m.loadedModel,
		Port:         port,
		BinaryReady:  m.BinaryReady(),
		BuildVersion: m.BinaryVersion(),
		LastError:    m.lastError,
	}
}

// Start launches llama-server with the given model, port, context size, and KV cache quant level.
// ctxSize is the KV-cache token limit passed via --ctx-size; defaults to 8192 if <= 0.
// kvCacheQuant sets -ctk/-ctv quantisation ("q4_0", "q8_0", "f16"); defaults to "q4_0" if empty.
// If a server is already running it is stopped first.
func (m *Manager) Start(modelName string, port int, ctxSize int, kvCacheQuant string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.BinaryReady() {
		return fmt.Errorf("llama-server binary not found — run 'make install' or 'make download-engine' to install it")
	}

	modelPath := filepath.Join(m.modelsDir, modelName)
	if _, err := os.Stat(modelPath); err != nil {
		return fmt.Errorf("model %q not found in models directory", modelName)
	}

	if ctxSize <= 0 {
		ctxSize = 8192
	}
	if kvCacheQuant == "" {
		kvCacheQuant = "q4_0"
	}

	// Stop any existing process first.
	m.stopLocked()

	threads := fmt.Sprintf("%d", detectPCoreCount())

	var stderrBuf bytes.Buffer
	cmd := exec.Command(
		m.BinaryPath(),
		"--model", modelPath,
		"--port", fmt.Sprintf("%d", port),
		"--host", "127.0.0.1",
		"--ctx-size", fmt.Sprintf("%d", ctxSize),
		"--n-gpu-layers", "99",     // offload all layers to Metal GPU when available
		"--flash-attn", "on",       // flash attention — cuts KV cache memory 2-4x, matches LM Studio default
		"-ctk", kvCacheQuant,        // KV K-cache quantisation — configurable; q4_0 matched to Q4 models, q8_0 for Q8/f16 models
		"-ctv", kvCacheQuant,        // KV V-cache quantisation — same level as K cache
		"--cache-prompt",           // reuse KV cache for identical prompt prefixes (system prompt etc.)
		"--no-mmap",                // load model fully into physical RAM — eliminates VM paging overhead during inference
		"--defrag-thold", "0.1",    // KV cache defrag threshold — reclaim fragmented slots; prevents TPS drop in long convos
		"-t", threads,              // inference threads — P-cores only, E-cores hurt llama.cpp throughput
		"-tb", threads,             // batch threads — same P-core count
		"--jinja",                  // enable Jinja chat templates — required for tool/function calling
		"--log-disable",            // suppress verbose llama.cpp log noise
		"--metrics",                // expose Prometheus metrics at /metrics for decode TPS tracking
	)
	cmd.Stdout = nil
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start llama-server: %w", err)
	}

	m.cmd = cmd
	m.port = port
	m.loadedModel = filepath.Base(modelName) // store filename only, not full path
	m.lastError = ""
	m.lastActivity = time.Now()
	m.startIdleWatcher()

	// Reap the process in background so ProcessState is set on crash/exit.
	go func() {
		cmd.Wait() //nolint:errcheck
		m.mu.Lock()
		if m.cmd == cmd {
			m.cmd = nil
			m.loadedModel = ""
			// Capture the last line of stderr as the exit reason for diagnostics.
			if msg := strings.TrimSpace(stderrBuf.String()); msg != "" {
				lines := strings.Split(msg, "\n")
				m.lastError = lines[len(lines)-1]
			}
		}
		m.mu.Unlock()
	}()

	return nil
}

// WaitUntilReady polls the llama-server /health endpoint until the server
// reports status "ok" (model fully loaded) or the timeout elapses.
// llama-server returns 200 {"status":"ok"} when ready, 503 while loading.
func (m *Manager) WaitUntilReady(port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	url := fmt.Sprintf("http://127.0.0.1:%d/health", port)
	client := &http.Client{Timeout: 2 * time.Second}
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	return fmt.Errorf("engine: llama-server did not become ready within %s", timeout)
}

// FetchDecodeTPS polls the llama-server /metrics endpoint and returns the
// current decode throughput in tokens/sec from the pre-computed
// llamacpp:predicted_tokens_seconds gauge. Returns 0 if the server is
// unreachable or has produced no tokens yet.
func (m *Manager) FetchDecodeTPS(port int) float64 {
	client := &http.Client{Timeout: 500 * time.Millisecond}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/metrics", port))
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0
	}

	tps := parsePrometheusCounter(string(body), "llamacpp:predicted_tokens_seconds")
	if tps <= 0 {
		return 0
	}

	m.mu.Lock()
	m.lastDecodeTPS = tps
	m.mu.Unlock()
	return tps
}

// FetchContextTokens is a stub — the n_past field was removed from the
// /slots response in recent llama-server builds. Returns -1 so callers
// treat it as unavailable.
func (m *Manager) FetchContextTokens(_ int) int { return -1 }

// parsePrometheusCounter extracts the float64 value of a counter metric from
// a Prometheus text-format response. Returns -1 if the metric is not found.
func parsePrometheusCounter(body, name string) float64 {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		if strings.HasPrefix(line, name+" ") || strings.HasPrefix(line, name+"{") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				if v, err := strconv.ParseFloat(parts[len(parts)-1], 64); err == nil {
					return v
				}
			}
		}
	}
	return -1
}

// Stop terminates the running llama-server process.
func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Kill the idle watcher before stopping the process.
	if m.watcherStop != nil {
		close(m.watcherStop)
		m.watcherStop = nil
	}
	return m.stopLocked()
}

// stopLocked must be called with m.mu held.
func (m *Manager) stopLocked() error {
	if m.cmd == nil || m.cmd.Process == nil {
		return nil
	}
	if err := m.cmd.Process.Kill(); err != nil {
		return err
	}
	m.cmd = nil
	m.loadedModel = ""
	return nil
}

// ListModels returns all .gguf files in the models directory.
func (m *Manager) ListModels() ([]ModelInfo, error) {
	entries, err := os.ReadDir(m.modelsDir)
	if os.IsNotExist(err) {
		return []ModelInfo{}, nil
	}
	if err != nil {
		return nil, err
	}
	var models []ModelInfo
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".gguf" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		models = append(models, ModelInfo{
			Name:      e.Name(),
			SizeBytes: info.Size(),
			SizeHuman: humanBytes(info.Size()),
		})
	}
	return models, nil
}

// DeleteModel removes a model file from the models directory.
// Returns an error if the model is currently loaded.
func (m *Manager) DeleteModel(name string) error {
	if filepath.Base(name) != name || name == "" {
		return fmt.Errorf("invalid model name")
	}
	m.mu.Lock()
	loaded := m.loadedModel
	m.mu.Unlock()
	if loaded == name {
		return fmt.Errorf("cannot delete the currently loaded model — stop Engine LM first")
	}
	return os.Remove(filepath.Join(m.modelsDir, name))
}

// DownloadBinary downloads the specified llama.cpp release tag (e.g. "b8641"),
// extracts llama-server + shared libs into the engine directory, and replaces
// the existing binary. Progress is reported via the callback.
// The server must be stopped before calling this.
func (m *Manager) DownloadBinary(version string, progress func(downloaded, total int64)) error {
	// Determine host architecture for URL construction.
	out, err := exec.Command("uname", "-m").Output()
	if err != nil {
		return fmt.Errorf("could not determine architecture: %w", err)
	}
	arch := strings.TrimSpace(string(out)) // "arm64" or "x86_64"

	// llama.cpp repo moved to ggml-org; releases use .tar.gz since ~b8000.
	tarName := fmt.Sprintf("llama-%s-bin-macos-%s.tar.gz", version, arch)
	url := fmt.Sprintf("https://github.com/ggml-org/llama.cpp/releases/download/%s/%s", version, tarName)

	engineDir := filepath.Join(m.installDir, "engine")
	if err := os.MkdirAll(engineDir, 0o755); err != nil {
		return fmt.Errorf("could not create engine directory: %w", err)
	}

	// Download to a temp file.
	tmpTar, err := os.CreateTemp("", "llama-engine-*.tar.gz")
	if err != nil {
		return fmt.Errorf("could not create temp file: %w", err)
	}
	defer os.Remove(tmpTar.Name())

	if err := downloadFile(url, tmpTar, progress); err != nil {
		tmpTar.Close()
		return fmt.Errorf("download failed: %w", err)
	}
	tmpTar.Close()

	// Extract using tar.
	tmpDir, err := os.MkdirTemp("", "llama-extract-*")
	if err != nil {
		return fmt.Errorf("could not create temp extract dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	if out, err := exec.Command("tar", "-xzf", tmpTar.Name(), "-C", tmpDir).CombinedOutput(); err != nil {
		return fmt.Errorf("tar failed: %s: %w", strings.TrimSpace(string(out)), err)
	}

	// Find the extracted llama-server binary (may be in a subdirectory).
	var serverPath string
	_ = filepath.Walk(tmpDir, func(path string, info os.FileInfo, _ error) error {
		if !info.IsDir() && info.Name() == "llama-server" {
			serverPath = path
		}
		return nil
	})
	if serverPath == "" {
		return fmt.Errorf("llama-server not found in archive")
	}

	// Copy llama-server into the engine directory.
	dest := filepath.Join(engineDir, "llama-server")
	if err := copyFile(serverPath, dest); err != nil {
		return fmt.Errorf("could not install llama-server: %w", err)
	}
	if err := os.Chmod(dest, 0o755); err != nil {
		return err
	}

	// Copy shared libraries alongside.
	binDir := filepath.Dir(serverPath)
	entries, _ := os.ReadDir(binDir)
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".dylib") {
			src := filepath.Join(binDir, e.Name())
			_ = copyFile(src, filepath.Join(engineDir, e.Name()))
		}
	}

	return nil
}

// downloadFile performs an HTTP GET and writes the response body to dst,
// calling progress(downloaded, total) periodically.
func downloadFile(url string, dst *os.File, progress func(int64, int64)) error {
	resp, err := http.Get(url) //nolint:gosec,noctx
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %d", resp.StatusCode)
	}
	total := resp.ContentLength
	var downloaded int64
	buf := make([]byte, 32*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := dst.Write(buf[:n]); writeErr != nil {
				return writeErr
			}
			downloaded += int64(n)
			if progress != nil {
				progress(downloaded, total)
			}
		}
		if readErr != nil {
			if readErr.Error() == "EOF" {
				break
			}
			return readErr
		}
	}
	return nil
}

// copyFile copies src to dst, creating or truncating dst.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// detectPCoreCount returns the number of performance (P) cores on Apple Silicon
// by querying sysctl. Using only P-cores for llama.cpp threads avoids the
// throughput penalty caused by mixing fast and efficiency cores. Falls back to
// 4 if the value cannot be determined.
func detectPCoreCount() int {
	out, err := exec.Command("sysctl", "-n", "hw.perflevel0.physicalcpu").Output()
	if err != nil {
		return 4
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil || n <= 0 {
		return 4
	}
	return n
}

// humanBytes formats a byte count as a human-readable string (KB/MB/GB).
func humanBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

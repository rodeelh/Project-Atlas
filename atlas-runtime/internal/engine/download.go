package engine

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DownloadModel downloads a GGUF model from url into the models directory.
//
// filename must end with ".gguf" and must not contain path separators.
// progress is called roughly every 250ms with (bytesDownloaded, totalBytes);
// totalBytes is -1 when the server does not send Content-Length.
//
// Supports resumable downloads: if a partial .download file exists from a
// previous interrupted attempt, the download continues from where it left off
// using an HTTP Range request.
func (m *Manager) DownloadModel(ctx context.Context, url, filename string, progress func(downloaded, total int64)) error {
	if !strings.HasSuffix(strings.ToLower(filename), ".gguf") {
		return fmt.Errorf("filename must end with .gguf")
	}
	if filepath.Base(filename) != filename {
		return fmt.Errorf("invalid filename — must not contain path separators")
	}

	// Ensure models directory exists.
	if err := os.MkdirAll(m.modelsDir, 0o700); err != nil {
		return fmt.Errorf("create models directory: %w", err)
	}

	destPath := filepath.Join(m.modelsDir, filename)
	tmpPath := destPath + ".download"

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	// Resume partial download if tmp file exists.
	var startOffset int64
	if info, statErr := os.Stat(tmpPath); statErr == nil {
		startOffset = info.Size()
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", startOffset))
	}

	client := &http.Client{Timeout: 0} // no global timeout — large files
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP GET: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("server returned HTTP %d %s", resp.StatusCode, resp.Status)
	}

	total := resp.ContentLength
	if total > 0 && startOffset > 0 && resp.StatusCode == http.StatusPartialContent {
		total += startOffset
	}

	// Decide whether to append (resume) or truncate (fresh start).
	fileFlag := os.O_CREATE | os.O_WRONLY
	if startOffset > 0 && resp.StatusCode == http.StatusPartialContent {
		fileFlag |= os.O_APPEND
	} else {
		fileFlag |= os.O_TRUNC
		startOffset = 0
	}

	f, err := os.OpenFile(tmpPath, fileFlag, 0o600)
	if err != nil {
		return fmt.Errorf("open temp file: %w", err)
	}

	downloaded := startOffset
	lastReport := time.Now()
	buf := make([]byte, 32*1024)

	for {
		if ctx.Err() != nil {
			f.Close()
			return ctx.Err()
		}
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := f.Write(buf[:n]); writeErr != nil {
				f.Close()
				return fmt.Errorf("write: %w", writeErr)
			}
			downloaded += int64(n)
			if progress != nil && time.Since(lastReport) >= 250*time.Millisecond {
				progress(downloaded, total)
				lastReport = time.Now()
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			f.Close()
			return fmt.Errorf("read: %w", readErr)
		}
	}

	if err := f.Close(); err != nil {
		return fmt.Errorf("close: %w", err)
	}

	// Final progress report.
	if progress != nil {
		progress(downloaded, downloaded)
	}

	// Atomically move the completed tmp file to the final destination.
	return os.Rename(tmpPath, destPath)
}

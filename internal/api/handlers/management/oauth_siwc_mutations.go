package management

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type siwcFileMutationKey struct{}

func (h *Handler) UploadAuthFileV8(c *gin.Context) {
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), siwcFileMutationKey{}, true))
	h.UploadAuthFile(c)
}

func (h *Handler) DeleteAuthFileV8(c *gin.Context) {
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), siwcFileMutationKey{}, true))
	h.DeleteAuthFile(c)
}

func (h *Handler) beginSIWCFileMutation(ctx context.Context, path string) func() {
	if ctx == nil || ctx.Value(siwcFileMutationKey{}) != true {
		return func() {}
	}
	h.siwcCommitMu.Lock()
	oauthSessions.mu.Lock()
	now := time.Now()
	for state, session := range oauthSessions.sessions {
		flow := session.SIWC
		if flow == nil || flow.owner != h || session.Completed || session.Status != "" || !sameSIWCMutationPath(path, flow.path) {
			continue
		}

		session.Status = "SIWC binding target changed; start a new binding"
		session.ExpiresAt = now.Add(oauthSessions.completedTTL)
		oauthSessions.sessions[state] = session
		flow.cancel()
	}
	oauthSessions.mu.Unlock()
	return h.siwcCommitMu.Unlock
}

func sameSIWCMutationPath(a, b string) bool {

	if ai, errA := os.Stat(a); errA == nil {
		if bi, errB := os.Stat(b); errB == nil && os.SameFile(ai, bi) {
			return true
		}
	}
	canonical := func(path string) string {
		if real, err := filepath.EvalSymlinks(path); err == nil {
			path = real
		} else if parent, errParent := filepath.EvalSymlinks(filepath.Dir(path)); errParent == nil {
			path = filepath.Join(parent, filepath.Base(path))
		}
		if abs, err := filepath.Abs(path); err == nil {
			path = abs
		}
		return filepath.Clean(path)
	}
	a, b = canonical(a), canonical(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

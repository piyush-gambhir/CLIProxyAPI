package claude

import (
	"context"
	"errors"
	"fmt"
	stdhtml "html"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

type OAuthServer struct {
	server *http.Server

	port int

	resultChan chan *OAuthResult

	errorChan chan error

	mu sync.Mutex

	running bool
}

type OAuthResult struct {
	Code string

	State string

	Error string
}

func NewOAuthServer(port int) *OAuthServer {
	return &OAuthServer{
		port:       port,
		resultChan: make(chan *OAuthResult, 1),
		errorChan:  make(chan error, 1),
	}
}

func (s *OAuthServer) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("server is already running")
	}

	if !s.isPortAvailable() {
		return fmt.Errorf("port %d is already in use", s.port)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", s.handleCallback)
	mux.HandleFunc("/success", s.handleSuccess)

	s.server = &http.Server{
		Addr:         fmt.Sprintf(":%d", s.port),
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	s.running = true

	go func() {
		if err := s.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.errorChan <- fmt.Errorf("server failed to start: %w", err)
		}
	}()

	time.Sleep(100 * time.Millisecond)

	return nil
}

func (s *OAuthServer) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running || s.server == nil {
		return nil
	}

	log.Debug("Stopping OAuth callback server")

	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	err := s.server.Shutdown(shutdownCtx)
	s.running = false
	s.server = nil

	return err
}

func (s *OAuthServer) WaitForCallback(timeout time.Duration) (*OAuthResult, error) {
	select {
	case result := <-s.resultChan:
		return result, nil
	case err := <-s.errorChan:
		return nil, err
	case <-time.After(timeout):
		return nil, fmt.Errorf("timeout waiting for OAuth callback")
	}
}

func (s *OAuthServer) handleCallback(w http.ResponseWriter, r *http.Request) {
	log.Debug("Received OAuth callback")

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	query := r.URL.Query()
	code := query.Get("code")
	state := query.Get("state")
	errorParam := query.Get("error")

	if errorParam != "" {
		log.Errorf("OAuth error received: %s", errorParam)
		result := &OAuthResult{
			Error: errorParam,
		}
		s.sendResult(result)
		http.Error(w, fmt.Sprintf("OAuth error: %s", errorParam), http.StatusBadRequest)
		return
	}

	if code == "" {
		log.Error("No authorization code received")
		result := &OAuthResult{
			Error: "no_code",
		}
		s.sendResult(result)
		http.Error(w, "No authorization code received", http.StatusBadRequest)
		return
	}

	if state == "" {
		log.Error("No state parameter received")
		result := &OAuthResult{
			Error: "no_state",
		}
		s.sendResult(result)
		http.Error(w, "No state parameter received", http.StatusBadRequest)
		return
	}

	result := &OAuthResult{
		Code:  code,
		State: state,
	}
	s.sendResult(result)

	http.Redirect(w, r, "/success", http.StatusFound)
}

func (s *OAuthServer) handleSuccess(w http.ResponseWriter, r *http.Request) {
	log.Debug("Serving success page")

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	query := r.URL.Query()
	setupRequired := query.Get("setup_required") == "true"
	platformURL := query.Get("platform_url")
	if platformURL == "" {
		platformURL = "https://console.anthropic.com/"
	}

	successHTML := s.generateSuccessHTML(setupRequired, platformURL)

	_, err := w.Write([]byte(successHTML))
	if err != nil {
		log.Errorf("Failed to write success page: %v", err)
	}
}

func (s *OAuthServer) generateSuccessHTML(setupRequired bool, platformURL string) string {

	parsed, err := url.Parse(platformURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		platformURL = "https://console.anthropic.com/"
	}
	platformURL = stdhtml.EscapeString(platformURL)
	html := LoginSuccessHtml

	html = strings.Replace(html, "{{PLATFORM_URL}}", platformURL, -1)

	if setupRequired {
		setupNotice := strings.Replace(SetupNoticeHtml, "{{PLATFORM_URL}}", platformURL, -1)
		html = strings.Replace(html, "{{SETUP_NOTICE}}", setupNotice, 1)
	} else {
		html = strings.Replace(html, "{{SETUP_NOTICE}}", "", 1)
	}

	return html
}

func (s *OAuthServer) sendResult(result *OAuthResult) {
	select {
	case s.resultChan <- result:
		log.Debug("OAuth result sent to channel")
	default:
		log.Warn("OAuth result channel is full, result dropped")
	}
}

func (s *OAuthServer) isPortAvailable() bool {
	addr := fmt.Sprintf(":%d", s.port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return false
	}
	defer func() {
		_ = listener.Close()
	}()
	return true
}

func (s *OAuthServer) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

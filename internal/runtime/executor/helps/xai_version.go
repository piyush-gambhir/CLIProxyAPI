package helps

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	log "github.com/sirupsen/logrus"
)

const (


	DefaultXAIFallbackClientVersion = "1.0.46"


	XAIClientVersionServerFloor = "1.0.13"

	XAIVersionRefreshInterval = 3 * time.Hour

	XAIVersionFetchTimeout = 10 * time.Second
)

var (
	xaiNPMRegistryURL = "https://registry.npmjs.org/@xai-official/grok/latest"
)

var (
	cachedXAIClientVersion = DefaultXAIFallbackClientVersion
	xaiVersionProxyURL     string
	xaiClientVersionMu     sync.RWMutex
	xaiUpdaterCancel       context.CancelFunc
	xaiVersionRefreshed    chan struct{}
)




func GetXAIClientVersion() string {
	xaiClientVersionMu.RLock()
	defer xaiClientVersionMu.RUnlock()
	return cachedXAIClientVersion
}




func StartXAIVersionUpdater(ctx context.Context, proxyURL string) {
	if ctx == nil {
		ctx = context.Background()
	}
	runCtx, cancel := context.WithCancel(ctx)

	xaiClientVersionMu.Lock()
	if xaiUpdaterCancel != nil {
		xaiUpdaterCancel()
	}
	xaiUpdaterCancel = cancel
	xaiVersionProxyURL = strings.TrimSpace(proxyURL)
	xaiClientVersionMu.Unlock()

	go runXAIVersionUpdater(runCtx)
}

func runXAIVersionUpdater(ctx context.Context) {
	refreshXAIClientVersion(ctx)

	ticker := time.NewTicker(XAIVersionRefreshInterval)
	defer ticker.Stop()

	log.Infof("periodic Grok CLI version refresh started (interval=%s)", XAIVersionRefreshInterval)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			refreshXAIClientVersion(ctx)
		}
	}
}

func refreshXAIClientVersion(ctx context.Context) {
	defer notifyXAIVersionRefreshed()

	version, errFetch := FetchXAINPMLatestVersion(ctx, nil)
	if errFetch != nil {
		log.WithError(errFetch).Warn("failed to fetch latest Grok CLI version from npm, keeping fallback/cached version")
		return
	}

	xaiClientVersionMu.Lock()
	changed := cachedXAIClientVersion != version
	if changed {
		cachedXAIClientVersion = version
	}
	xaiClientVersionMu.Unlock()

	if changed {
		log.WithField("version", version).Info("updated Grok CLI client version from npm")
	}
}



func FetchXAINPMLatestVersion(ctx context.Context, client *http.Client) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	fetchCtx, cancel := context.WithTimeout(ctx, XAIVersionFetchTimeout)
	defer cancel()

	xaiClientVersionMu.RLock()
	registryURL := xaiNPMRegistryURL
	xaiClientVersionMu.RUnlock()

	req, errReq := http.NewRequestWithContext(fetchCtx, http.MethodGet, registryURL, nil)
	if errReq != nil {
		return "", fmt.Errorf("create npm registry request: %w", errReq)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "CLIProxyAPI")

	if client == nil {
		client = xaiVersionHTTPClient()
	}

	resp, errDo := client.Do(req)
	if errDo != nil {
		return "", fmt.Errorf("npm registry request failed: %w", errDo)
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			log.Errorf("failed to close npm response body: %v", errClose)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("npm registry returned HTTP %d", resp.StatusCode)
	}

	body, errRead := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if errRead != nil {
		return "", fmt.Errorf("read npm registry response: %w", errRead)
	}

	version := strings.Clone(strings.TrimSpace(ojson.GetBytesNoCopy(body, "version").String()))
	if version == "" {
		return "", errors.New("version not found in npm response")
	}
	if !acceptableXAIClientVersion(version) {
		return "", fmt.Errorf("npm registry returned unacceptable Grok CLI version %s", strconv.Quote(version))
	}

	return version, nil
}

func xaiVersionHTTPClient() *http.Client {
	xaiClientVersionMu.RLock()
	proxyURL := xaiVersionProxyURL
	xaiClientVersionMu.RUnlock()
	if strings.TrimSpace(proxyURL) == "" {
		return &http.Client{Timeout: XAIVersionFetchTimeout}
	}
	return NewProxyAwareHTTPClient(context.Background(), &config.Config{SDKConfig: config.SDKConfig{ProxyURL: proxyURL}}, nil, XAIVersionFetchTimeout)
}

func acceptableXAIClientVersion(version string) bool {
	if !isStrictXAISemver(version) {
		return false
	}
	return xaiVersionAtLeast(version, XAIClientVersionServerFloor)
}

func isStrictXAISemver(version string) bool {
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		for _, ch := range part {
			if ch < '0' || ch > '9' {
				return false
			}
		}
	}
	return true
}

func xaiVersionAtLeast(got, floor string) bool {
	gotParts := strings.Split(got, ".")
	floorParts := strings.Split(floor, ".")
	for i := 0; i < len(gotParts) || i < len(floorParts); i++ {
		var g, f int
		var errG, errF error
		if i < len(gotParts) {
			g, errG = strconv.Atoi(gotParts[i])
			if errG != nil {
				return false
			}
		}
		if i < len(floorParts) {
			f, errF = strconv.Atoi(floorParts[i])
			if errF != nil {
				return false
			}
		}
		if g != f {
			return g > f
		}
	}
	return true
}

func notifyXAIVersionRefreshed() {
	xaiClientVersionMu.RLock()
	ch := xaiVersionRefreshed
	xaiClientVersionMu.RUnlock()
	if ch == nil {
		return
	}
	select {
	case ch <- struct{}{}:
	default:
	}
}


func OverrideXAINPMRegistryURLForTest(url string) func() {
	xaiClientVersionMu.Lock()
	oldURL := xaiNPMRegistryURL
	xaiNPMRegistryURL = url
	xaiClientVersionMu.Unlock()
	return func() {
		xaiClientVersionMu.Lock()
		xaiNPMRegistryURL = oldURL
		xaiClientVersionMu.Unlock()
	}
}


func SetXAIClientVersionForTest(version string) func() {
	xaiClientVersionMu.Lock()
	old := cachedXAIClientVersion
	cachedXAIClientVersion = version
	xaiClientVersionMu.Unlock()
	return func() {
		xaiClientVersionMu.Lock()
		cachedXAIClientVersion = old
		xaiClientVersionMu.Unlock()
	}
}


func SetXAIVersionRefreshedHookForTest(ch chan struct{}) func() {
	xaiClientVersionMu.Lock()
	old := xaiVersionRefreshed
	xaiVersionRefreshed = ch
	xaiClientVersionMu.Unlock()
	return func() {
		xaiClientVersionMu.Lock()
		xaiVersionRefreshed = old
		xaiClientVersionMu.Unlock()
	}
}

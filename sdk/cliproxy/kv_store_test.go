package cliproxy

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/home"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/kvstore"
)

func TestServiceApplyKVStoreConfig_LifecycleAndHotReload(t *testing.T) {
	t.Cleanup(func() {
		home.ClearCurrent()
		_ = kvstore.ShutdownLocal()
	})

	dir := t.TempDir()
	authDir := filepath.Join(dir, "auths")
	defaultAuthDBPath := filepath.Join(authDir, config.DefaultKVStorePath)
	dbPath1 := filepath.Join(dir, "kv1.db")
	dbPath2 := filepath.Join(dir, "kv2.db")

	unmanagedSvc := &Service{}
	unmanagedSvc.applyConfigUpdate(&config.Config{
		AuthDir: authDir,
	})
	if client, active, errClient := kvstore.CurrentKVClient(); client != nil || active || errClient != nil {
		t.Fatalf("CurrentKVClient(unmanaged applyConfigUpdate) = %v, %v, %v; want nil, false, nil", client, active, errClient)
	}

	svc := &Service{}
	svc.applyKVStoreConfig(&config.Config{
		AuthDir: authDir,
	})
	clientDefault, active, errClient := kvstore.CurrentKVClient()
	if errClient != nil || !active || clientDefault == nil {
		t.Fatalf("CurrentKVClient(default enabled) = %v, %v, %v; want client, true, nil", clientDefault, active, errClient)
	}
	if _, errStat := os.Stat(defaultAuthDBPath); errStat != nil {
		t.Fatalf("expected defaultAuthDBPath %s to exist: %v", defaultAuthDBPath, errStat)
	}

	svc.applyKVStoreConfig(&config.Config{
		AuthDir: authDir,
		KVStore: config.KVStoreConfig{Disabled: false, Path: dbPath1},
	})
	client1, active, errClient := kvstore.CurrentKVClient()
	if errClient != nil || !active || client1 == nil || client1 == clientDefault {
		t.Fatalf("CurrentKVClient(explicit path) = %v, %v, %v; want new client, true, nil", client1, active, errClient)
	}
	if _, errStat := os.Stat(dbPath1); errStat != nil {
		t.Fatalf("expected dbPath1 %s to exist: %v", dbPath1, errStat)
	}

	svc.applyKVStoreConfig(&config.Config{
		AuthDir: authDir,
		KVStore: config.KVStoreConfig{Disabled: false, Path: dbPath1},
	})
	clientSame, active, errClient := kvstore.CurrentKVClient()
	if errClient != nil || !active || clientSame != client1 {
		t.Fatalf("same path reload did not reuse store: got %v want %v", clientSame, client1)
	}

	svc.applyKVStoreConfig(&config.Config{
		KVStore: config.KVStoreConfig{Disabled: false, Path: dbPath2},
	})
	client2, active, errClient := kvstore.CurrentKVClient()
	if errClient != nil || !active || client2 == nil || client2 == client1 {
		t.Fatalf("path switch failed: client=%v active=%v err=%v", client2, active, errClient)
	}

	svc.applyKVStoreConfig(&config.Config{
		KVStore: config.KVStoreConfig{Disabled: true, Path: dbPath2},
	})
	if client, active, errClient := kvstore.CurrentKVClient(); client != nil || active || errClient != nil {
		t.Fatalf("CurrentKVClient(disabled) = %v, %v, %v; want nil, false, nil", client, active, errClient)
	}

	svc.applyKVStoreConfig(&config.Config{
		KVStore: config.KVStoreConfig{Disabled: false, Path: dbPath2},
	})
	svc.applyKVStoreConfig(nil)
	if client, active, errClient := kvstore.CurrentKVClient(); client != nil || active || errClient != nil {
		t.Fatalf("CurrentKVClient(nil cfg) = %v, %v, %v; want nil, false, nil", client, active, errClient)
	}

	svc.applyConfigUpdate(&config.Config{
		KVStore: config.KVStoreConfig{Disabled: false, Path: dbPath1},
	})
	if client, active, errClient := kvstore.CurrentKVClient(); errClient != nil || !active || client == nil {
		t.Fatalf("CurrentKVClient(after applyConfigUpdate) = %v, %v, %v; want client, true, nil", client, active, errClient)
	}
	if errShutdown := svc.Shutdown(context.Background()); errShutdown != nil {
		t.Fatalf("Shutdown() error = %v", errShutdown)
	}
	if client, active, errClient := kvstore.CurrentKVClient(); client != nil || active || errClient != nil {
		t.Fatalf("CurrentKVClient(after Shutdown) = %v, %v, %v; want nil, false, nil", client, active, errClient)
	}

	var nilSvc *Service
	nilSvc.applyKVStoreConfig(&config.Config{
		KVStore: config.KVStoreConfig{Disabled: false, Path: dbPath1},
	})
	if errNilShutdown := nilSvc.shutdownKVStore(); errNilShutdown != nil {
		t.Fatalf("nilSvc.shutdownKVStore() error = %v; want nil", errNilShutdown)
	}
}

func TestServiceApplyKVStoreConfig_HomeModeNeverOpensBBolt(t *testing.T) {
	t.Cleanup(func() {
		home.ClearCurrent()
		_ = kvstore.ShutdownLocal()
	})

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "home-should-not-create.db")
	cfg := &config.Config{
		Home:    config.HomeConfig{Enabled: true},
		KVStore: config.KVStoreConfig{Disabled: false, Path: dbPath},
	}
	forceHomeRuntimeConfig(cfg)
	if !cfg.KVStore.Disabled {
		t.Fatal("forceHomeRuntimeConfig did not set KVStore.Disabled = true")
	}

	svc := &Service{}
	svc.applyKVStoreConfig(&config.Config{
		Home:    config.HomeConfig{Enabled: true},
		KVStore: config.KVStoreConfig{Disabled: false, Path: dbPath},
	})
	if _, errStat := os.Stat(dbPath); !os.IsNotExist(errStat) {
		t.Fatalf("Home mode created bbolt file at %s (err=%v)", dbPath, errStat)
	}

	localBeforeHomePath := filepath.Join(dir, "local-before-home.db")
	svc.applyKVStoreConfig(&config.Config{
		KVStore: config.KVStoreConfig{Disabled: false, Path: localBeforeHomePath},
	})
	if client, active, errClient := kvstore.CurrentKVClient(); errClient != nil || !active || client == nil {
		t.Fatalf("expected local store active before Home activation, got %v, %v, %v", client, active, errClient)
	}

	homeActivePath := filepath.Join(dir, "home-active-should-not-create.db")
	home.SetCurrent(&home.Client{})
	svc.applyKVStoreConfig(&config.Config{
		KVStore: config.KVStoreConfig{Disabled: false, Path: homeActivePath},
	})
	if _, errStat := os.Stat(homeActivePath); !os.IsNotExist(errStat) {
		t.Fatalf("active home.Current() created bbolt file at %s (err=%v)", homeActivePath, errStat)
	}
	home.ClearCurrent()
	if client, active, errClient := kvstore.CurrentKVClient(); client != nil || active || errClient != nil {
		t.Fatalf("expected local store shut down when home.Current() was active, got %v, %v, %v", client, active, errClient)
	}
}

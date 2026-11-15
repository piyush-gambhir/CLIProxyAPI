package store

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage/filesystem"
	"github.com/go-git/go-git/v6/storage/memory"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/auth/codex"
	log "github.com/sirupsen/logrus"
)




func (s *GitTokenStore) RefreshClaim(ctx context.Context, key string) (claimed bool, errResult error) {
	errResult = s.withRefreshRepository(ctx, func(repo *git.Repository) error {
		ref := refreshGitRef("claim", key)
		if hash, errLookup := s.refreshRemoteRef(ctx, repo, ref); errLookup != nil {
			return errLookup
		} else if !hash.IsZero() {
			return nil
		}
		nonce := make([]byte, 32)
		if _, errRand := rand.Read(nonce); errRand != nil {
			return errRand
		}
		errPublish := s.publishRefreshRecord(ctx, repo, ref, fmt.Sprintf("refresh claim %x", nonce))
		if errPublish == nil {
			claimed = true
			return nil
		}

		if hash, errLookup := s.refreshRemoteRef(ctx, repo, ref); errLookup == nil && !hash.IsZero() {
			return nil
		}
		return errPublish
	})
	return claimed, errResult
}

func (s *GitTokenStore) RefreshRead(ctx context.Context, key string) (receipt []byte, errResult error) {
	errResult = s.withRefreshRepository(ctx, func(repo *git.Repository) error {
		ref := refreshGitRef("receipt", key)
		hash, errLookup := s.refreshRemoteRef(ctx, repo, ref)
		if errLookup != nil {
			return errLookup
		}
		if hash.IsZero() {
			claim, errClaim := s.refreshRemoteRef(ctx, repo, refreshGitRef("claim", key))
			if errClaim != nil {
				return errClaim
			}
			if !claim.IsZero() {
				receipt = []byte(codex.PendingRefreshReceipt)
			}
			return nil
		}
		if errFetch := repo.FetchContext(ctx, &git.FetchOptions{RemoteName: "origin", ClientOptions: s.gitClientOptions(), RefSpecs: []config.RefSpec{config.RefSpec(ref.String() + ":" + ref.String())}, Tags: git.NoTags}); errFetch != nil && !errors.Is(errFetch, git.NoErrAlreadyUpToDate) {
			return errFetch
		}
		commit, errCommit := repo.CommitObject(hash)
		if errCommit != nil {
			return errCommit
		}
		receipt = []byte(commit.Message)
		return nil
	})
	return receipt, errResult
}

func (s *GitTokenStore) RefreshComplete(ctx context.Context, key string, receipt []byte) error {
	return s.withRefreshRepository(ctx, func(repo *git.Repository) error {
		return s.publishRefreshRecord(ctx, repo, refreshGitRef("receipt", key), string(receipt))
	})
}

func refreshGitRef(kind, key string) plumbing.ReferenceName {
	return plumbing.ReferenceName("refs/tags/cliproxy-refresh/" + kind + "/" + key)
}

func (s *GitTokenStore) withRefreshRepository(ctx context.Context, run func(*git.Repository) error) (errResult error) {
	if s == nil || strings.TrimSpace(s.remote) == "" {
		return fmt.Errorf("git refresh journal is unavailable")
	}




	repo, errInit := git.Init(memory.NewStorage(), nil)
	if errInit != nil {
		return errInit
	}
	defer func() { errResult = errors.Join(errResult, repo.Close()) }()
	if _, errRemote := repo.CreateRemote(&config.RemoteConfig{Name: "origin", URLs: []string{s.remote}}); errRemote != nil {
		return errRemote
	}


	endpoint, errParse := transport.ParseURL(s.remote)
	if errParse != nil {
		return errParse
	}
	if endpoint.Scheme == "file" {


		remoteStorage, errLoad := transport.DefaultLoader.Load(endpoint)
		if errLoad != nil {
			return errLoad
		}
		remoteFilesystem, ok := remoteStorage.(*filesystem.Storage)
		if !ok {
			return fmt.Errorf("local git refresh journal requires filesystem storage")
		}
		defer func() { errResult = errors.Join(errResult, remoteFilesystem.Close()) }()
		canonical, errCanonical := filepath.EvalSymlinks(remoteFilesystem.Filesystem().Root())
		if errCanonical != nil {
			return errCanonical
		}
		return codex.WithFileLock(filepath.Join(canonical, ".cliproxy-refresh"), func() error { return run(repo) })
	}
	return run(repo)
}

func (s *GitTokenStore) refreshRemoteRef(ctx context.Context, repo *git.Repository, name plumbing.ReferenceName) (plumbing.Hash, error) {
	remote, errRemote := repo.Remote("origin")
	if errRemote != nil {
		return plumbing.ZeroHash, errRemote
	}
	refs, errList := remote.ListContext(ctx, &git.ListOptions{ClientOptions: s.gitClientOptions()})
	if errList != nil {
		return plumbing.ZeroHash, errList
	}
	for _, ref := range refs {
		if ref.Name() == name {
			return ref.Hash(), nil
		}
	}
	return plumbing.ZeroHash, nil
}

func (s *GitTokenStore) publishRefreshRecord(ctx context.Context, repo *git.Repository, ref plumbing.ReferenceName, message string) error {
	tree := &object.Tree{}
	treeObject := repo.Storer.NewEncodedObject()
	if errEncode := tree.Encode(treeObject); errEncode != nil {
		return errEncode
	}
	treeHash, errTree := repo.Storer.SetEncodedObject(treeObject)
	if errTree != nil {
		return errTree
	}
	signature := object.Signature{Name: "CLIProxyAPI", Email: "refresh@cliproxyapi.local", When: time.Now()}
	commit := &object.Commit{Author: signature, Committer: signature, Message: message, TreeHash: treeHash}
	encoded := repo.Storer.NewEncodedObject()
	if errEncode := commit.Encode(encoded); errEncode != nil {
		return errEncode
	}
	hash, errStore := repo.Storer.SetEncodedObject(encoded)
	if errStore != nil {
		return errStore
	}

	localRef := plumbing.ReferenceName("refs/cliproxy-refresh-pending/" + hash.String())
	if errReference := repo.Storer.SetReference(plumbing.NewHashReference(localRef, hash)); errReference != nil {
		return errReference
	}
	defer func() {
		if errRemove := repo.Storer.RemoveReference(localRef); errRemove != nil {
			log.Warnf("remove temporary refresh journal reference: %v", errRemove)
		}
	}()
	return repo.PushContext(ctx, &git.PushOptions{RemoteName: "origin", ClientOptions: s.gitClientOptions(), RefSpecs: []config.RefSpec{config.RefSpec(localRef.String() + ":" + ref.String())}})
}

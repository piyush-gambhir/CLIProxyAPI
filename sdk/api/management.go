



package api

import (
	"context"

	"github.com/gin-gonic/gin"
	internalmanagement "github.com/router-for-me/CLIProxyAPI/v8/internal/api/handlers/management"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/config"
)


type Handler = internalmanagement.Handler


type ManagementTokenRequester interface {
	RequestAnthropicToken(*gin.Context)
	RequestCodexToken(*gin.Context)
	RequestAntigravityToken(*gin.Context)
	RequestKimiToken(*gin.Context)
	GetAuthStatus(c *gin.Context)
	PostOAuthCallback(c *gin.Context)
}

type managementTokenRequester struct {
	handler *Handler
}


func NewHandler(cfg *config.Config, configFilePath string, manager *coreauth.Manager) *Handler {
	return internalmanagement.NewHandler(cfg, configFilePath, manager)
}


func NewHandlerWithoutConfigFilePath(cfg *config.Config, manager *coreauth.Manager) *Handler {
	return internalmanagement.NewHandlerWithoutConfigFilePath(cfg, manager)
}


func NewManagementTokenRequester(cfg *config.Config, manager *coreauth.Manager) ManagementTokenRequester {
	return &managementTokenRequester{
		handler: NewHandlerWithoutConfigFilePath(cfg, manager),
	}
}

func (m *managementTokenRequester) RequestAnthropicToken(c *gin.Context) {
	m.handler.RequestAnthropicToken(c)
}

func (m *managementTokenRequester) RequestCodexToken(c *gin.Context) {
	m.handler.RequestCodexToken(c)
}

func (m *managementTokenRequester) RequestAntigravityToken(c *gin.Context) {
	m.handler.RequestAntigravityToken(c)
}

func (m *managementTokenRequester) RequestKimiToken(c *gin.Context) {
	m.handler.RequestKimiToken(c)
}

func (m *managementTokenRequester) GetAuthStatus(c *gin.Context) {
	m.handler.GetAuthStatus(c)
}

func (m *managementTokenRequester) PostOAuthCallback(c *gin.Context) {
	m.handler.PostOAuthCallback(c)
}


func WriteConfig(path string, data []byte) error {
	return internalmanagement.WriteConfig(path, data)
}


func RegisterOAuthSession(state, provider string) {
	internalmanagement.RegisterOAuthSession(state, provider)
}


func SetOAuthSessionError(state, message string) {
	internalmanagement.SetOAuthSessionError(state, message)
}


func CompleteOAuthSession(state string) {
	internalmanagement.CompleteOAuthSession(state)
}


func CompleteOAuthSessionsByProvider(provider string) int {
	return internalmanagement.CompleteOAuthSessionsByProvider(provider)
}


func GetOAuthSession(state string) (provider string, status string, ok bool) {
	return internalmanagement.GetOAuthSession(state)
}


func IsOAuthSessionPending(state, provider string) bool {
	return internalmanagement.IsOAuthSessionPending(state, provider)
}


func ValidateOAuthState(state string) error {
	return internalmanagement.ValidateOAuthState(state)
}


func NormalizeOAuthProvider(provider string) (string, error) {
	return internalmanagement.NormalizeOAuthProvider(provider)
}


func WriteOAuthCallbackFile(authDir, provider, state, code, errorMessage string) (string, error) {
	return internalmanagement.WriteOAuthCallbackFile(authDir, provider, state, code, errorMessage)
}


func WriteOAuthCallbackFileForPendingSession(authDir, provider, state, code, errorMessage string) (string, error) {
	return internalmanagement.WriteOAuthCallbackFileForPendingSession(authDir, provider, state, code, errorMessage)
}


func PopulateAuthContext(ctx context.Context, c *gin.Context) context.Context {
	return internalmanagement.PopulateAuthContext(ctx, c)
}

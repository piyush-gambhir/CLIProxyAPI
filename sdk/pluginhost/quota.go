package pluginhost

import (
	"context"

	internalpluginhost "github.com/router-for-me/CLIProxyAPI/v8/internal/pluginhost"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)


type RegisteredQuotaProviderInfo = internalpluginhost.RegisteredQuotaProviderInfo


func (h *Host) QuotaProviders(ctx context.Context) []RegisteredQuotaProviderInfo {
	if h == nil || h.inner == nil {
		return nil
	}
	return h.inner.QuotaProviders(ctx)
}



func (h *Host) QuotaProviderPluginIDs(ctx context.Context) map[string]string {
	if h == nil || h.inner == nil {
		return nil
	}
	return h.inner.QuotaProviderPluginIDs(ctx)
}


func (h *Host) HasQuotaProviderContext(ctx context.Context, provider string) bool {
	return h != nil && h.inner != nil && h.inner.HasQuotaProviderContext(ctx, provider)
}


func (h *Host) HasQuotaProviderForPlugin(pluginID string) bool {
	return h != nil && h.inner != nil && h.inner.HasQuotaProviderForPlugin(pluginID)
}


func (h *Host) FetchQuota(ctx context.Context, req pluginapi.QuotaFetchRequest) (pluginapi.QuotaFetchResponse, bool, error) {
	if h == nil || h.inner == nil {
		return pluginapi.QuotaFetchResponse{}, false, nil
	}
	return h.inner.FetchQuota(ctx, req)
}


func (h *Host) FetchQuotaByPlugin(ctx context.Context, pluginID string, req pluginapi.QuotaFetchRequest) (pluginapi.QuotaFetchResponse, bool, error) {
	if h == nil || h.inner == nil {
		return pluginapi.QuotaFetchResponse{}, false, nil
	}
	return h.inner.FetchQuotaByPlugin(ctx, pluginID, req)
}


func (h *Host) ResetQuota(ctx context.Context, req pluginapi.QuotaResetRequest) (pluginapi.QuotaResetResponse, bool, error) {
	if h == nil || h.inner == nil {
		return pluginapi.QuotaResetResponse{}, false, nil
	}
	return h.inner.ResetQuota(ctx, req)
}


func (h *Host) ResetQuotaByPlugin(ctx context.Context, pluginID string, req pluginapi.QuotaResetRequest) (pluginapi.QuotaResetResponse, bool, error) {
	if h == nil || h.inner == nil {
		return pluginapi.QuotaResetResponse{}, false, nil
	}
	return h.inner.ResetQuotaByPlugin(ctx, pluginID, req)
}

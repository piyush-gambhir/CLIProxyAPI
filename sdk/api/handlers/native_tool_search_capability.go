package handlers

func (h *BaseAPIHandler) SupportsNativeToolSearchModel(model string) bool {
	if h == nil || h.AuthManager == nil {
		return false
	}
	return h.AuthManager.SupportsNativeToolSearchForModel(model)
}

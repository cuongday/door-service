package onvifmanager

func supportsAccessControl(endpoints []serviceEndpoint) bool {
	return findServiceEndpoint(endpoints, AccessControlNamespace) != ""
}

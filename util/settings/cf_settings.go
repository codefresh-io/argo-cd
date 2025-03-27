package settings

func (mgr *SettingsManager) GetKustomizeSetNamespaceEnabled() bool {
	argoCDCM, err := mgr.getConfigMap()
	if err != nil {
		return true
	}
	kustomizeSetNamespaceEnabled := argoCDCM.Data[kustomizeSetNamespaceEnabledKey]
	if kustomizeSetNamespaceEnabled == "" {
		// enabled by default because it is a breaking change to disable it
		return true
	}
	return kustomizeSetNamespaceEnabled == "true"
}

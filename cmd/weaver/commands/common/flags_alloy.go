// SPDX-License-Identifier: Apache-2.0

package common

// Flag descriptor factories for the `alloy cluster` command tree.
//
// FlagAlloyClusterSecretStore is registered hidden (deprecated, kept for backward
// compatibility). FlagPrometheusRemotes / FlagLokiRemotes use
// RepeatableStringFlagDefinition because their spec format embeds commas inside
// a single value (e.g. "name=x,url=y,username=z,labelProfile=eng").

func FlagClusterName() FlagDefinition[string] {
	return FlagDefinition[string]{
		Name:        "cluster-name",
		ShortName:   "",
		Description: "Cluster name for Alloy metrics/logs labels",
		Default:     "",
	}
}

func FlagMonitorBlockNode() FlagDefinition[bool] {
	return FlagDefinition[bool]{
		Name:        "monitor-block-node",
		ShortName:   "",
		Description: "Enable Block Node monitoring in Alloy",
		Default:     false,
	}
}

func FlagAlloyClusterSecretStore() FlagDefinition[string] {
	return FlagDefinition[string]{
		Name:        "cluster-secret-store",
		ShortName:   "",
		Description: "Name of the ClusterSecretStore resource",
		Default:     "vault-secret-store",
	}
}

func FlagPrometheusRemotes() RepeatableStringFlagDefinition {
	return RepeatableStringFlagDefinition{
		Name:      "add-prometheus-remote",
		ShortName: "",
		Description: "Add a Prometheus remote (format: name=<name>,url=<url>,username=<username>[,labelProfile=eng|ops]). Can be specified multiple times. " +
			"Default labelProfile is 'eng' (cluster label only). " +
			"Password is expected in K8s Secret 'grafana-alloy-secrets' under key 'PROMETHEUS_PASSWORD_<NAME>'",
		Default: nil,
	}
}

func FlagLokiRemotes() RepeatableStringFlagDefinition {
	return RepeatableStringFlagDefinition{
		Name:      "add-loki-remote",
		ShortName: "",
		Description: "Add a Loki remote (format: name=<name>,url=<url>,username=<username>[,labelProfile=eng|ops]). Can be specified multiple times. " +
			"Default labelProfile is 'eng' (cluster label only). " +
			"Password is expected in K8s Secret 'grafana-alloy-secrets' under key 'LOKI_PASSWORD_<NAME>'",
		Default: nil,
	}
}

func FlagPrometheusURL() FlagDefinition[string] {
	return FlagDefinition[string]{
		Name:        "prometheus-url",
		ShortName:   "",
		Description: "Prometheus remote write URL (deprecated: use --add-prometheus-remote)",
		Default:     "",
	}
}

func FlagPrometheusUsername() FlagDefinition[string] {
	return FlagDefinition[string]{
		Name:        "prometheus-username",
		ShortName:   "",
		Description: "Prometheus username (deprecated: use --add-prometheus-remote)",
		Default:     "",
	}
}

func FlagLokiURL() FlagDefinition[string] {
	return FlagDefinition[string]{
		Name:        "loki-url",
		ShortName:   "",
		Description: "Loki remote write URL (deprecated: use --add-loki-remote)",
		Default:     "",
	}
}

func FlagLokiUsername() FlagDefinition[string] {
	return FlagDefinition[string]{
		Name:        "loki-username",
		ShortName:   "",
		Description: "Loki username (deprecated: use --add-loki-remote)",
		Default:     "",
	}
}

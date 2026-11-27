// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"fmt"

	"github.com/automa-saga/errx"
	"github.com/joomcode/errorx"

	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
)

// remoteSet is one remote type's settings: the list and the deprecated single-URL keys.
type remoteSet struct {
	remotes  []models.AlloyRemoteConfig
	url      string
	username string
}

func (s remoteSet) isEmpty() bool {
	return len(s.remotes) == 0 && s.url == "" && s.username == ""
}

// remoteKind names one remote type's flags and --config keys for error messages.
type remoteKind struct {
	flagAdd, flagURL, flagUsername string
	keyList, keyURL, keyUsername   string
}

var (
	prometheusKind = remoteKind{
		flagAdd:      "--" + common.FlagPrometheusRemotes().Name,
		flagURL:      "--" + common.FlagPrometheusURL().Name,
		flagUsername: "--" + common.FlagPrometheusUsername().Name,
		keyList:      "alloy.prometheusRemotes",
		keyURL:       "alloy.prometheusUrl",
		keyUsername:  "alloy.prometheusUsername",
	}
	lokiKind = remoteKind{
		flagAdd:      "--" + common.FlagLokiRemotes().Name,
		flagURL:      "--" + common.FlagLokiURL().Name,
		flagUsername: "--" + common.FlagLokiUsername().Name,
		keyList:      "alloy.lokiRemotes",
		keyURL:       "alloy.lokiUrl",
		keyUsername:  "alloy.lokiUsername",
	}
)

// resolveAlloyConfig merges the alloy cluster install flags over the --config
// values: a flag wins when set, else the file. monitorBlockNodeSet reports
// whether --monitor-block-node was passed, because false is a value. Each remote
// type comes whole from one source: any flag for that type replaces the file's
// list and its legacy keys.
func resolveAlloyConfig(file, flags models.AlloyConfig, monitorBlockNodeSet bool) (models.AlloyConfig, error) {
	prometheusFlags := remoteSet{flags.PrometheusRemotes, flags.PrometheusURL, flags.PrometheusUsername}
	lokiFlags := remoteSet{flags.LokiRemotes, flags.LokiURL, flags.LokiUsername}

	hasLegacyFlags := prometheusFlags.url != "" || prometheusFlags.username != "" || lokiFlags.url != "" || lokiFlags.username != ""
	hasNewFlags := len(prometheusFlags.remotes) > 0 || len(lokiFlags.remotes) > 0
	if hasLegacyFlags && hasNewFlags {
		return models.AlloyConfig{}, errorx.IllegalArgument.New("cannot mix legacy flags (--prometheus-url, --prometheus-username, --loki-url, --loki-username) with new flags (--add-prometheus-remote, --add-loki-remote); use one style or the other")
	}

	out := file
	if monitorBlockNodeSet {
		out.MonitorBlockNode = flags.MonitorBlockNode
	}
	if flags.ClusterName != "" {
		out.ClusterName = flags.ClusterName
	}
	if flags.Environment != "" {
		out.Environment = flags.Environment
	}
	if flags.ClusterSecretStoreName != "" {
		out.ClusterSecretStoreName = flags.ClusterSecretStoreName
	}

	prometheus, err := resolveRemoteSet(prometheusKind,
		remoteSet{file.PrometheusRemotes, file.PrometheusURL, file.PrometheusUsername}, prometheusFlags)
	if err != nil {
		return models.AlloyConfig{}, err
	}
	out.PrometheusRemotes, out.PrometheusURL, out.PrometheusUsername = prometheus.remotes, prometheus.url, prometheus.username

	loki, err := resolveRemoteSet(lokiKind,
		remoteSet{file.LokiRemotes, file.LokiURL, file.LokiUsername}, lokiFlags)
	if err != nil {
		return models.AlloyConfig{}, err
	}
	out.LokiRemotes, out.LokiURL, out.LokiUsername = loki.remotes, loki.url, loki.username

	return out, nil
}

// resolveRemoteSet picks the flags' set when any flag for the type was given,
// else the file's. It rejects a set that would render a different remote than
// the operator wrote: a username with no URL yields no remote at all, and a list
// next to legacy keys silently drops the legacy remote.
func resolveRemoteSet(kind remoteKind, file, flags remoteSet) (remoteSet, error) {
	if !flags.isEmpty() {
		if flags.username != "" && flags.url == "" {
			return remoteSet{}, errx.Decorate(
				errorx.IllegalArgument.New("%s needs %s", kind.flagUsername, kind.flagURL),
				reasons.InvalidArgument,
				fmt.Sprintf("Pass %s with %s", kind.flagURL, kind.flagUsername),
				fmt.Sprintf("Or drop %s and set %s next to %s in --config", kind.flagUsername, kind.keyUsername, kind.keyURL),
				fmt.Sprintf("Or use %s=name=<name>,url=<url>,username=<username>", kind.flagAdd))
		}
		return flags, nil
	}

	if len(file.remotes) > 0 && (file.url != "" || file.username != "") {
		return remoteSet{}, errx.Decorate(
			errorx.IllegalArgument.New("--config sets both %s and the deprecated %s/%s", kind.keyList, kind.keyURL, kind.keyUsername),
			reasons.InvalidArgument,
			fmt.Sprintf("Remove %s and %s from --config; %s replaces them", kind.keyURL, kind.keyUsername, kind.keyList))
	}
	if file.username != "" && file.url == "" {
		return remoteSet{}, errx.Decorate(
			errorx.IllegalArgument.New("--config sets %s without %s", kind.keyUsername, kind.keyURL),
			reasons.InvalidArgument,
			fmt.Sprintf("Add %s to --config, or list the remote under %s", kind.keyURL, kind.keyList))
	}
	return file, nil
}

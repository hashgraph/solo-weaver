// SPDX-License-Identifier: Apache-2.0

package consensus

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/automa-saga/errx"
	"github.com/automa-saga/logx"
	cnkeys "github.com/hashgraph/solo-weaver/internal/consensus/keys"
	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
)

// keyCluster is the cluster access resolveKeySecrets needs: labelled Secrets,
// plus the node's live ConsensusCapsule.
type keyCluster interface {
	cnkeys.SecretClient
	GetResourceNestedString(ctx context.Context, apiVersion, kind, namespace, name string, fields ...string) (string, error)
}

// newKeyCluster is a var so tests can stub cluster access.
var newKeyCluster = func() (keyCluster, error) {
	return kube.NewClient()
}

// liveSecretNames reads the secret names the node's ConsensusCapsule already
// references. A node that is not installed yet, or a capsule that cannot be read,
// yields empty names, and the caller falls back to the labelled secrets.
func liveSecretNames(ctx context.Context, c keyCluster, custom *models.ConsensusNodeInputs) (signing, grpc, admin string) {
	apiVersion := kube.SoloOperatorGroup + "/" + kube.SoloOperatorVersion
	capsule := models.ConsensusCapsuleName(custom.OrbitName, custom.NodeId)
	var readErr error
	get := func(field string) string {
		if readErr != nil {
			return ""
		}
		v, err := c.GetResourceNestedString(ctx, apiVersion, string(kube.KindConsensusCapsule), custom.Namespace, capsule,
			"spec", "secrets", "secretResources", field, "secretName")
		readErr = err
		return v
	}
	signing, grpc, admin = get("signingPrivateKey"), get("grpcTlsPrivateKey"), get("adminPublicKey")
	if readErr != nil {
		// A missing capsule is not an error, so this is a real read failure.
		logx.As().Warn().Err(readErr).Str("capsule", capsule).
			Msg("Cannot read the live ConsensusCapsule; using the labelled key secrets")
		return "", "", ""
	}
	return signing, grpc, admin
}

const keySecretLookupTimeout = 30 * time.Second

// resolveKeySecrets fills in the signing, gRPC TLS and admin public key secret
// names the operator did not pass. Precedence, per key type:
//
//  1. the name given by flag;
//  2. the secret the node's live ConsensusCapsule already references, so a
//     reinstall keeps the keys in use even while a rotation is staged beside them;
//  3. the one secret `consensus keys import` created for this node (matched by
//     labels), for a first install. Several generations stop the install and list
//     them, since only the operator can say which to use.
//
// The label-matched secret has its content checked against its name and
// fingerprint.
//
// The admin public key secret is optional: when none exists, it is left empty and
// the capsule carries no admin key reference. Several admin secrets stop the install.
func resolveKeySecrets(ctx context.Context, client keyCluster, custom *models.ConsensusNodeInputs) error {
	// Without the orbit name the live-capsule lookup reads the wrong capsule and
	// silently falls back to the labels.
	if custom.OrbitName == "" {
		return errx.Decorate(errorx.IllegalState.New("orbit name is not set before the key secret lookup"), reasons.Internal)
	}
	liveSigning, liveGrpc, liveAdmin := liveSecretNames(ctx, client, custom)
	pick := func(t cnkeys.KeyType, current *string, live string, optional bool) error {
		if *current != "" {
			return nil
		}
		if live != "" {
			*current = live
			return nil
		}
		secret, err := cnkeys.ResolveSecret(ctx, client, custom.Namespace, custom.NodeId, t, cnkeys.ResolveOptions{})
		if err != nil {
			if optional && hasNone(err) {
				return nil
			}
			if optional {
				return errx.Decorate(err, reasons.PreconditionNotMet,
					"Name the admin public key secret to use with --admin-key-secret",
					fmt.Sprintf("Check the admin secret with `consensus keys verify --from-cluster --admin --namespace %s --node-id %d`",
						custom.Namespace, custom.NodeId))
			}
			return errx.Decorate(err, reasons.PreconditionNotMet,
				"Import the node's keys first with `consensus keys import` (or `consensus keys init`)",
				"Or name the secret yourself with --signing-secret, --grpc-tls-secret or --admin-key-secret")
		}
		*current = secret.Name
		return nil
	}

	if err := pick(cnkeys.KeyTypeGossip, &custom.SigningSecret, liveSigning, false); err != nil {
		return err
	}
	if err := pick(cnkeys.KeyTypeGrpc, &custom.GrpcTlsSecret, liveGrpc, false); err != nil {
		return err
	}
	return pick(cnkeys.KeyTypeAdmin, &custom.AdminKeySecret, liveAdmin, true)
}

// hasNone reports whether a resolve error means no secret of the type exists, as
// opposed to several, a bad secret or a failed lookup.
func hasNone(err error) bool {
	return err != nil && strings.Contains(err.Error(), "key secret found for node")
}

func lookupContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), keySecretLookupTimeout)
}

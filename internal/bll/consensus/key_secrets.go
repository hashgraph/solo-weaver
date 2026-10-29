// SPDX-License-Identifier: Apache-2.0

package consensus

import (
	"context"
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
	get := func(field string) string {
		v, _ := c.GetResourceNestedString(ctx, apiVersion, string(kube.KindConsensusCapsule), custom.Namespace, capsule,
			"spec", "secrets", "secretResources", field, "secretName")
		return v
	}
	return get("signingPrivateKey"), get("grpcTlsPrivateKey"), get("adminPublicKey")
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
// The admin public key secret is optional: when none exists, or several exist and
// none is named, it is left empty and the capsule carries no admin key reference.
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
			if optional {
				if hasMultiple(err) {
					logx.As().Warn().Err(err).Str("keyType", string(t)).
						Msg("Several key secrets found and none named; leaving the admin key reference out (pass --admin-key-secret to choose one)")
				}
				return nil
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

// hasMultiple reports whether a resolve error means several generations exist
// (which is never ignored), as opposed to none.
func hasMultiple(err error) bool {
	return err != nil && strings.Contains(err.Error(), "key secrets found")
}

func lookupContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), keySecretLookupTimeout)
}

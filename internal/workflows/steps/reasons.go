// SPDX-License-Identifier: Apache-2.0

package steps

import "github.com/automa-saga/errx"

// ReasonHostCommandFailed - a command run on the host (kubeadm, swapoff) exited
// with an error; its output in the log says why.
const ReasonHostCommandFailed errx.Reason = "HostCommandFailed"

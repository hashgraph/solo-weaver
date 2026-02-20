// SPDX-License-Identifier: Apache-2.0

package bll

type Option[T any] func(*T) error

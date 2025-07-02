/*
 * Copyright 2016-2023 Hedera Hashgraph, LLC
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package otl

import (
	"go.opentelemetry.io/otel/attribute"
	"sync"
)

// attributeCache caches the list of common attributes as a key-value map
// This is for internal use only so it is not exposed
// Default attributes are set using WithSpanAttributes during initialization of a Otl instance
// Any other span attributes can be added to the Span after instantiation of a Span using StartSpan function
type attributeCache struct {
	mutex sync.Mutex
	cache map[attribute.Key]attribute.KeyValue
}

// newAttributeCache returns an instance of attributeCache with the set of given attributes
func newAttributeCache(attrs ...attribute.KeyValue) *attributeCache {
	at := &attributeCache{
		mutex: sync.Mutex{},
		cache: map[attribute.Key]attribute.KeyValue{},
	}

	at.add(attrs...)

	return at
}

// add adds an attribute
// It stores them in a map so by adding the same attribute it overwrites the old
func (a *attributeCache) add(attrs ...attribute.KeyValue) {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	for _, attr := range attrs {
		a.cache[attr.Key] = attr
	}
}

// getAll returns the list of attributes
func (a *attributeCache) getAll() []attribute.KeyValue {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	var attrs []attribute.KeyValue
	for _, attr := range a.cache {
		attrs = append(attrs, attr)
	}

	return attrs
}

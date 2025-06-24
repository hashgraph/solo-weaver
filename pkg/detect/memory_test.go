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

package detect

import (
	"github.com/cockroachdb/errors"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestMemoryManager_getSystemMemory(t *testing.T) {
	req := require.New(t)
	mm := NewMemoryManager(WithMemoryManagerLogger(nolog))

	sm, err := mm.GetSystemMemory()
	req.NoError(err)
	req.NotZero(sm.TotalBytes)
	req.Greater(sm.TotalBytes, sm.FreeBytes)
}

func TestMemoryManager_GetSystemMemory_Fail_FreeMemory(t *testing.T) {
	req := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	expected := SystemMemoryInfo{
		TotalBytes: uint64(1e9),
		FreeBytes:  uint64(0),
	}

	detector := NewMockMemoryDetector(ctrl)
	detector.EXPECT().TotalMemory().Return(expected.TotalBytes, nil)
	detector.EXPECT().FreeMemory().Return(expected.FreeBytes, errors.New("mock error"))
	mm := NewMemoryManager(WithSystemMemoryDetector(detector))
	sm, err := mm.GetSystemMemory()
	req.Error(err)
	req.Equal(expected.TotalBytes, sm.TotalBytes)
	req.Equal(expected.FreeBytes, sm.FreeBytes)
}

func TestMemoryManager_GetSystemMemory_Fail_TotalMemory(t *testing.T) {
	req := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	expected := SystemMemoryInfo{
		TotalBytes: uint64(0),
		FreeBytes:  uint64(0),
	}

	detector := NewMockMemoryDetector(ctrl)
	detector.EXPECT().TotalMemory().Return(expected.FreeBytes, errors.New("mock error"))
	mm := NewMemoryManager(WithSystemMemoryDetector(detector))
	sm, err := mm.GetSystemMemory()
	req.Error(err)
	req.Equal(expected.TotalBytes, sm.TotalBytes)
	req.Equal(expected.FreeBytes, sm.FreeBytes)
}

func TestMemoryManager_CheckJavaMemoryPair(t *testing.T) {
	req := require.New(t)
	mm := NewMemoryManager(WithMemoryManagerLogger(nolog))

	testCases := []struct {
		min    string
		max    string
		errMsg string
	}{
		{
			min:    "1m",
			max:    "2m",
			errMsg: "",
		},
		{
			min:    "2m",
			max:    "1m",
			errMsg: "illegal minimum & maximum memory allocation",
		},
		{
			min:    "1x",
			max:    "2m",
			errMsg: "failed to parse minSize",
		},
		{
			min:    "1m",
			max:    "2x",
			errMsg: "failed to parse maxSize",
		},
	}

	for _, test := range testCases {
		expectedMin, err := ParseMemorySizeInBytes(test.min)
		expectedMax, err := ParseMemorySizeInBytes(test.max)
		minBytes, maxBytes, err := mm.CheckJavaMemoryPair(test.min, test.max)
		if test.errMsg != "" {
			req.Error(err)
			req.Contains(err.Error(), test.errMsg)
			req.Zero(minBytes)
			req.Zero(maxBytes)
		} else {
			req.Equal(expectedMin, minBytes)
			req.Equal(expectedMax, maxBytes)
		}
	}
}

func TestMemoryManager_HasTotalMemory(t *testing.T) {
	req := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	testCases := []struct {
		reqBytes uint64
		total    uint64
		free     uint64
		errMsg   string
	}{
		{
			reqBytes: 1e6,
			total:    10e6,
			free:     5e6,
			errMsg:   "",
		},
		{
			reqBytes: 11e6,
			total:    10e6,
			free:     5e6,
			errMsg:   "exceeds currently available total physical memory",
		},
		{
			reqBytes: 1e6,
			total:    0,
			free:     0,
			errMsg:   "failed to retrieve system memory",
		},
	}

	for _, test := range testCases {
		mockDetector := NewMockMemoryDetector(ctrl)
		if test.total == 0 { // if total is 0, then return error from detector
			mockDetector.EXPECT().TotalMemory().Return(test.total, errors.New("mock error"))
		} else {
			mockDetector.EXPECT().TotalMemory().Return(test.total, nil)
			mockDetector.EXPECT().FreeMemory().Return(test.free, nil)
		}

		mm := NewMemoryManager(WithMemoryManagerLogger(nolog), WithSystemMemoryDetector(mockDetector))
		err := mm.HasTotalMemory(test.reqBytes)
		if test.errMsg != "" {
			req.Error(err)
			req.Contains(err.Error(), test.errMsg)
		}

	}
}

func TestMemoryManager_HasFreeMemory(t *testing.T) {
	req := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	testCases := []struct {
		reqBytes uint64
		total    uint64
		free     uint64
		errMsg   string
	}{
		{
			reqBytes: 1e6,
			total:    10e6,
			free:     5e6,
			errMsg:   "",
		},
		{
			reqBytes: 8e6,
			total:    10e6,
			free:     5e6,
			errMsg:   "exceeds currently available system memory",
		},
		{
			reqBytes: 1e6,
			total:    10e6,
			free:     0,
			errMsg:   "failed to retrieve system memory",
		},
	}

	for _, test := range testCases {
		mockDetector := NewMockMemoryDetector(ctrl)
		if test.free == 0 { // if free is 0, then return error from detector
			mockDetector.EXPECT().TotalMemory().Return(test.total, nil)
			mockDetector.EXPECT().FreeMemory().Return(test.free, errors.New("mock error"))
		} else {
			mockDetector.EXPECT().TotalMemory().Return(test.total, nil)
			mockDetector.EXPECT().FreeMemory().Return(test.free, nil)
		}

		mm := NewMemoryManager(WithMemoryManagerLogger(nolog), WithSystemMemoryDetector(mockDetector))
		err := mm.HasFreeMemory(test.reqBytes)
		if test.errMsg != "" {
			req.Error(err)
			req.Contains(err.Error(), test.errMsg)
		}

	}
}

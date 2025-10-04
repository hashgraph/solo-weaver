package kernel

import (
	"errors"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
)

func TestDefaultModule_Load(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockOps := NewMockmoduleOperations(ctrl)
	module := &defaultModule{
		name: "test_module",
		ops:  mockOps,
	}

	t.Run("should load module when not loaded and not persist", func(t *testing.T) {
		mockOps.EXPECT().isLoaded("test_module").Return(false, nil)
		mockOps.EXPECT().load("test_module").Return(nil)

		err := module.Load(false)
		assert.NoError(t, err)
	})

	t.Run("should load module when not loaded and persist", func(t *testing.T) {
		mockOps.EXPECT().isLoaded("test_module").Return(false, nil)
		mockOps.EXPECT().load("test_module").Return(nil)
		mockOps.EXPECT().persist("test_module").Return(nil)

		err := module.Load(true)
		assert.NoError(t, err)
	})

	t.Run("should not load module when already loaded but persist", func(t *testing.T) {
		mockOps.EXPECT().isLoaded("test_module").Return(true, nil)
		mockOps.EXPECT().persist("test_module").Return(nil)

		err := module.Load(true)
		assert.NoError(t, err)
	})

	t.Run("should not load module when already loaded and not persist", func(t *testing.T) {
		mockOps.EXPECT().isLoaded("test_module").Return(true, nil)

		err := module.Load(false)
		assert.NoError(t, err)
	})

	t.Run("should return error when isLoaded fails", func(t *testing.T) {
		expectedErr := errors.New("failed to check if module is loaded")
		mockOps.EXPECT().isLoaded("test_module").Return(false, expectedErr)

		err := module.Load(false)
		assert.Error(t, err)
		assert.Equal(t, expectedErr, err)
	})

	t.Run("should return error when load fails", func(t *testing.T) {
		expectedErr := errors.New("failed to load module")
		mockOps.EXPECT().isLoaded("test_module").Return(false, nil)
		mockOps.EXPECT().load("test_module").Return(expectedErr)

		err := module.Load(false)
		assert.Error(t, err)
		assert.Equal(t, expectedErr, err)
	})

	t.Run("should return error when persist fails", func(t *testing.T) {
		expectedErr := errors.New("failed to persist module")
		mockOps.EXPECT().isLoaded("test_module").Return(false, nil)
		mockOps.EXPECT().load("test_module").Return(nil)
		mockOps.EXPECT().persist("test_module").Return(expectedErr)

		err := module.Load(true)
		assert.Error(t, err)
		assert.Equal(t, expectedErr, err)
	})

	t.Run("should return error when persist fails and module already loaded", func(t *testing.T) {
		expectedErr := errors.New("failed to persist module")
		mockOps.EXPECT().isLoaded("test_module").Return(true, nil)
		mockOps.EXPECT().persist("test_module").Return(expectedErr)

		err := module.Load(true)
		assert.Error(t, err)
		assert.Equal(t, expectedErr, err)
	})
}

func TestDefaultModule_Unload(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockOps := NewMockmoduleOperations(ctrl)
	module := &defaultModule{
		name: "test_module",
		ops:  mockOps,
	}

	t.Run("should unload module when loaded and not unpersist", func(t *testing.T) {
		mockOps.EXPECT().isLoaded("test_module").Return(true, nil)
		mockOps.EXPECT().unload("test_module").Return(nil)

		err := module.Unload(false)
		assert.NoError(t, err)
	})

	t.Run("should unload module when loaded and unpersist", func(t *testing.T) {
		mockOps.EXPECT().unpersist("test_module").Return(nil)
		mockOps.EXPECT().isLoaded("test_module").Return(true, nil)
		mockOps.EXPECT().unload("test_module").Return(nil)

		err := module.Unload(true)
		assert.NoError(t, err)
	})

	t.Run("should not unload module when not loaded but unpersist", func(t *testing.T) {
		mockOps.EXPECT().unpersist("test_module").Return(nil)
		mockOps.EXPECT().isLoaded("test_module").Return(false, nil)

		err := module.Unload(true)
		assert.NoError(t, err)
	})

	t.Run("should not unload module when not loaded and not unpersist", func(t *testing.T) {
		mockOps.EXPECT().isLoaded("test_module").Return(false, nil)

		err := module.Unload(false)
		assert.NoError(t, err)
	})

	t.Run("should return error when unpersist fails", func(t *testing.T) {
		expectedErr := errors.New("failed to unpersist module")
		mockOps.EXPECT().unpersist("test_module").Return(expectedErr)

		err := module.Unload(true)
		assert.Error(t, err)
		assert.Equal(t, expectedErr, err)
	})

	t.Run("should return error when isLoaded fails", func(t *testing.T) {
		expectedErr := errors.New("failed to check if module is loaded")
		mockOps.EXPECT().isLoaded("test_module").Return(false, expectedErr)

		err := module.Unload(false)
		assert.Error(t, err)
		assert.Equal(t, expectedErr, err)
	})

	t.Run("should return error when isLoaded fails after unpersist", func(t *testing.T) {
		expectedErr := errors.New("failed to check if module is loaded")
		mockOps.EXPECT().unpersist("test_module").Return(nil)
		mockOps.EXPECT().isLoaded("test_module").Return(false, expectedErr)

		err := module.Unload(true)
		assert.Error(t, err)
		assert.Equal(t, expectedErr, err)
	})

	t.Run("should return error when unload fails", func(t *testing.T) {
		expectedErr := errors.New("failed to unload module")
		mockOps.EXPECT().isLoaded("test_module").Return(true, nil)
		mockOps.EXPECT().unload("test_module").Return(expectedErr)

		err := module.Unload(false)
		assert.Error(t, err)
		assert.Equal(t, expectedErr, err)
	})

	t.Run("should return error when unload fails after unpersist", func(t *testing.T) {
		expectedErr := errors.New("failed to unload module")
		mockOps.EXPECT().unpersist("test_module").Return(nil)
		mockOps.EXPECT().isLoaded("test_module").Return(true, nil)
		mockOps.EXPECT().unload("test_module").Return(expectedErr)

		err := module.Unload(true)
		assert.Error(t, err)
		assert.Equal(t, expectedErr, err)
	})
}

func TestDefaultModule_IsLoaded(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockOps := NewMockmoduleOperations(ctrl)
	module := &defaultModule{
		name: "test_module",
		ops:  mockOps,
	}

	t.Run("should return loaded=true, persisted=true when both are true", func(t *testing.T) {
		mockOps.EXPECT().isLoaded("test_module").Return(true, nil)
		mockOps.EXPECT().isPersisted("test_module").Return(true, nil)

		loaded, persisted, err := module.IsLoaded()
		assert.NoError(t, err)
		assert.True(t, loaded)
		assert.True(t, persisted)
	})

	t.Run("should return loaded=true, persisted=false when loaded but not persisted", func(t *testing.T) {
		mockOps.EXPECT().isLoaded("test_module").Return(true, nil)
		mockOps.EXPECT().isPersisted("test_module").Return(false, nil)

		loaded, persisted, err := module.IsLoaded()
		assert.NoError(t, err)
		assert.True(t, loaded)
		assert.False(t, persisted)
	})

	t.Run("should return loaded=false, persisted=true when not loaded but persisted", func(t *testing.T) {
		mockOps.EXPECT().isLoaded("test_module").Return(false, nil)
		mockOps.EXPECT().isPersisted("test_module").Return(true, nil)

		loaded, persisted, err := module.IsLoaded()
		assert.NoError(t, err)
		assert.False(t, loaded)
		assert.True(t, persisted)
	})

	t.Run("should return loaded=false, persisted=false when both are false", func(t *testing.T) {
		mockOps.EXPECT().isLoaded("test_module").Return(false, nil)
		mockOps.EXPECT().isPersisted("test_module").Return(false, nil)

		loaded, persisted, err := module.IsLoaded()
		assert.NoError(t, err)
		assert.False(t, loaded)
		assert.False(t, persisted)
	})

	t.Run("should return error when isLoaded fails", func(t *testing.T) {
		expectedErr := errors.New("failed to check if module is loaded")
		mockOps.EXPECT().isLoaded("test_module").Return(false, expectedErr)

		loaded, persisted, err := module.IsLoaded()
		assert.Error(t, err)
		assert.Equal(t, expectedErr, err)
		assert.False(t, loaded)
		assert.False(t, persisted)
	})

	t.Run("should return error when isPersisted fails but return loaded status", func(t *testing.T) {
		expectedErr := errors.New("failed to check if module is persisted")
		mockOps.EXPECT().isLoaded("test_module").Return(true, nil)
		mockOps.EXPECT().isPersisted("test_module").Return(false, expectedErr)

		loaded, persisted, err := module.IsLoaded()
		assert.Error(t, err)
		assert.Equal(t, expectedErr, err)
		assert.True(t, loaded)
		assert.False(t, persisted)
	})

	t.Run("should return error when isPersisted fails with false loaded status", func(t *testing.T) {
		expectedErr := errors.New("failed to check if module is persisted")
		mockOps.EXPECT().isLoaded("test_module").Return(false, nil)
		mockOps.EXPECT().isPersisted("test_module").Return(false, expectedErr)

		loaded, persisted, err := module.IsLoaded()
		assert.Error(t, err)
		assert.Equal(t, expectedErr, err)
		assert.False(t, loaded)
		assert.False(t, persisted)
	})
}

package service

import (
	"errors"
	"testing"

	"github.com/abbott/hardn/pkg/domain/model"
	"github.com/stretchr/testify/assert"
)

// mockLogsRepository implements the LogsRepository interface for testing
type mockLogsRepository struct {
	logs        []model.LogEntry
	config      *model.LogsConfig
	err         error
	printCalled bool
}

func (m *mockLogsRepository) GetLogs() ([]model.LogEntry, error) {
	return m.logs, m.err
}

func (m *mockLogsRepository) GetLogConfig() (*model.LogsConfig, error) {
	return m.config, m.err
}

func (m *mockLogsRepository) PrintLogs() error {
	m.printCalled = true
	return m.err
}

func TestNewLogsServiceImpl(t *testing.T) {
	mockRepo := &mockLogsRepository{}
	service := NewLogsServiceImpl(mockRepo)

	assert.NotNil(t, service)
	assert.Equal(t, mockRepo, service.repository)
}

func TestLogsServiceImpl_GetLogs(t *testing.T) {
	t.Run("success case", func(t *testing.T) {
		expectedLogs := []model.LogEntry{
			{Level: "INFO", Message: "Test message 1", Time: "2023-01-01T12:00:00Z"},
			{Level: "ERROR", Message: "Test error", Time: "2023-01-01T12:01:00Z"},
		}
		mockRepo := &mockLogsRepository{logs: expectedLogs, err: nil}
		service := NewLogsServiceImpl(mockRepo)

		logs, err := service.GetLogs()

		assert.NoError(t, err)
		assert.Equal(t, expectedLogs, logs)
	})

	t.Run("error case", func(t *testing.T) {
		expectedErr := errors.New("failed to retrieve logs")
		mockRepo := &mockLogsRepository{logs: nil, err: expectedErr}
		service := NewLogsServiceImpl(mockRepo)

		logs, err := service.GetLogs()

		assert.Error(t, err)
		assert.Equal(t, expectedErr, err)
		assert.Nil(t, logs)
	})
}

func TestLogsServiceImpl_GetLogConfig(t *testing.T) {
	t.Run("success case", func(t *testing.T) {
		expectedConfig := &model.LogsConfig{LogFilePath: "/var/log/app.log"}
		mockRepo := &mockLogsRepository{config: expectedConfig, err: nil}
		service := NewLogsServiceImpl(mockRepo)

		config, err := service.GetLogConfig()

		assert.NoError(t, err)
		assert.Equal(t, expectedConfig, config)
	})

	t.Run("error case", func(t *testing.T) {
		expectedErr := errors.New("failed to get log configuration")
		mockRepo := &mockLogsRepository{config: nil, err: expectedErr}
		service := NewLogsServiceImpl(mockRepo)

		config, err := service.GetLogConfig()

		assert.Error(t, err)
		assert.Equal(t, expectedErr, err)
		assert.Nil(t, config)
	})
}

func TestLogsServiceImpl_PrintLogs(t *testing.T) {
	t.Run("success case", func(t *testing.T) {
		mockRepo := &mockLogsRepository{err: nil}
		service := NewLogsServiceImpl(mockRepo)

		err := service.PrintLogs()

		assert.NoError(t, err)
		assert.True(t, mockRepo.printCalled)
	})

	t.Run("error case", func(t *testing.T) {
		expectedErr := errors.New("failed to print logs")
		mockRepo := &mockLogsRepository{err: expectedErr}
		service := NewLogsServiceImpl(mockRepo)

		err := service.PrintLogs()

		assert.Error(t, err)
		assert.Equal(t, expectedErr, err)
		assert.True(t, mockRepo.printCalled)
	})
}
